package httpserver

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/sanskarpan/http-server/internal/middleware"
	"github.com/sanskarpan/http-server/internal/pool"
	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
	"github.com/sanskarpan/http-server/internal/router"
	"github.com/sanskarpan/http-server/internal/server"
	"github.com/sanskarpan/http-server/internal/websocket"
)

// Server is the HTTP server
type Server struct {
	router *router.Router
	server *server.Server
	config *server.Config
}

// Request is an HTTP request
type Request = request.Request

// ResponseWriter is an HTTP response writer
type ResponseWriter = response.ResponseWriter

// Handler is an HTTP handler
type Handler = router.Handler

// HandlerFunc is an HTTP handler function
type HandlerFunc = router.HandlerFunc

// Middleware is a middleware function
type Middleware = router.Middleware

// WebSocket is a WebSocket connection
type WebSocket = websocket.WebSocket

// Config is the public server configuration type.
type Config = server.Config

// StaticConfig is the public static file server configuration type.
type StaticConfig = server.StaticConfig

// CORSConfig is the public CORS configuration type.
type CORSConfig = middleware.CORSConfig

// WorkerPoolStats exposes worker-pool telemetry.
type WorkerPoolStats = pool.PoolStats

// ServerStats exposes server telemetry.
type ServerStats struct {
	ActiveConnections int64
	TotalConnections  int64
	TotalRequests     int64
	WorkerPoolStats   WorkerPoolStats
}

// New creates a new HTTP server
func New() *Server {
	return NewWithConfig(nil)
}

// DefaultConfig returns the default server configuration.
func DefaultConfig() *Config {
	return server.DefaultConfig()
}

// DefaultStaticConfig returns the default static-file configuration.
func DefaultStaticConfig(root string) *StaticConfig {
	return server.DefaultStaticConfig(root)
}

// DefaultCORSConfig returns the default CORS configuration.
func DefaultCORSConfig() *CORSConfig {
	return middleware.DefaultCORSConfig()
}

// NewWithConfig creates a new HTTP server with custom configuration
func NewWithConfig(config *Config) *Server {
	if config == nil {
		config = server.DefaultConfig()
	}

	r := router.New()

	s := &Server{
		router: r,
		config: config,
	}

	return s
}

// Use adds global middleware
func (s *Server) Use(middleware Middleware) {
	s.router.Use(middleware)
}

// GET registers a GET handler
func (s *Server) GET(path string, handler interface{}) {
	s.router.GET(path, convertHandler(handler))
}

// POST registers a POST handler
func (s *Server) POST(path string, handler interface{}) {
	s.router.POST(path, convertHandler(handler))
}

// PUT registers a PUT handler
func (s *Server) PUT(path string, handler interface{}) {
	s.router.PUT(path, convertHandler(handler))
}

// DELETE registers a DELETE handler
func (s *Server) DELETE(path string, handler interface{}) {
	s.router.DELETE(path, convertHandler(handler))
}

// PATCH registers a PATCH handler
func (s *Server) PATCH(path string, handler interface{}) {
	s.router.PATCH(path, convertHandler(handler))
}

// HEAD registers a HEAD handler
func (s *Server) HEAD(path string, handler interface{}) {
	s.router.HEAD(path, convertHandler(handler))
}

// OPTIONS registers an OPTIONS handler
func (s *Server) OPTIONS(path string, handler interface{}) {
	s.router.OPTIONS(path, convertHandler(handler))
}

// Handle registers a handler for any method
func (s *Server) Handle(method, path string, handler interface{}) {
	s.router.Handle(method, path, convertHandler(handler))
}

// Group creates a route group
func (s *Server) Group(prefix string) *Group {
	return &Group{
		group: s.router.Group(prefix),
	}
}

// Static serves static files from a directory
func (s *Server) Static(urlPath, dirPath string) {
	config := server.DefaultStaticConfig(dirPath)
	handler := server.StaticFileServer(config)

	// Register with wildcard
	s.router.GET(urlPath+"/*filepath", handler)
}

// StaticFS serves static files with custom configuration
func (s *Server) StaticFS(urlPath string, config *StaticConfig) {
	handler := server.StaticFileServer(config)
	s.router.GET(urlPath+"/*filepath", handler)
}

// Listen starts the server
func (s *Server) Listen(addr string) error {
	s.config.Addr = addr
	return s.start()
}

// ListenTLS starts the server with TLS
func (s *Server) ListenTLS(addr, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}

	s.config.Addr = addr
	s.config.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	return s.start()
}

// start starts the server
func (s *Server) start() error {
	// Create internal server
	srv, err := server.New(s.config, s.router)
	if err != nil {
		return err
	}

	s.server = srv

	return srv.Start()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(timeout time.Duration) error {
	if s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.server.Shutdown(ctx)
}

// SetConfig sets server configuration
func (s *Server) SetConfig(config *Config) {
	s.config = config
}

// Stats returns a point-in-time snapshot of server metrics.
func (s *Server) Stats() ServerStats {
	if s.server == nil {
		return ServerStats{}
	}

	stats := s.server.Stats()
	return ServerStats{
		ActiveConnections: stats.ActiveConnections,
		TotalConnections:  stats.TotalConnections,
		TotalRequests:     stats.TotalRequests,
		WorkerPoolStats:   stats.WorkerPoolStats,
	}
}

// Group represents a route group
type Group struct {
	group *router.Group
}

// Use adds middleware to the group
func (g *Group) Use(middleware Middleware) {
	g.group.Use(middleware)
}

// GET registers a GET handler in the group
func (g *Group) GET(path string, handler interface{}) {
	g.group.GET(path, convertHandler(handler))
}

// POST registers a POST handler in the group
func (g *Group) POST(path string, handler interface{}) {
	g.group.POST(path, convertHandler(handler))
}

// PUT registers a PUT handler in the group
func (g *Group) PUT(path string, handler interface{}) {
	g.group.PUT(path, convertHandler(handler))
}

// DELETE registers a DELETE handler in the group
func (g *Group) DELETE(path string, handler interface{}) {
	g.group.DELETE(path, convertHandler(handler))
}

// PATCH registers a PATCH handler in the group
func (g *Group) PATCH(path string, handler interface{}) {
	g.group.PATCH(path, convertHandler(handler))
}

// HEAD registers a HEAD handler in the group
func (g *Group) HEAD(path string, handler interface{}) {
	g.group.HEAD(path, convertHandler(handler))
}

// OPTIONS registers an OPTIONS handler in the group
func (g *Group) OPTIONS(path string, handler interface{}) {
	g.group.OPTIONS(path, convertHandler(handler))
}

// Group creates a sub-group
func (g *Group) Group(prefix string) *Group {
	return &Group{
		group: g.group.Group(prefix),
	}
}

// convertHandler converts a handler function to Handler interface
func convertHandler(handler interface{}) Handler {
	switch h := handler.(type) {
	case Handler:
		return h
	case func(ResponseWriter, *Request):
		return HandlerFunc(h)
	default:
		panic("invalid handler type")
	}
}

// Middleware functions

// Logger returns a logging middleware
func Logger() Middleware {
	return middleware.Logger()
}

// Recovery returns a panic recovery middleware
func Recovery() Middleware {
	return middleware.Recovery()
}

// CORS returns a CORS middleware with default config
func CORS() Middleware {
	return middleware.CORS(nil)
}

// CORSWithConfig returns a CORS middleware with custom config
func CORSWithConfig(config *CORSConfig) Middleware {
	return middleware.CORS(config)
}

// RateLimit returns a rate limiting middleware
func RateLimit(requestsPerSecond float64, burst int) Middleware {
	return middleware.RateLimit(requestsPerSecond, burst)
}

// BasicAuth returns a basic authentication middleware
func BasicAuth(username, password string) Middleware {
	return middleware.BasicAuth(username, password)
}

// BearerAuth returns a bearer token authentication middleware
func BearerAuth(validTokens map[string]bool) Middleware {
	return middleware.BearerAuth(validTokens)
}

// APIKeyAuth returns an API key authentication middleware
func APIKeyAuth(validKeys map[string]bool, headerName string) Middleware {
	return middleware.APIKeyAuth(validKeys, headerName)
}

// Compression returns a compression middleware
func Compression(level int) Middleware {
	return middleware.Compression(level)
}

// UpgradeWebSocket upgrades an HTTP connection to WebSocket
func UpgradeWebSocket(w ResponseWriter, r *Request) (*WebSocket, error) {
	return websocket.Upgrade(w, r)
}

// Helper functions for responses

// WriteJSON writes a JSON response
func WriteJSON(w ResponseWriter, statusCode int, data []byte) error {
	return response.WriteJSON(w, statusCode, data)
}

// WriteHTML writes an HTML response
func WriteHTML(w ResponseWriter, statusCode int, html string) error {
	return response.WriteHTML(w, statusCode, html)
}

// WriteError writes an error response
func WriteError(w ResponseWriter, statusCode int, message string) error {
	return response.WriteError(w, statusCode, message)
}

// Redirect writes a redirect response
func Redirect(w ResponseWriter, url string, statusCode int) error {
	return response.Redirect(w, url, statusCode)
}

// Status codes
const (
	StatusOK                  = response.StatusOK
	StatusCreated             = response.StatusCreated
	StatusAccepted            = response.StatusAccepted
	StatusNoContent           = response.StatusNoContent
	StatusMovedPermanently    = response.StatusMovedPermanently
	StatusFound               = response.StatusFound
	StatusSeeOther            = response.StatusSeeOther
	StatusNotModified         = response.StatusNotModified
	StatusBadRequest          = response.StatusBadRequest
	StatusUnauthorized        = response.StatusUnauthorized
	StatusForbidden           = response.StatusForbidden
	StatusNotFound            = response.StatusNotFound
	StatusMethodNotAllowed    = response.StatusMethodNotAllowed
	StatusTooManyRequests     = response.StatusTooManyRequests
	StatusInternalServerError = response.StatusInternalServerError
	StatusNotImplemented      = response.StatusNotImplemented
	StatusServiceUnavailable  = response.StatusServiceUnavailable
)
