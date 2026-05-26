package benchmark

import (
	"bufio"
	"bytes"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/sanskarpan/http-server/internal/middleware"
	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
	"github.com/sanskarpan/http-server/internal/router"
)

type benchConn struct {
	bytes.Buffer
}

func (c *benchConn) Read(p []byte) (int, error)         { return 0, nil }
func (c *benchConn) Write(p []byte) (int, error)        { return c.Buffer.Write(p) }
func (c *benchConn) Close() error                       { return nil }
func (c *benchConn) LocalAddr() net.Addr                { return nil }
func (c *benchConn) RemoteAddr() net.Addr               { return nil }
func (c *benchConn) SetDeadline(_ time.Time) error      { return nil }
func (c *benchConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *benchConn) SetWriteDeadline(_ time.Time) error { return nil }

func BenchmarkRequestParseSimpleGET(b *testing.B) {
	raw := []byte("GET /hello?name=world HTTP/1.1\r\nHost: localhost\r\nUser-Agent: benchmark\r\n\r\n")
	parser := request.NewParser()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader := bufio.NewReader(bytes.NewReader(raw))
		req, err := parser.Parse(reader, "127.0.0.1:1234")
		if err != nil {
			b.Fatal(err)
		}
		_ = req.Body.Close()
	}
}

func BenchmarkRouterStaticLookup(b *testing.B) {
	r := router.New()
	r.GET("/health", router.HandlerFunc(func(w response.ResponseWriter, req *request.Request) {}))

	parsedURL, _ := url.Parse("/health")
	req := &request.Request{
		Method: "GET",
		URL:    parsedURL,
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var buf []byte
		w := &benchResponseWriter{buf: &buf}
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRouterParamLookup(b *testing.B) {
	r := router.New()
	r.GET("/users/:id/posts/:postID", router.HandlerFunc(func(w response.ResponseWriter, req *request.Request) {}))

	parsedURL, _ := url.Parse("/users/123/posts/456")
	req := &request.Request{
		Method: "GET",
		URL:    parsedURL,
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var buf []byte
		w := &benchResponseWriter{buf: &buf}
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRateLimiterAllow(b *testing.B) {
	bucket := middleware.NewTokenBucket(float64(b.N+1), 0)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !bucket.Allow() {
			b.Fatal("unexpected rate limit in benchmark")
		}
	}
}

func BenchmarkResponseWriterChunked(b *testing.B) {
	payload := []byte(`{"status":"ok","message":"benchmark payload"}`)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		conn := &benchConn{}
		writer := response.NewWriter(conn)
		if _, err := writer.Write(payload); err != nil {
			b.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResponseWriterCompressed(b *testing.B) {
	payload := []byte(bytes.Repeat([]byte("compressed-benchmark-payload"), 16))

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		conn := &benchConn{}
		writer := response.NewWriter(conn)
		if err := writer.EnableCompression(6); err != nil {
			b.Fatal(err)
		}
		if _, err := writer.Write(payload); err != nil {
			b.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

type benchResponseWriter struct {
	buf        *[]byte
	statusCode int
	headers    map[string][]string
}

func (m *benchResponseWriter) Header() map[string][]string {
	if m.headers == nil {
		m.headers = make(map[string][]string)
	}
	return m.headers
}

func (m *benchResponseWriter) Write(p []byte) (int, error) {
	*m.buf = append(*m.buf, p...)
	return len(p), nil
}

func (m *benchResponseWriter) WriteHeader(code int) {
	m.statusCode = code
}

func (m *benchResponseWriter) WriteString(s string) (int, error) {
	return m.Write([]byte(s))
}

func (m *benchResponseWriter) Status() int {
	return m.statusCode
}

func (m *benchResponseWriter) Written() int64 {
	return int64(len(*m.buf))
}

func (m *benchResponseWriter) Flush() error {
	return nil
}
