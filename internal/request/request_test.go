package request

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseSimpleGET(t *testing.T) {
	req := "GET /hello HTTP/1.1\r\nHost: localhost\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	parser := NewParser()
	parsed, err := parser.Parse(reader, "127.0.0.1:8080")

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.Method != "GET" {
		t.Errorf("Expected method GET, got %s", parsed.Method)
	}

	if parsed.URL.Path != "/hello" {
		t.Errorf("Expected path /hello, got %s", parsed.URL.Path)
	}

	if parsed.Proto != "HTTP/1.1" {
		t.Errorf("Expected proto HTTP/1.1, got %s", parsed.Proto)
	}

	if parsed.Host != "localhost" {
		t.Errorf("Expected host localhost, got %s", parsed.Host)
	}
}

func TestParsePOSTWithBody(t *testing.T) {
	body := "test=data"
	req := "POST /api/test HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Content-Length: 9\r\n" +
		"Content-Type: application/x-www-form-urlencoded\r\n" +
		"\r\n" +
		body

	reader := bufio.NewReader(strings.NewReader(req))
	parser := NewParser()
	parsed, err := parser.Parse(reader, "127.0.0.1:8080")

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.Method != "POST" {
		t.Errorf("Expected method POST, got %s", parsed.Method)
	}

	if parsed.ContentLength != 9 {
		t.Errorf("Expected content length 9, got %d", parsed.ContentLength)
	}

	readBody := make([]byte, parsed.ContentLength)
	parsed.Body.Read(readBody)

	if string(readBody) != body {
		t.Errorf("Expected body %s, got %s", body, string(readBody))
	}
}

func TestParseHeaders(t *testing.T) {
	req := "GET / HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"User-Agent: TestClient/1.0\r\n" +
		"Accept: application/json\r\n" +
		"Authorization: Bearer token123\r\n" +
		"\r\n"

	reader := bufio.NewReader(strings.NewReader(req))
	parser := NewParser()
	parsed, err := parser.Parse(reader, "127.0.0.1:8080")

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	testCases := []struct {
		header string
		want   string
	}{
		{"Host", "example.com"},
		{"User-Agent", "TestClient/1.0"},
		{"Accept", "application/json"},
		{"Authorization", "Bearer token123"},
	}

	for _, tc := range testCases {
		got := parsed.GetHeader(tc.header)
		if got != tc.want {
			t.Errorf("Header %s: expected %s, got %s", tc.header, tc.want, got)
		}
	}
}

func TestParseQueryParameters(t *testing.T) {
	req := "GET /search?q=golang&page=2&limit=10 HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"\r\n"

	reader := bufio.NewReader(strings.NewReader(req))
	parser := NewParser()
	parsed, err := parser.Parse(reader, "127.0.0.1:8080")

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.Query.Get("q") != "golang" {
		t.Errorf("Expected q=golang, got %s", parsed.Query.Get("q"))
	}

	if parsed.Query.Get("page") != "2" {
		t.Errorf("Expected page=2, got %s", parsed.Query.Get("page"))
	}

	if parsed.Query.Get("limit") != "10" {
		t.Errorf("Expected limit=10, got %s", parsed.Query.Get("limit"))
	}
}

func TestParseEscapedPathPreservesDecodedAndRawForms(t *testing.T) {
	req := "GET /a%2Fb?name=test HTTP/1.1\r\nHost: localhost\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	parser := NewParser()
	parsed, err := parser.Parse(reader, "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.URL.Path != "/a/b" {
		t.Fatalf("expected decoded path /a/b, got %q", parsed.URL.Path)
	}
	if parsed.URL.RawPath != "/a%2Fb" {
		t.Fatalf("expected raw path /a%%2Fb, got %q", parsed.URL.RawPath)
	}
	if parsed.Query.Get("name") != "test" {
		t.Fatalf("expected query name=test, got %q", parsed.Query.Get("name"))
	}
}

func TestParseInvalidMethod(t *testing.T) {
	req := "INVALID /path HTTP/1.1\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	parser := NewParser()
	_, err := parser.Parse(reader, "127.0.0.1:8080")

	if err == nil {
		t.Error("Expected error for invalid method, got nil")
	}
}

func TestParseMalformedRequest(t *testing.T) {
	req := "GET\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	parser := NewParser()
	_, err := parser.Parse(reader, "127.0.0.1:8080")

	if err == nil {
		t.Error("Expected error for malformed request, got nil")
	}
}

func TestParseHTTPVersions(t *testing.T) {
	testCases := []struct {
		proto string
		major int
		minor int
	}{
		{"HTTP/1.0", 1, 0},
		{"HTTP/1.1", 1, 1},
	}

	for _, tc := range testCases {
		req := "GET / " + tc.proto + "\r\n"
		if tc.proto == "HTTP/1.1" {
			req += "Host: localhost\r\n"
		}
		req += "\r\n"
		reader := bufio.NewReader(strings.NewReader(req))

		parser := NewParser()
		parsed, err := parser.Parse(reader, "127.0.0.1:8080")

		if err != nil {
			t.Fatalf("Parse failed for %s: %v", tc.proto, err)
		}

		if parsed.ProtoMajor != tc.major {
			t.Errorf("Expected major version %d, got %d", tc.major, parsed.ProtoMajor)
		}

		if parsed.ProtoMinor != tc.minor {
			t.Errorf("Expected minor version %d, got %d", tc.minor, parsed.ProtoMinor)
		}
	}
}

func TestHeaderNormalization(t *testing.T) {
	testCases := []struct {
		input string
		want  string
	}{
		{"content-type", "Content-Type"},
		{"CONTENT-TYPE", "Content-Type"},
		{"Content-Type", "Content-Type"},
		{"x-custom-header", "X-Custom-Header"},
	}

	for _, tc := range testCases {
		got := normalizeHeaderKey(tc.input)
		if got != tc.want {
			t.Errorf("normalizeHeaderKey(%s) = %s, want %s", tc.input, got, tc.want)
		}
	}
}

func TestParseMultipleHeaderValues(t *testing.T) {
	req := "GET / HTTP/1.1\r\n" +
		"Accept: text/html\r\n" +
		"Accept: application/json\r\n" +
		"Host: localhost\r\n" +
		"\r\n"

	reader := bufio.NewReader(strings.NewReader(req))
	parser := NewParser()
	parsed, err := parser.Parse(reader, "127.0.0.1:8080")

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	accepts := parsed.GetHeaders("Accept")
	if len(accepts) != 2 {
		t.Errorf("Expected 2 Accept headers, got %d", len(accepts))
	}

	if accepts[0] != "text/html" || accepts[1] != "application/json" {
		t.Errorf("Unexpected Accept header values: %v", accepts)
	}
}

func TestParseNegativeContentLength(t *testing.T) {
	req := "POST /upload HTTP/1.1\r\nHost: localhost\r\nContent-Length: -1\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	parser := NewParser()
	_, err := parser.Parse(reader, "127.0.0.1:8080")

	if err == nil {
		t.Fatal("Expected error for negative Content-Length")
	}
}

func TestParseChunkedBody(t *testing.T) {
	req := "" +
		"POST /chunked HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"\r\n" +
		"4\r\nWiki\r\n" +
		"5\r\npedia\r\n" +
		"0\r\n\r\n"

	reader := bufio.NewReader(strings.NewReader(req))
	parser := NewParser()
	parsed, err := parser.Parse(reader, "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	body := make([]byte, parsed.ContentLength)
	if _, err := parsed.Body.Read(body); err != nil {
		t.Fatalf("Failed to read parsed chunked body: %v", err)
	}

	if string(body) != "Wikipedia" {
		t.Fatalf("Expected chunked body Wikipedia, got %q", string(body))
	}
}

func TestParseMissingHostOnHTTP11(t *testing.T) {
	req := "GET /hello HTTP/1.1\r\nUser-Agent: test\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	parser := NewParser()
	_, err := parser.Parse(reader, "127.0.0.1:8080")
	if err == nil {
		t.Fatal("Expected missing Host header error")
	}
}

func TestParseRejectsMultipleHostHeaders(t *testing.T) {
	req := "GET /hello HTTP/1.1\r\nHost: localhost\r\nHost: attacker\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	parser := NewParser()
	_, err := parser.Parse(reader, "127.0.0.1:8080")
	if err == nil {
		t.Fatal("Expected multiple Host header error")
	}
}

func TestParseRejectsAbsoluteFormHostMismatch(t *testing.T) {
	req := "GET http://example.com/hello HTTP/1.1\r\nHost: attacker.com\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	parser := NewParser()
	_, err := parser.Parse(reader, "127.0.0.1:8080")
	if err == nil {
		t.Fatal("Expected host mismatch error")
	}
}

func TestParseRejectsOversizedRequestLine(t *testing.T) {
	parser := NewParser()
	parser.SetMaxHeaderSize(64)

	req := "GET /" + strings.Repeat("a", 80) + " HTTP/1.1\r\nHost: localhost\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(req))

	_, err := parser.Parse(reader, "127.0.0.1:8080")
	if err == nil {
		t.Fatal("Expected oversized request line error")
	}
}

func TestParseRejectsUnsupportedTransferEncodingChain(t *testing.T) {
	req := "" +
		"POST /chunked HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Transfer-Encoding: gzip, chunked\r\n" +
		"\r\n"

	reader := bufio.NewReader(strings.NewReader(req))
	parser := NewParser()
	_, err := parser.Parse(reader, "127.0.0.1:8080")
	if err == nil {
		t.Fatal("Expected unsupported transfer-encoding error")
	}
}
