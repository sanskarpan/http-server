package server

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
)

// Test setup helper
func setupTestFiles(t *testing.T) (string, func()) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "http-server-static-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create test files
	testFiles := map[string]string{
		"index.html":          "<html><body>Index</body></html>",
		"test.txt":            "Hello, World!",
		"test.json":           `{"key": "value"}`,
		"large.txt":           string(make([]byte, 1024*1024)), // 1MB
		".env":                "SECRET=password123",
		".git/config":         "git config",
		"subdir/file.txt":     "Subdirectory file",
		"subdir/index.html":   "<html>Subdir index</html>",
		"special-chars-_.txt": "Special chars",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", path, err)
		}
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup
}

// Mock response writer for testing
type mockResponseWriter struct {
	headers    map[string][]string
	statusCode int
	body       []byte
	written    int64
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		headers: make(map[string][]string),
	}
}

func (m *mockResponseWriter) Header() map[string][]string {
	return m.headers
}

func (m *mockResponseWriter) Write(p []byte) (int, error) {
	m.body = append(m.body, p...)
	m.written += int64(len(p))
	return len(p), nil
}

func (m *mockResponseWriter) WriteHeader(code int) {
	m.statusCode = code
}

func (m *mockResponseWriter) WriteString(s string) (int, error) {
	return m.Write([]byte(s))
}

func (m *mockResponseWriter) Status() int {
	return m.statusCode
}

func (m *mockResponseWriter) Written() int64 {
	return m.written
}

func (m *mockResponseWriter) Flush() error {
	return nil
}

// SECURITY TESTS - PATH TRAVERSAL

func TestStaticFileServer_PathTraversal_DoubleDot(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	// Create a secret file outside the root
	secretFile := filepath.Join(filepath.Dir(tmpDir), "secret.txt")
	os.WriteFile(secretFile, []byte("SECRET DATA"), 0644)
	defer os.Remove(secretFile)

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	testCases := []struct {
		name       string
		path       string
		shouldFail bool
	}{
		{"Simple traversal", "/../secret.txt", true},
		{"Double traversal", "/../../secret.txt", true},
		{"Traversal with file", "/test.txt/../../secret.txt", true},
		{"Traversal from subdir", "/subdir/../../secret.txt", true},
		{"Valid file", "/test.txt", false},
		{"Valid subdir file", "/subdir/file.txt", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &request.Request{
				Method: "GET",
			}
			req.URL, _ = parseURL(tc.path)

			w := newMockResponseWriter()
			handler(w, req)

			if tc.shouldFail {
				// Accept 403 Forbidden or 404 Not Found - both prevent access
				if w.statusCode != response.StatusForbidden && w.statusCode != response.StatusNotFound {
					t.Errorf("Expected 403 or 404, got %d", w.statusCode)
				}
				if string(w.body) == "SECRET DATA" {
					t.Error("Path traversal attack succeeded - secret file exposed!")
				}
			} else {
				if w.statusCode != response.StatusOK {
					t.Errorf("Valid file request failed with %d: %s", w.statusCode, tc.path)
				}
			}
		})
	}
}

func TestStaticFileServer_PathTraversal_URLEncoded(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	testCases := []string{
		"/%2e%2e/secret.txt",          // URL encoded ../
		"/%2e%2e%2fsecret.txt",        // URL encoded ../
		"/%252e%252e/secret.txt",      // Double encoded
		"/test.txt/%2e%2e/secret.txt", // Encoded after valid path
	}

	for _, path := range testCases {
		t.Run(path, func(t *testing.T) {
			req := &request.Request{
				Method: "GET",
			}
			req.URL, _ = parseURL(path)

			w := newMockResponseWriter()
			handler(w, req)

			// Should be blocked (403) or not found (404), but never expose parent files
			if w.statusCode == response.StatusOK {
				t.Errorf("URL-encoded path traversal succeeded with status %d", w.statusCode)
			}
		})
	}
}

func TestStaticFileServer_HiddenFiles(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	testCases := []struct {
		path       string
		shouldFail bool
	}{
		{"/.env", true},
		{"/.git/config", true},
		{"/test.txt", false},
		{"/subdir/.env", true}, // if we created it
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			req := &request.Request{
				Method: "GET",
			}
			req.URL, _ = parseURL(tc.path)

			w := newMockResponseWriter()
			handler(w, req)

			if tc.shouldFail {
				if w.statusCode == response.StatusOK {
					t.Errorf("Hidden file exposed: %s", tc.path)
				}
				if string(w.body) == "SECRET=password123" {
					t.Error(".env file content exposed!")
				}
			}
		})
	}
}

func TestStaticFileServer_BlocksSymlinkEscapes(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	secretFile := filepath.Join(filepath.Dir(tmpDir), "outside-secret.txt")
	if err := os.WriteFile(secretFile, []byte("TOP SECRET"), 0644); err != nil {
		t.Fatalf("Failed to create outside file: %v", err)
	}
	defer os.Remove(secretFile)

	linkPath := filepath.Join(tmpDir, "symlink-secret.txt")
	if err := os.Symlink(secretFile, linkPath); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	req := &request.Request{Method: "GET"}
	req.URL, _ = parseURL("/symlink-secret.txt")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode == response.StatusOK {
		t.Fatalf("expected symlink escape to be blocked, got 200 with body %q", string(w.body))
	}
}

func TestStaticFileServer_DirectoryListingEscapesNamesAndOmitsHiddenFiles(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	dangerousName := `<img onerror=alert(1)>.txt`
	listDir := filepath.Join(tmpDir, "listdir")
	if err := os.MkdirAll(listDir, 0755); err != nil {
		t.Fatalf("Failed to create listing dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(listDir, dangerousName), []byte("xss"), 0644); err != nil {
		t.Fatalf("Failed to create dangerous file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(listDir, ".env"), []byte("secret"), 0644); err != nil {
		t.Fatalf("Failed to create hidden file: %v", err)
	}

	config := DefaultStaticConfig(tmpDir)
	config.AllowDirList = true
	handler := StaticFileServer(config)

	req := &request.Request{Method: "GET"}
	req.URL, _ = parseURL("/listdir/")

	w := newMockResponseWriter()
	handler(w, req)

	body := string(w.body)
	if strings.Contains(body, dangerousName) {
		t.Fatalf("directory listing leaked unescaped filename: %q", body)
	}
	if !strings.Contains(body, "&lt;img onerror=alert(1)&gt;.txt") {
		t.Fatalf("directory listing did not escape filename: %q", body)
	}
	if strings.Contains(body, ".env") || strings.Contains(body, ".git") {
		t.Fatalf("directory listing exposed hidden entries: %q", body)
	}
}

// BASIC FUNCTIONALITY TESTS

func TestStaticFileServer_BasicFileServing(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	testCases := []struct {
		path            string
		expectedStatus  int
		expectedContent string
		expectedType    string
	}{
		{"/test.txt", response.StatusOK, "Hello, World!", "text/plain"},
		{"/test.json", response.StatusOK, `{"key": "value"}`, "application/json"},
		{"/index.html", response.StatusOK, "<html><body>Index</body></html>", "text/html"},
		{"/subdir/file.txt", response.StatusOK, "Subdirectory file", "text/plain"},
		{"/nonexistent.txt", response.StatusNotFound, "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			req := &request.Request{
				Method: "GET",
			}
			req.URL, _ = parseURL(tc.path)

			w := newMockResponseWriter()
			handler(w, req)

			if w.statusCode != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.statusCode)
			}

			if tc.expectedStatus == response.StatusOK {
				if string(w.body) != tc.expectedContent {
					t.Errorf("Expected content %q, got %q", tc.expectedContent, string(w.body))
				}

				contentType := w.headers["Content-Type"]
				if len(contentType) == 0 {
					t.Error("No Content-Type header set")
				} else if !strings.Contains(contentType[0], tc.expectedType) {
					t.Errorf("Expected Content-Type containing %q, got %q", tc.expectedType, contentType[0])
				}
			}
		})
	}
}

func TestStaticFileServer_DirectoryIndex(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	testCases := []struct {
		name           string
		path           string
		expectedStatus int
		shouldContain  string
	}{
		{"Root with index.html", "/", response.StatusOK, "<html><body>Index</body></html>"},
		{"Subdir with index.html", "/subdir/", response.StatusOK, "<html>Subdir index</html>"},
		{"Subdir without slash", "/subdir", response.StatusOK, "<html>Subdir index</html>"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &request.Request{
				Method: "GET",
			}
			req.URL, _ = parseURL(tc.path)

			w := newMockResponseWriter()
			handler(w, req)

			if w.statusCode != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.statusCode)
			}

			if tc.shouldContain != "" && !strings.Contains(string(w.body), tc.shouldContain) {
				t.Errorf("Response should contain %q, got %q", tc.shouldContain, string(w.body))
			}
		})
	}
}

func TestStaticFileServer_DirectoryListingForbidden(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	// Create directory without index file
	noIndexDir := filepath.Join(tmpDir, "noindex")
	os.Mkdir(noIndexDir, 0755)
	os.WriteFile(filepath.Join(noIndexDir, "file.txt"), []byte("content"), 0644)

	config := DefaultStaticConfig(tmpDir)
	config.AllowDirList = false
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
	}
	req.URL, _ = parseURL("/noindex/")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusForbidden {
		t.Errorf("Expected 403 Forbidden, got %d", w.statusCode)
	}
}

func TestStaticFileServer_DirectoryListingAllowed(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	// Create directory without index file for listing test
	noIndexDir := filepath.Join(tmpDir, "listdir")
	os.Mkdir(noIndexDir, 0755)
	os.WriteFile(filepath.Join(noIndexDir, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(noIndexDir, "file2.txt"), []byte("content2"), 0644)

	config := DefaultStaticConfig(tmpDir)
	config.AllowDirList = true
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
	}
	req.URL, _ = parseURL("/listdir/")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusOK {
		t.Errorf("Expected 200 OK, got %d", w.statusCode)
	}

	body := string(w.body)
	if !strings.Contains(body, "Index of") {
		t.Error("Directory listing should contain 'Index of'")
	}
	if !strings.Contains(body, "file1.txt") {
		t.Error("Directory listing should contain 'file1.txt'")
	}
}

// ETAG CACHING TESTS

func TestStaticFileServer_ETagGeneration(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	config.EnableCaching = true
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
	}
	req.URL, _ = parseURL("/test.txt")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", w.statusCode)
	}

	etag := w.headers["ETag"]
	if len(etag) == 0 {
		t.Error("ETag header not set")
	}

	lastModified := w.headers["Last-Modified"]
	if len(lastModified) == 0 {
		t.Error("Last-Modified header not set")
	}

	cacheControl := w.headers["Cache-Control"]
	if len(cacheControl) == 0 {
		t.Error("Cache-Control header not set")
	}
}

func TestStaticFileServer_IfNoneMatch_304(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	config.EnableCaching = true
	handler := StaticFileServer(config)

	// First request to get ETag
	req1 := &request.Request{
		Method:  "GET",
		Headers: make(map[string][]string),
	}
	req1.URL, _ = parseURL("/test.txt")

	w1 := newMockResponseWriter()
	handler(w1, req1)

	if w1.statusCode != response.StatusOK {
		t.Fatalf("First request failed with status %d", w1.statusCode)
	}

	etag := w1.headers["ETag"]
	if len(etag) == 0 {
		t.Fatal("No ETag returned")
	}

	// Second request with If-None-Match
	req2 := &request.Request{
		Method: "GET",
		Headers: map[string][]string{
			"If-None-Match": {etag[0]},
		},
	}
	req2.URL, _ = parseURL("/test.txt")

	w2 := newMockResponseWriter()
	handler(w2, req2)

	if w2.statusCode != response.StatusNotModified {
		t.Errorf("Expected 304 Not Modified, got %d", w2.statusCode)
	}

	if len(w2.body) > 0 {
		t.Error("304 response should have empty body")
	}
}

// RANGE REQUEST TESTS

func TestStaticFileServer_RangeRequest_Simple(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	config.EnableRange = true
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
		Headers: map[string][]string{
			"Range": {"bytes=0-4"},
		},
	}
	req.URL, _ = parseURL("/test.txt")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusPartialContent {
		t.Errorf("Expected 206 Partial Content, got %d", w.statusCode)
	}

	if string(w.body) != "Hello" {
		t.Errorf("Expected 'Hello', got %q", string(w.body))
	}

	contentRange := w.headers["Content-Range"]
	if len(contentRange) == 0 {
		t.Error("Content-Range header not set")
	} else if contentRange[0] != "bytes 0-4/13" {
		t.Errorf("Expected 'bytes 0-4/13', got %q", contentRange[0])
	}
}

func TestStaticFileServer_RangeRequest_OpenEnded(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	config.EnableRange = true
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
		Headers: map[string][]string{
			"Range": {"bytes=7-"},
		},
	}
	req.URL, _ = parseURL("/test.txt")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusPartialContent {
		t.Errorf("Expected 206 Partial Content, got %d", w.statusCode)
	}

	if string(w.body) != "World!" {
		t.Errorf("Expected 'World!', got %q", string(w.body))
	}
}

func TestStaticFileServer_RangeRequest_Suffix(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	config.EnableRange = true
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
		Headers: map[string][]string{
			"Range": {"bytes=-6"},
		},
	}
	req.URL, _ = parseURL("/test.txt")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusPartialContent {
		t.Errorf("Expected 206 Partial Content, got %d", w.statusCode)
	}

	if string(w.body) != "World!" {
		t.Errorf("Expected 'World!' (last 6 bytes), got %q", string(w.body))
	}
}

func TestStaticFileServer_RangeRequest_Invalid(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	config.EnableRange = true
	handler := StaticFileServer(config)

	testCases := []string{
		"bytes=100-200", // Beyond file size
		"bytes=10-5",    // Start > End
		"notbytes=0-5",  // Invalid format
		"bytes=",        // Empty
	}

	for _, rangeHeader := range testCases {
		t.Run(rangeHeader, func(t *testing.T) {
			req := &request.Request{
				Method: "GET",
				Headers: map[string][]string{
					"Range": {rangeHeader},
				},
			}
			req.URL, _ = parseURL("/test.txt")

			w := newMockResponseWriter()
			handler(w, req)

			if w.statusCode == response.StatusPartialContent {
				t.Errorf("Invalid range %q should not return 206", rangeHeader)
			}
		})
	}
}

// METHOD TESTS

func TestStaticFileServer_MethodNotAllowed(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	methods := []string{"POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := &request.Request{
				Method: method,
			}
			req.URL, _ = parseURL("/test.txt")

			w := newMockResponseWriter()
			handler(w, req)

			if w.statusCode != response.StatusMethodNotAllowed {
				t.Errorf("Expected 405 Method Not Allowed for %s, got %d", method, w.statusCode)
			}
		})
	}
}

func TestStaticFileServer_HEADRequest(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "HEAD",
	}
	req.URL, _ = parseURL("/test.txt")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusOK {
		t.Errorf("Expected 200 OK, got %d", w.statusCode)
	}

	if len(w.body) > 0 {
		t.Error("HEAD request should have empty body")
	}

	// Should still have headers
	if len(w.headers["Content-Type"]) == 0 {
		t.Error("HEAD request should have Content-Type header")
	}
}

// FILE SIZE LIMIT TESTS

func TestStaticFileServer_FileSizeLimit(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	config.MaxFileSize = 500 * 1024 // 500 KB
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
	}
	req.URL, _ = parseURL("/large.txt")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusPayloadTooLarge {
		t.Errorf("Expected 413 Payload Too Large, got %d", w.statusCode)
	}
}

// EDGE CASES

func TestStaticFileServer_EmptyPath(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
	}
	req.URL, _ = parseURL("/")

	w := newMockResponseWriter()
	handler(w, req)

	// Should serve index.html
	if w.statusCode != response.StatusOK {
		t.Errorf("Expected 200 OK, got %d", w.statusCode)
	}
}

func TestStaticFileServer_SpecialCharacters(t *testing.T) {
	tmpDir, cleanup := setupTestFiles(t)
	defer cleanup()

	config := DefaultStaticConfig(tmpDir)
	handler := StaticFileServer(config)

	req := &request.Request{
		Method: "GET",
	}
	req.URL, _ = parseURL("/special-chars-_.txt")

	w := newMockResponseWriter()
	handler(w, req)

	if w.statusCode != response.StatusOK {
		t.Errorf("Expected 200 OK for file with special chars, got %d", w.statusCode)
	}
}

// Helper functions

func parseURL(path string) (*url.URL, error) {
	return url.Parse(path)
}
