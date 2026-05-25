package middleware

import (
	"strconv"
	"strings"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
	"github.com/sanskar/http-server/internal/router"
)

// CORSConfig contains CORS configuration
type CORSConfig struct {
	AllowOrigins     []string // Allowed origins (* for all)
	AllowMethods     []string // Allowed HTTP methods
	AllowHeaders     []string // Allowed headers
	ExposeHeaders    []string // Headers exposed to client
	AllowCredentials bool     // Allow credentials
	MaxAge           int      // Preflight cache duration in seconds
}

// DefaultCORSConfig returns default CORS configuration
func DefaultCORSConfig() *CORSConfig {
	return &CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Accept", "Content-Type", "Content-Length", "Authorization"},
		ExposeHeaders:    []string{},
		AllowCredentials: false,
		MaxAge:           3600,
	}
}

// CORS returns a CORS middleware with custom configuration
func CORS(config *CORSConfig) router.Middleware {
	if config == nil {
		config = DefaultCORSConfig()
	}

	allowMethods := strings.Join(config.AllowMethods, ", ")
	allowHeaders := strings.Join(config.AllowHeaders, ", ")
	exposeHeaders := strings.Join(config.ExposeHeaders, ", ")

	return func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			origin := r.GetHeader("Origin")
			isWildcard := len(config.AllowOrigins) == 1 && config.AllowOrigins[0] == "*"

			// Check if origin is allowed
			allowedOrigin, allowed := resolveAllowedOrigin(config, origin)

			if r.Method == "OPTIONS" {
				addVaryHeader(w.Header(), "Origin")
				addVaryHeader(w.Header(), "Access-Control-Request-Method")
				addVaryHeader(w.Header(), "Access-Control-Request-Headers")

				if !allowed || !isAllowedMethod(config.AllowMethods, r.GetHeader("Access-Control-Request-Method")) || !areAllowedHeaders(config.AllowHeaders, r.GetHeader("Access-Control-Request-Headers")) {
					response.WriteError(w, response.StatusForbidden, "CORS preflight forbidden")
					return
				}
			}

			if allowed {
				// Set CORS headers
				if allowedOrigin != "" {
					w.Header()["Access-Control-Allow-Origin"] = []string{allowedOrigin}
				}

				w.Header()["Access-Control-Allow-Methods"] = []string{allowMethods}
				w.Header()["Access-Control-Allow-Headers"] = []string{allowHeaders}
				if !isWildcard || config.AllowCredentials {
					addVaryHeader(w.Header(), "Origin")
				}

				if len(exposeHeaders) > 0 {
					w.Header()["Access-Control-Expose-Headers"] = []string{exposeHeaders}
				}

				if config.AllowCredentials {
					w.Header()["Access-Control-Allow-Credentials"] = []string{"true"}
				}

				if config.MaxAge > 0 {
					w.Header()["Access-Control-Max-Age"] = []string{strconv.Itoa(config.MaxAge)}
				}
			}

			// Handle preflight OPTIONS request
			if r.Method == "OPTIONS" {
				w.WriteHeader(response.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func resolveAllowedOrigin(config *CORSConfig, origin string) (string, bool) {
	if origin == "" {
		return "", false
	}

	if len(config.AllowOrigins) == 1 && config.AllowOrigins[0] == "*" {
		if config.AllowCredentials {
			return origin, true
		}
		return "*", true
	}

	for _, allowedOrigin := range config.AllowOrigins {
		if allowedOrigin == origin {
			return origin, true
		}
	}

	return "", false
}

func isAllowedMethod(allowedMethods []string, requested string) bool {
	for _, method := range allowedMethods {
		if strings.EqualFold(method, requested) {
			return true
		}
	}
	return false
}

func areAllowedHeaders(allowedHeaders []string, requested string) bool {
	if strings.TrimSpace(requested) == "" {
		return true
	}

	allowed := make(map[string]struct{}, len(allowedHeaders))
	for _, header := range allowedHeaders {
		allowed[strings.ToLower(strings.TrimSpace(header))] = struct{}{}
	}

	for _, header := range strings.Split(requested, ",") {
		normalized := strings.ToLower(strings.TrimSpace(header))
		if normalized == "" {
			continue
		}
		if _, ok := allowed[normalized]; !ok {
			return false
		}
	}

	return true
}

func addVaryHeader(headers map[string][]string, value string) {
	for _, existing := range headers["Vary"] {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}

	headers["Vary"] = append(headers["Vary"], value)
}
