package middleware

import (
	"strings"
	"testing"

	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
	"github.com/sanskarpan/http-server/internal/router"
)

func TestCORSWildcardWithCredentialsReflectsOrigin(t *testing.T) {
	middleware := CORS(&CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           600,
	})

	called := false
	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		called = true
	})

	req := &request.Request{
		Method: "GET",
		Headers: map[string][]string{
			"Origin": {"https://example.com"},
		},
	}

	w := newMockResponseWriter()
	middleware(handler).ServeHTTP(w, req)

	if !called {
		t.Fatal("expected wrapped handler to be called")
	}
	if got := w.Header()["Access-Control-Allow-Origin"]; len(got) != 1 || got[0] != "https://example.com" {
		t.Fatalf("expected reflected origin, got %v", got)
	}
	if got := w.Header()["Vary"]; len(got) == 0 || !strings.Contains(strings.Join(got, ","), "Origin") {
		t.Fatalf("expected Vary: Origin, got %v", got)
	}
}

func TestCORSPreflightRejectsDisallowedOrigin(t *testing.T) {
	middleware := CORS(&CORSConfig{
		AllowOrigins: []string{"https://allowed.example"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Content-Type"},
	})

	req := &request.Request{
		Method: "OPTIONS",
		Headers: map[string][]string{
			"Origin":                         {"https://blocked.example"},
			"Access-Control-Request-Method":  {"POST"},
			"Access-Control-Request-Headers": {"Content-Type"},
		},
	}

	w := newMockResponseWriter()
	middleware(router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {})).ServeHTTP(w, req)

	if w.Status() != response.StatusForbidden {
		t.Fatalf("expected 403 for disallowed origin, got %d", w.Status())
	}
}

func TestCORSPreflightRejectsDisallowedHeaders(t *testing.T) {
	middleware := CORS(&CORSConfig{
		AllowOrigins: []string{"https://allowed.example"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Content-Type"},
	})

	req := &request.Request{
		Method: "OPTIONS",
		Headers: map[string][]string{
			"Origin":                         {"https://allowed.example"},
			"Access-Control-Request-Method":  {"POST"},
			"Access-Control-Request-Headers": {"Authorization"},
		},
	}

	w := newMockResponseWriter()
	middleware(router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {})).ServeHTTP(w, req)

	if w.Status() != response.StatusForbidden {
		t.Fatalf("expected 403 for disallowed headers, got %d", w.Status())
	}
}
