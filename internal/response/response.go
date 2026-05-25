package response

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"sync"
)

// Status code constants
const (
	StatusContinue           = 100
	StatusSwitchingProtocols = 101

	StatusOK             = 200
	StatusCreated        = 201
	StatusAccepted       = 202
	StatusNoContent      = 204
	StatusPartialContent = 206

	StatusMovedPermanently  = 301
	StatusFound             = 302
	StatusSeeOther          = 303
	StatusNotModified       = 304
	StatusTemporaryRedirect = 307
	StatusPermanentRedirect = 308

	StatusBadRequest                   = 400
	StatusUnauthorized                 = 401
	StatusForbidden                    = 403
	StatusNotFound                     = 404
	StatusMethodNotAllowed             = 405
	StatusRequestTimeout               = 408
	StatusPayloadTooLarge              = 413
	StatusURITooLong                   = 414
	StatusUnsupportedMediaType         = 415
	StatusRequestedRangeNotSatisfiable = 416
	StatusTooManyRequests              = 429

	StatusInternalServerError     = 500
	StatusNotImplemented          = 501
	StatusBadGateway              = 502
	StatusServiceUnavailable      = 503
	StatusGatewayTimeout          = 504
	StatusHTTPVersionNotSupported = 505
)

// Status code descriptions
var statusText = map[int]string{
	StatusContinue:           "Continue",
	StatusSwitchingProtocols: "Switching Protocols",

	StatusOK:             "OK",
	StatusCreated:        "Created",
	StatusAccepted:       "Accepted",
	StatusNoContent:      "No Content",
	StatusPartialContent: "Partial Content",

	StatusMovedPermanently:  "Moved Permanently",
	StatusFound:             "Found",
	StatusSeeOther:          "See Other",
	StatusNotModified:       "Not Modified",
	StatusTemporaryRedirect: "Temporary Redirect",
	StatusPermanentRedirect: "Permanent Redirect",

	StatusBadRequest:                   "Bad Request",
	StatusUnauthorized:                 "Unauthorized",
	StatusForbidden:                    "Forbidden",
	StatusNotFound:                     "Not Found",
	StatusMethodNotAllowed:             "Method Not Allowed",
	StatusRequestTimeout:               "Request Timeout",
	StatusPayloadTooLarge:              "Payload Too Large",
	StatusURITooLong:                   "URI Too Long",
	StatusUnsupportedMediaType:         "Unsupported Media Type",
	StatusRequestedRangeNotSatisfiable: "Requested Range Not Satisfiable",
	StatusTooManyRequests:              "Too Many Requests",

	StatusInternalServerError:     "Internal Server Error",
	StatusNotImplemented:          "Not Implemented",
	StatusBadGateway:              "Bad Gateway",
	StatusServiceUnavailable:      "Service Unavailable",
	StatusGatewayTimeout:          "Gateway Timeout",
	StatusHTTPVersionNotSupported: "HTTP Version Not Supported",
}

// ResponseWriter is the interface for writing HTTP responses
type ResponseWriter interface {
	// Header returns the header map
	Header() map[string][]string

	// Write writes data to the response body
	Write([]byte) (int, error)

	// WriteHeader writes the status code
	WriteHeader(statusCode int)

	// WriteString writes a string to the response body
	WriteString(s string) (int, error)

	// Status returns the status code
	Status() int

	// Written returns the number of bytes written
	Written() int64

	// Flush flushes the buffered data
	Flush() error
}

// Unwrapper can expose an underlying response writer.
type Unwrapper interface {
	Unwrap() ResponseWriter
}

// CompressionEnabler enables response compression.
type CompressionEnabler interface {
	EnableCompression(level int) error
}

// ConnProvider exposes the underlying network connection.
type ConnProvider interface {
	UnderlyingConn() net.Conn
}

// BodySuppressor suppresses the response body while preserving headers/status.
type BodySuppressor interface {
	SuppressBody()
}

// Writer implements ResponseWriter
type Writer struct {
	Conn          net.Conn // Exported for WebSocket upgrade
	writer        *bufio.Writer
	statusCode    int
	headers       map[string][]string
	headerWritten bool
	finished      bool
	suppressBody  bool
	bodyWritten   int64
	chunked       bool
	contentLength int64

	// Compression
	compressionEnabled bool
	compressionLevel   int
	compressor         *gzip.Writer
	compressorPool     *sync.Pool
	chunkWriter        *chunkWriter
	resourcesReleased  bool

	mu sync.Mutex
}

type chunkWriter struct {
	writer *bufio.Writer
}

func (w *chunkWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	var chunkSize [18]byte
	encoded := strconv.AppendInt(chunkSize[:0], int64(len(data)), 16)
	encoded = append(encoded, '\r', '\n')
	if _, err := w.writer.Write(encoded); err != nil {
		return 0, err
	}
	if _, err := w.writer.Write(data); err != nil {
		return 0, err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return 0, err
	}

	return len(data), nil
}

var responseWriterPool = sync.Pool{
	New: func() any {
		return bufio.NewWriter(io.Discard)
	},
}

var gzipWriterPools sync.Map

// NewWriter creates a new response writer
func NewWriter(conn net.Conn) *Writer {
	bufferedWriter := responseWriterPool.Get().(*bufio.Writer)
	bufferedWriter.Reset(conn)
	return &Writer{
		Conn:       conn,
		writer:     bufferedWriter,
		statusCode: StatusOK,
		headers:    make(map[string][]string),
		chunked:    true, // Default to chunked encoding
	}
}

// Header returns the header map
func (w *Writer) Header() map[string][]string {
	return w.headers
}

// Unwrap returns the concrete writer.
func (w *Writer) Unwrap() ResponseWriter {
	return w
}

// UnderlyingConn returns the underlying connection.
func (w *Writer) UnderlyingConn() net.Conn {
	return w.Conn
}

// SuppressBody forces header-only semantics for responses such as HEAD.
func (w *Writer) SuppressBody() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.suppressBody = true
}

// HeaderWritten reports whether response headers have already been sent.
func (w *Writer) HeaderWritten() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.headerWritten
}

// SetHeader sets a header value
func (w *Writer) SetHeader(key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key = normalizeHeaderKey(key)
	w.headers[key] = []string{value}
}

// AddHeader adds a header value
func (w *Writer) AddHeader(key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key = normalizeHeaderKey(key)
	w.headers[key] = append(w.headers[key], value)
}

// GetHeader returns the first value for a header
func (w *Writer) GetHeader(key string) string {
	w.mu.Lock()
	defer w.mu.Unlock()

	key = normalizeHeaderKey(key)
	values := w.headers[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// WriteHeader writes the HTTP status code
func (w *Writer) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.headerWritten {
		return
	}

	w.statusCode = statusCode
}

// Write writes data to the response body
func (w *Writer) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.finished {
		return 0, errors.New("cannot write after response is finished")
	}

	if w.suppressBody || !statusAllowsBody(w.statusCode) {
		if !w.headerWritten {
			if err := w.writeHeaders(); err != nil {
				return 0, err
			}
		}
		w.bodyWritten += int64(len(data))
		return len(data), nil
	}

	if w.compressionEnabled {
		if !w.headerWritten {
			if err := w.writeHeaders(); err != nil {
				return 0, err
			}
		}
		if err := w.ensureCompressor(); err != nil {
			return 0, err
		}
		n, err := w.compressor.Write(data)
		w.bodyWritten += int64(n)
		return n, err
	}

	// Write headers if not yet written
	if !w.headerWritten {
		if err := w.writeHeaders(); err != nil {
			return 0, err
		}
	}

	// Write data
	var n int
	var err error

	if w.chunked {
		var chunkSize [18]byte
		encoded := strconv.AppendInt(chunkSize[:0], int64(len(data)), 16)
		encoded = append(encoded, '\r', '\n')
		if _, err = w.writer.Write(encoded); err != nil {
			return 0, err
		}

		// Write chunk data
		n, err = w.writer.Write(data)
		if err != nil {
			return n, err
		}

		// Write chunk terminator
		if _, err = w.writer.WriteString("\r\n"); err != nil {
			return n, err
		}
	} else {
		// Fixed content-length mode
		n, err = w.writer.Write(data)
		if err != nil {
			return n, err
		}
	}

	w.bodyWritten += int64(n)
	return n, nil
}

// WriteString writes a string to the response body
func (w *Writer) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// Status returns the status code
func (w *Writer) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.statusCode
}

// Written returns the number of bytes written
func (w *Writer) Written() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bodyWritten
}

// Flush flushes any buffered data
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.finished {
		return nil
	}

	if w.compressionEnabled {
		if !w.headerWritten {
			if err := w.writeHeaders(); err != nil {
				return err
			}
		}
		if !w.responseAllowsBody() {
			return w.writer.Flush()
		}
		if err := w.ensureCompressor(); err != nil {
			return err
		}
		if err := w.compressor.Flush(); err != nil {
			return err
		}
		return w.writer.Flush()
	}

	if !w.headerWritten {
		if err := w.writeHeaders(); err != nil {
			return err
		}
	}

	return w.writer.Flush()
}

// EnableCompression enables gzip compression
func (w *Writer) EnableCompression(level int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.headerWritten {
		return errors.New("cannot enable compression after headers written")
	}

	w.compressionEnabled = true
	w.compressionLevel = level
	w.headers["Content-Encoding"] = []string{"gzip"}

	return nil
}

// writeHeaders writes the HTTP status line and headers
func (w *Writer) writeHeaders() error {
	if w.headerWritten {
		return nil
	}

	// Write status line
	statusText := statusText[w.statusCode]
	if statusText == "" {
		statusText = "Unknown"
	}

	if _, err := w.writer.WriteString("HTTP/1.1 "); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(w.statusCode)); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(" "); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(statusText); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}

	// Check if we should use chunked encoding
	_, hasContentLength := w.headers["Content-Length"]
	if w.compressionEnabled && w.responseAllowsBody() {
		delete(w.headers, "Content-Length")
		w.chunked = true
		w.headers["Transfer-Encoding"] = []string{"chunked"}
	} else if !hasContentLength && w.responseAllowsBody() {
		w.chunked = true
		w.headers["Transfer-Encoding"] = []string{"chunked"}
	} else {
		w.chunked = false
		delete(w.headers, "Transfer-Encoding")
	}

	// Write headers
	for key, values := range w.headers {
		for _, value := range values {
			if _, err := w.writer.WriteString(key); err != nil {
				return err
			}
			if _, err := w.writer.WriteString(": "); err != nil {
				return err
			}
			if _, err := w.writer.WriteString(value); err != nil {
				return err
			}
			if _, err := w.writer.WriteString("\r\n"); err != nil {
				return err
			}
		}
	}

	// Empty line to end headers
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}

	w.headerWritten = true
	return nil
}

// Close closes the writer and flushes any buffered data
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.finished {
		return nil
	}

	if w.compressionEnabled {
		return w.finishCompressed()
	}

	if !w.headerWritten {
		if err := w.writeHeaders(); err != nil {
			return err
		}
	}

	if w.chunked && w.responseAllowsBody() {
		if _, err := w.writer.WriteString("0\r\n\r\n"); err != nil {
			w.releaseResourcesLocked()
			return err
		}
	}

	w.finished = true
	err := w.writer.Flush()
	w.releaseResourcesLocked()
	return err
}

// SetContentLength sets the Content-Length header and disables chunked encoding
func (w *Writer) SetContentLength(length int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.headerWritten {
		return
	}

	w.contentLength = length
	w.chunked = false
	w.headers["Content-Length"] = []string{strconv.FormatInt(length, 10)}
	delete(w.headers, "Transfer-Encoding")
}

// Unwrap walks wrapper layers until it reaches the innermost writer.
func Unwrap(w ResponseWriter) ResponseWriter {
	for {
		unwrapper, ok := w.(Unwrapper)
		if !ok {
			return w
		}

		next := unwrapper.Unwrap()
		if next == nil || next == w {
			return w
		}
		w = next
	}
}

// normalizeHeaderKey normalizes a header key (Title-Case)
func normalizeHeaderKey(key string) string {
	return textproto.CanonicalMIMEHeaderKey(key)
}

// WriteError writes an error response
func WriteError(w ResponseWriter, statusCode int, message string) error {
	w.Header()["Content-Type"] = []string{"text/plain; charset=utf-8"}
	w.WriteHeader(statusCode)

	body := fmt.Sprintf("%d %s\n%s\n", statusCode, statusText[statusCode], message)
	_, err := w.WriteString(body)
	return err
}

// WriteJSON writes a JSON response
func WriteJSON(w ResponseWriter, statusCode int, data []byte) error {
	w.Header()["Content-Type"] = []string{"application/json; charset=utf-8"}
	w.WriteHeader(statusCode)

	_, err := w.Write(data)
	return err
}

// WriteHTML writes an HTML response
func WriteHTML(w ResponseWriter, statusCode int, html string) error {
	w.Header()["Content-Type"] = []string{"text/html; charset=utf-8"}
	w.WriteHeader(statusCode)

	_, err := w.WriteString(html)
	return err
}

// Redirect writes a redirect response
func Redirect(w ResponseWriter, url string, statusCode int) error {
	if statusCode < 300 || statusCode >= 400 {
		statusCode = StatusFound
	}

	w.Header()["Location"] = []string{url}
	w.Header()["Content-Type"] = []string{"text/html; charset=utf-8"}
	w.WriteHeader(statusCode)

	body := fmt.Sprintf("<a href=\"%s\">%s</a>\n", url, statusText[statusCode])
	_, err := w.WriteString(body)
	return err
}

func (w *Writer) finishCompressed() error {
	if !w.headerWritten {
		if err := w.writeHeaders(); err != nil {
			return err
		}
	}
	if !w.responseAllowsBody() {
		w.finished = true
		err := w.writer.Flush()
		w.releaseResourcesLocked()
		return err
	}
	if err := w.ensureCompressor(); err != nil {
		return err
	}
	if err := w.compressor.Close(); err != nil {
		w.releaseResourcesLocked()
		return err
	}
	w.releaseCompressorLocked()
	if w.chunked {
		if _, err := w.writer.WriteString("0\r\n\r\n"); err != nil {
			w.releaseResourcesLocked()
			return err
		}
	}
	w.finished = true
	err := w.writer.Flush()
	w.releaseResourcesLocked()
	return err
}

func (w *Writer) ensureCompressor() error {
	if w.compressor != nil {
		return nil
	}

	w.chunkWriter = &chunkWriter{writer: w.writer}
	compressor, pool, err := getPooledGzipWriter(w.compressionLevel, w.chunkWriter)
	if err != nil {
		return err
	}
	w.compressor = compressor
	w.compressorPool = pool
	return nil
}

func (w *Writer) releaseCompressorLocked() {
	if w.compressor == nil {
		return
	}
	compressor := w.compressor
	compressor.Reset(io.Discard)
	if w.compressorPool != nil {
		w.compressorPool.Put(compressor)
	}
	w.compressor = nil
	w.compressorPool = nil
	w.chunkWriter = nil
}

func (w *Writer) releaseResourcesLocked() {
	if w.resourcesReleased {
		return
	}
	w.releaseCompressorLocked()
	if w.writer != nil {
		w.writer.Reset(io.Discard)
		responseWriterPool.Put(w.writer)
		w.writer = nil
	}
	w.resourcesReleased = true
}

func getPooledGzipWriter(level int, dst io.Writer) (*gzip.Writer, *sync.Pool, error) {
	if cachedPool, ok := gzipWriterPools.Load(level); ok {
		pool := cachedPool.(*sync.Pool)
		writer := pool.Get().(*gzip.Writer)
		writer.Reset(dst)
		return writer, pool, nil
	}

	writer, err := gzip.NewWriterLevel(io.Discard, level)
	if err != nil {
		return nil, nil, err
	}

	pool := &sync.Pool{
		New: func() any {
			pooledWriter, poolErr := gzip.NewWriterLevel(io.Discard, level)
			if poolErr != nil {
				return gzip.NewWriter(io.Discard)
			}
			return pooledWriter
		},
	}
	pool.Put(writer)

	actual, _ := gzipWriterPools.LoadOrStore(level, pool)
	selectedPool := actual.(*sync.Pool)
	selectedWriter := selectedPool.Get().(*gzip.Writer)
	selectedWriter.Reset(dst)
	return selectedWriter, selectedPool, nil
}

func statusAllowsBody(statusCode int) bool {
	return !(statusCode >= 100 && statusCode < 200) &&
		statusCode != StatusNoContent &&
		statusCode != StatusNotModified
}

func (w *Writer) responseAllowsBody() bool {
	return !w.suppressBody && statusAllowsBody(w.statusCode)
}
