package request

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
)

// Request represents an HTTP request
type Request struct {
	Method        string
	URL           *url.URL
	Proto         string
	ProtoMajor    int
	ProtoMinor    int
	Headers       map[string][]string
	Body          io.ReadCloser
	ContentLength int64
	Host          string
	RemoteAddr    string
	RequestURI    string

	// Parsed query parameters
	Query url.Values

	// For router to store path parameters
	PathParams map[string]string
}

// Parser parses HTTP requests from a reader
type Parser struct {
	maxHeaderSize int64
	maxBodySize   int64
}

type emptyBody struct{}

func (emptyBody) Read(_ []byte) (int, error) { return 0, io.EOF }
func (emptyBody) Close() error               { return nil }

// NewParser creates a new request parser
func NewParser() *Parser {
	return &Parser{
		maxHeaderSize: 1 << 20,  // 1 MB
		maxBodySize:   10 << 20, // 10 MB
	}
}

// SetMaxHeaderSize sets the maximum header size
func (p *Parser) SetMaxHeaderSize(size int64) {
	p.maxHeaderSize = size
}

// SetMaxBodySize sets the maximum body size
func (p *Parser) SetMaxBodySize(size int64) {
	p.maxBodySize = size
}

// Parse parses an HTTP request from a reader
func (p *Parser) Parse(reader *bufio.Reader, remoteAddr string) (*Request, error) {
	req := &Request{
		Headers:    make(map[string][]string, 8),
		RemoteAddr: remoteAddr,
	}

	// Parse request line
	if err := p.parseRequestLine(reader, req); err != nil {
		return nil, fmt.Errorf("parse request line: %w", err)
	}

	// Parse headers
	if err := p.parseHeaders(reader, req); err != nil {
		return nil, fmt.Errorf("parse headers: %w", err)
	}

	if err := validateAuthority(req); err != nil {
		return nil, err
	}

	if req.ProtoMajor == 1 && req.ProtoMinor >= 1 && req.Host == "" {
		return nil, errors.New("missing Host header")
	}

	// Parse body if present
	if err := p.parseBody(reader, req); err != nil {
		return nil, fmt.Errorf("parse body: %w", err)
	}

	// Parse query parameters
	if req.URL.RawQuery != "" {
		req.Query = req.URL.Query()
	}

	return req, nil
}

// parseRequestLine parses the request line (e.g., "GET /path HTTP/1.1")
func (p *Parser) parseRequestLine(reader *bufio.Reader, req *Request) error {
	line, err := p.readLine(reader, p.maxHeaderSize)
	if err != nil {
		return err
	}

	firstSpace := bytes.IndexByte(line, ' ')
	if firstSpace <= 0 {
		return errors.New("malformed request line")
	}

	secondSpace := bytes.IndexByte(line[firstSpace+1:], ' ')
	if secondSpace <= 0 {
		return errors.New("malformed request line")
	}
	secondSpace += firstSpace + 1
	if secondSpace+1 >= len(line) {
		return errors.New("malformed request line")
	}
	if bytes.IndexByte(line[secondSpace+1:], ' ') != -1 {
		return errors.New("malformed request line")
	}

	req.Method = string(line[:firstSpace])
	req.RequestURI = string(line[firstSpace+1 : secondSpace])
	req.Proto = string(line[secondSpace+1:])

	// Validate HTTP method
	if !isValidMethod(req.Method) {
		return fmt.Errorf("invalid method: %s", req.Method)
	}

	// Parse URL
	parsedURL, err := parseRequestURI(req.RequestURI)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	req.URL = parsedURL
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}

	// Parse protocol version
	req.ProtoMajor, req.ProtoMinor, err = parseProtocolVersion(req.Proto)
	if err != nil {
		return err
	}

	return nil
}

// parseHeaders parses HTTP headers
func (p *Parser) parseHeaders(reader *bufio.Reader, req *Request) error {
	var totalSize int64
	var contentLengthSet bool
	var hostCount int

	for {
		remaining := p.maxHeaderSize - totalSize
		if remaining <= 0 {
			return errors.New("headers too large")
		}

		line, err := p.readLine(reader, remaining)
		if err != nil {
			return err
		}

		totalSize += int64(len(line))
		if totalSize > p.maxHeaderSize {
			return errors.New("headers too large")
		}

		// Empty line signals end of headers
		if len(line) == 0 {
			break
		}

		// Parse header
		colonIndex := bytes.IndexByte(line, ':')
		if colonIndex == -1 {
			return errors.New("malformed header")
		}

		key := string(bytes.TrimSpace(line[:colonIndex]))
		value := string(bytes.TrimSpace(line[colonIndex+1:]))
		if !isValidHeaderFieldName(key) {
			return errors.New("invalid header name")
		}
		if hasInvalidHeaderValue(value) {
			return errors.New("invalid header value")
		}

		// Normalize header key
		key = normalizeHeaderKey(key)

		// Add to headers map
		req.Headers[key] = append(req.Headers[key], value)

		// Special handling for Host and Content-Length
		switch key {
		case "Host":
			hostCount++
			if hostCount > 1 {
				return errors.New("multiple Host headers")
			}
			req.Host = value
		case "Content-Length":
			length, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid Content-Length: %w", err)
			}
			if length < 0 {
				return errors.New("negative Content-Length")
			}
			if contentLengthSet && req.ContentLength != length {
				return errors.New("conflicting Content-Length headers")
			}
			req.ContentLength = length
			contentLengthSet = true
		}
	}

	return nil
}

// parseBody parses the request body
func (p *Parser) parseBody(reader *bufio.Reader, req *Request) error {
	transferEncoding := strings.ToLower(req.GetHeader("Transfer-Encoding"))
	if transferEncoding != "" {
		if req.ContentLength > 0 {
			return errors.New("Content-Length and Transfer-Encoding cannot be used together")
		}
		if !supportsChunkedRequestBody(transferEncoding) {
			return fmt.Errorf("unsupported Transfer-Encoding: %s", transferEncoding)
		}
		return p.parseChunkedBody(reader, req)
	}

	if req.ContentLength == 0 {
		req.Body = emptyBody{}
		return nil
	}

	if req.ContentLength > p.maxBodySize {
		return errors.New("body too large")
	}

	// Read body into buffer
	body := make([]byte, req.ContentLength)
	_, err := io.ReadFull(reader, body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	req.Body = io.NopCloser(bytes.NewReader(body))
	return nil
}

func (p *Parser) parseChunkedBody(reader *bufio.Reader, req *Request) error {
	var body bytes.Buffer
	var totalSize int64

	for {
		line, err := p.readLine(reader, p.maxHeaderSize)
		if err != nil {
			return err
		}

		sizeLine := string(line)
		if semi := strings.IndexByte(sizeLine, ';'); semi >= 0 {
			sizeLine = sizeLine[:semi]
		}

		chunkSize, err := strconv.ParseInt(strings.TrimSpace(sizeLine), 16, 64)
		if err != nil {
			return fmt.Errorf("invalid chunk size: %w", err)
		}
		if chunkSize < 0 {
			return errors.New("negative chunk size")
		}

		if chunkSize == 0 {
			for {
				trailer, err := p.readLine(reader, p.maxHeaderSize)
				if err != nil {
					return err
				}
				if len(trailer) == 0 {
					req.ContentLength = totalSize
					req.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
					return nil
				}
			}
		}

		totalSize += chunkSize
		if totalSize > p.maxBodySize {
			return errors.New("body too large")
		}

		chunk := make([]byte, chunkSize)
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return fmt.Errorf("read chunk: %w", err)
		}
		if _, err := body.Write(chunk); err != nil {
			return err
		}

		var crlf [2]byte
		if _, err := io.ReadFull(reader, crlf[:]); err != nil {
			return fmt.Errorf("read chunk terminator: %w", err)
		}
		if crlf != [2]byte{'\r', '\n'} {
			return errors.New("invalid chunk terminator")
		}
	}
}

// readLine reads a line from the reader (up to CRLF)
func (p *Parser) readLine(reader *bufio.Reader, limit int64) ([]byte, error) {
	fragment, err := reader.ReadSlice('\n')
	if err == nil {
		if int64(len(fragment)) > limit {
			return nil, errors.New("line too long")
		}
		return trimLine(fragment), nil
	}
	if !errors.Is(err, bufio.ErrBufferFull) {
		return nil, err
	}

	line := append([]byte(nil), fragment...)
	if int64(len(line)) > limit {
		return nil, errors.New("line too long")
	}

	for {
		fragment, err = reader.ReadSlice('\n')
		line = append(line, fragment...)
		if int64(len(line)) > limit {
			return nil, errors.New("line too long")
		}
		if err == nil {
			return trimLine(line), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}

// GetHeader returns the first value for a header
func (r *Request) GetHeader(key string) string {
	key = normalizeHeaderKey(key)
	values := r.Headers[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// GetHeaders returns all values for a header
func (r *Request) GetHeaders(key string) []string {
	key = normalizeHeaderKey(key)
	return r.Headers[key]
}

// SetHeader sets a header value
func (r *Request) SetHeader(key, value string) {
	key = normalizeHeaderKey(key)
	r.Headers[key] = []string{value}
}

// AddHeader adds a header value
func (r *Request) AddHeader(key, value string) {
	key = normalizeHeaderKey(key)
	r.Headers[key] = append(r.Headers[key], value)
}

// isValidMethod checks if an HTTP method is valid
func isValidMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		return true
	default:
		return false
	}
}

// normalizeHeaderKey normalizes a header key (Title-Case)
func normalizeHeaderKey(key string) string {
	return textproto.CanonicalMIMEHeaderKey(key)
}

func trimLine(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\r\n"))
	return bytes.TrimSuffix(line, []byte("\n"))
}

func parseProtocolVersion(proto string) (int, int, error) {
	if !strings.HasPrefix(proto, "HTTP/") {
		return 0, 0, fmt.Errorf("invalid protocol: %s", proto)
	}

	version := proto[len("HTTP/"):]
	dot := strings.IndexByte(version, '.')
	if dot <= 0 || dot == len(version)-1 {
		return 0, 0, fmt.Errorf("invalid protocol version: %s", proto)
	}

	major, err := strconv.Atoi(version[:dot])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid protocol major version: %w", err)
	}
	minor, err := strconv.Atoi(version[dot+1:])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid protocol minor version: %w", err)
	}

	return major, minor, nil
}

func parseRequestURI(requestURI string) (*url.URL, error) {
	if requestURI == "" {
		return nil, errors.New("empty request URI")
	}
	if requestURI == "*" {
		return &url.URL{Path: "*"}, nil
	}
	if requestURI[0] == '/' {
		path := requestURI
		rawQuery := ""
		if queryIndex := strings.IndexByte(requestURI, '?'); queryIndex >= 0 {
			path = requestURI[:queryIndex]
			rawQuery = requestURI[queryIndex+1:]
		}
		if path == "" {
			path = "/"
		}
		if strings.IndexByte(path, '%') == -1 {
			return &url.URL{
				Path:     path,
				RawQuery: rawQuery,
			}, nil
		}
	}

	return url.Parse(requestURI)
}

func validateAuthority(req *Request) error {
	if req.URL == nil || req.URL.Host == "" {
		return nil
	}

	if req.Host == "" {
		req.Host = req.URL.Host
		return nil
	}

	if !strings.EqualFold(req.Host, req.URL.Host) {
		return errors.New("absolute-form request target host does not match Host header")
	}

	return nil
}

func supportsChunkedRequestBody(transferEncoding string) bool {
	tokens := strings.Split(transferEncoding, ",")
	if len(tokens) != 1 {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(tokens[0]), "chunked")
}

func isValidHeaderFieldName(name string) bool {
	if name == "" {
		return false
	}

	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)):
		default:
			return false
		}
	}

	return true
}

func hasInvalidHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\t' {
			continue
		}
		if c < 0x20 || c == 0x7f {
			return true
		}
	}

	return false
}
