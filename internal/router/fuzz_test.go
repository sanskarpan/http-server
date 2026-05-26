package router

import (
	"net/url"
	"testing"

	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
)

func FuzzRouterServeHTTP(f *testing.F) {
	f.Add("GET", "/")
	f.Add("HEAD", "/users/123")
	f.Add("POST", "/files/docs/readme.md")
	f.Add("OPTIONS", "/api/v1/users")

	f.Fuzz(func(t *testing.T, method, path string) {
		if len(method) > 16 || len(path) > 2048 {
			t.Skip()
		}

		r := New()
		handler := HandlerFunc(func(w response.ResponseWriter, req *request.Request) {
			_, _ = w.WriteString("ok")
		})
		r.GET("/", handler)
		r.GET("/users/:id", handler)
		r.POST("/files/*filepath", handler)
		r.OPTIONS("/api/v1/users", handler)

		if path == "" {
			path = "/"
		}
		parsedURL, err := url.Parse(path)
		if err != nil {
			return
		}

		req := &request.Request{
			Method:     method,
			URL:        parsedURL,
			PathParams: make(map[string]string),
		}

		var buf []byte
		w := &mockResponseWriter{buf: &buf}
		r.ServeHTTP(w, req)
	})
}
