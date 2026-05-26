package router

import (
	"net/url"
	"testing"

	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
)

func TestStaticRoutes(t *testing.T) {
	router := New()

	called := false
	router.GET("/users", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		called = true
	}))

	// Create mock request
	req := &request.Request{
		Method: "GET",
	}
	req.URL, _ = parseURL("/users")

	// Create mock response writer
	var buf []byte
	w := &mockResponseWriter{buf: &buf}

	router.ServeHTTP(w, req)

	if !called {
		t.Error("Handler was not called for static route")
	}
}

func TestParametricRoutes(t *testing.T) {
	router := New()

	var capturedID string
	router.GET("/users/:id", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		capturedID = r.PathParams["id"]
	}))

	req := &request.Request{
		Method:     "GET",
		PathParams: make(map[string]string),
	}
	req.URL, _ = parseURL("/users/123")

	var buf []byte
	w := &mockResponseWriter{buf: &buf}

	router.ServeHTTP(w, req)

	if capturedID != "123" {
		t.Errorf("Expected id=123, got %s", capturedID)
	}
}

func TestWildcardRoutes(t *testing.T) {
	router := New()

	var capturedPath string
	router.GET("/files/*filepath", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		capturedPath = r.PathParams["filepath"]
	}))

	req := &request.Request{
		Method:     "GET",
		PathParams: make(map[string]string),
	}
	req.URL, _ = parseURL("/files/docs/readme.md")

	var buf []byte
	w := &mockResponseWriter{buf: &buf}

	router.ServeHTTP(w, req)

	if capturedPath != "docs/readme.md" {
		t.Errorf("Expected filepath=docs/readme.md, got %s", capturedPath)
	}
}

func TestMethodRouting(t *testing.T) {
	router := New()

	var method string
	handler := HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		method = r.Method
	})

	router.GET("/test", handler)
	router.POST("/test", handler)
	router.PUT("/test", handler)
	router.DELETE("/test", handler)

	testCases := []string{"GET", "POST", "PUT", "DELETE"}

	for _, tc := range testCases {
		req := &request.Request{
			Method:     tc,
			PathParams: make(map[string]string),
		}
		req.URL, _ = parseURL("/test")

		var buf []byte
		w := &mockResponseWriter{buf: &buf}

		router.ServeHTTP(w, req)

		if method != tc {
			t.Errorf("Expected method %s, got %s", tc, method)
		}
	}
}

func TestNotFound(t *testing.T) {
	router := New()

	router.GET("/exists", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {}))

	req := &request.Request{
		Method:     "GET",
		PathParams: make(map[string]string),
	}
	req.URL, _ = parseURL("/notfound")

	var buf []byte
	w := &mockResponseWriter{buf: &buf, statusCode: 200}

	router.ServeHTTP(w, req)

	if w.statusCode != 404 {
		t.Errorf("Expected 404 status, got %d", w.statusCode)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	router := New()

	router.GET("/users", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {}))

	req := &request.Request{
		Method:     "POST",
		PathParams: make(map[string]string),
	}
	req.URL, _ = parseURL("/users")

	var buf []byte
	w := &mockResponseWriter{buf: &buf, statusCode: 200}

	router.ServeHTTP(w, req)

	if w.statusCode != 405 {
		t.Fatalf("Expected 405 status, got %d", w.statusCode)
	}
	if allow := w.Header()["Allow"]; len(allow) != 1 || allow[0] != "GET" {
		t.Fatalf("Expected Allow header GET, got %v", allow)
	}
}

func TestHEADFallsBackToGET(t *testing.T) {
	router := New()

	called := false
	router.GET("/users", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		called = true
	}))

	req := &request.Request{
		Method:     "HEAD",
		PathParams: make(map[string]string),
	}
	req.URL, _ = parseURL("/users")

	var buf []byte
	w := &mockResponseWriter{buf: &buf}

	router.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected HEAD to fall back to GET handler")
	}
}

func TestNestedParamRouteRegistrationAndLookup(t *testing.T) {
	router := New()

	var userID, postID string
	router.GET("/users/:id/posts/:postID", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		userID = r.PathParams["id"]
		postID = r.PathParams["postID"]
	}))

	req := &request.Request{
		Method:     "GET",
		PathParams: make(map[string]string),
	}
	req.URL, _ = parseURL("/users/123/posts/456")

	var buf []byte
	w := &mockResponseWriter{buf: &buf}
	router.ServeHTTP(w, req)

	if userID != "123" || postID != "456" {
		t.Fatalf("expected nested params to be extracted, got id=%q postID=%q", userID, postID)
	}
}

func TestRouteGroups(t *testing.T) {
	router := New()

	api := router.Group("/api")
	v1 := api.Group("/v1")

	var path string
	v1.GET("/users", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		path = r.URL.Path
	}))

	req := &request.Request{
		Method:     "GET",
		PathParams: make(map[string]string),
	}
	req.URL, _ = parseURL("/api/v1/users")

	var buf []byte
	w := &mockResponseWriter{buf: &buf}

	router.ServeHTTP(w, req)

	if path != "/api/v1/users" {
		t.Errorf("Expected path /api/v1/users, got %s", path)
	}
}

func TestMiddlewareExecution(t *testing.T) {
	router := New()

	var executionOrder []string

	middleware1 := func(next Handler) Handler {
		return HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			executionOrder = append(executionOrder, "middleware1-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "middleware1-after")
		})
	}

	middleware2 := func(next Handler) Handler {
		return HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			executionOrder = append(executionOrder, "middleware2-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "middleware2-after")
		})
	}

	router.Use(middleware1)
	router.Use(middleware2)

	router.GET("/test", HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		executionOrder = append(executionOrder, "handler")
	}))

	req := &request.Request{
		Method:     "GET",
		PathParams: make(map[string]string),
	}
	req.URL, _ = parseURL("/test")

	var buf []byte
	w := &mockResponseWriter{buf: &buf}

	router.ServeHTTP(w, req)

	expected := []string{
		"middleware1-before",
		"middleware2-before",
		"handler",
		"middleware2-after",
		"middleware1-after",
	}

	if len(executionOrder) != len(expected) {
		t.Fatalf("Expected %d executions, got %d", len(expected), len(executionOrder))
	}

	for i, exp := range expected {
		if executionOrder[i] != exp {
			t.Errorf("At position %d: expected %s, got %s", i, exp, executionOrder[i])
		}
	}
}

// Mock response writer for testing
type mockResponseWriter struct {
	buf        *[]byte
	statusCode int
	headers    map[string][]string
}

func (m *mockResponseWriter) Header() map[string][]string {
	if m.headers == nil {
		m.headers = make(map[string][]string)
	}
	return m.headers
}

func (m *mockResponseWriter) Write(p []byte) (int, error) {
	*m.buf = append(*m.buf, p...)
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
	return int64(len(*m.buf))
}

func (m *mockResponseWriter) Flush() error {
	return nil
}

// Helper function
func parseURL(path string) (*url.URL, error) {
	return url.Parse(path)
}
