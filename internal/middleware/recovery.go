package middleware

import (
	"log"
	"runtime/debug"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
	"github.com/sanskar/http-server/internal/router"
)

// Recovery returns a panic recovery middleware
func Recovery() router.Middleware {
	return func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log panic with stack trace
					log.Printf("PANIC: %v\n%s", err, debug.Stack())

					// Send 500 Internal Server Error
					w.Header()["Content-Type"] = []string{"text/plain"}
					w.WriteHeader(response.StatusInternalServerError)
					w.WriteString("Internal Server Error\n")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
