package middleware

import (
	"strings"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
	"github.com/sanskar/http-server/internal/router"
)

// Compression returns a compression middleware
func Compression(level int) router.Middleware {
	if level < 1 || level > 9 {
		level = 6 // Default compression level
	}

	return func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			if r.Method == "HEAD" {
				next.ServeHTTP(w, r)
				return
			}

			// Check if client accepts gzip
			acceptEncoding := r.GetHeader("Accept-Encoding")
			if !strings.Contains(acceptEncoding, "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			// Enable compression on response writer
			if writer, ok := response.Unwrap(w).(response.CompressionEnabler); ok {
				_ = writer.EnableCompression(level)
			}

			next.ServeHTTP(w, r)
		})
	}
}
