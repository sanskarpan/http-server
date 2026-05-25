package middleware

import (
	"log"
	"time"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
	"github.com/sanskar/http-server/internal/router"
)

// Logger returns a logging middleware
func Logger() router.Middleware {
	return func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			start := time.Now()

			// Create a response wrapper to capture status code
			wrapper := &responseWrapper{
				ResponseWriter: w,
				statusCode:     200,
			}

			// Call next handler
			next.ServeHTTP(wrapper, r)

			// Log request
			duration := time.Since(start)
			log.Printf("[%s] %s %s - %d (%s)",
				r.Method,
				r.URL.Path,
				r.RemoteAddr,
				wrapper.statusCode,
				duration,
			)
		})
	}
}

// responseWrapper wraps a ResponseWriter to capture status code
type responseWrapper struct {
	response.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code
func (w *responseWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the underlying response writer for transport-level features.
func (w *responseWrapper) Unwrap() response.ResponseWriter {
	return w.ResponseWriter
}
