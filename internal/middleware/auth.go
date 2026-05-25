package middleware

import (
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
	"github.com/sanskar/http-server/internal/router"
)

// BasicAuth returns a basic authentication middleware
func BasicAuth(username, password string) router.Middleware {
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

	return func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			auth := r.GetHeader("Authorization")

			if subtle.ConstantTimeCompare([]byte(auth), []byte(expectedAuth)) != 1 {
				w.Header()["WWW-Authenticate"] = []string{`Basic realm="Restricted"`}
				w.Header()["Content-Type"] = []string{"text/plain"}
				w.WriteHeader(response.StatusUnauthorized)
				w.WriteString("Unauthorized\n")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// BearerAuth returns a bearer token authentication middleware
func BearerAuth(validTokens map[string]bool) router.Middleware {
	return func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			auth := r.GetHeader("Authorization")

			if !strings.HasPrefix(auth, "Bearer ") {
				w.Header()["Content-Type"] = []string{"text/plain"}
				w.WriteHeader(response.StatusUnauthorized)
				w.WriteString("Unauthorized: Bearer token required\n")
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")

			if !containsSecret(validTokens, token) {
				w.Header()["Content-Type"] = []string{"text/plain"}
				w.WriteHeader(response.StatusUnauthorized)
				w.WriteString("Unauthorized: Invalid token\n")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// APIKeyAuth returns an API key authentication middleware
func APIKeyAuth(validKeys map[string]bool, headerName string) router.Middleware {
	if headerName == "" {
		headerName = "X-API-Key"
	}

	return func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			apiKey := r.GetHeader(headerName)

			if apiKey == "" || !containsSecret(validKeys, apiKey) {
				w.Header()["Content-Type"] = []string{"text/plain"}
				w.WriteHeader(response.StatusUnauthorized)
				w.WriteString("Unauthorized: Invalid API key\n")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func containsSecret(validSecrets map[string]bool, candidate string) bool {
	for secret, enabled := range validSecrets {
		if !enabled {
			continue
		}

		if subtle.ConstantTimeCompare([]byte(secret), []byte(candidate)) == 1 {
			return true
		}
	}

	return false
}
