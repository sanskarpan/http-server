package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sanskarpan/http-server/internal/pool"
	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
)

// Handler is the interface for handling HTTP requests
type Handler interface {
	ServeHTTP(w response.ResponseWriter, r *request.Request)
}

// HandlerFunc is an adapter to allow ordinary functions to be used as handlers
type HandlerFunc func(w response.ResponseWriter, r *request.Request)

// ServeHTTP calls f(w, r)
func (f HandlerFunc) ServeHTTP(w response.ResponseWriter, r *request.Request) {
	f(w, r)
}

// Server is an HTTP server
type Server struct {
	config  *Config
	handler Handler

	listener      net.Listener
	workerPool    *pool.WorkerPool
	requestParser *request.Parser

	// Lifecycle management
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	shutdownCh chan struct{}
	running    atomic.Bool

	// Connection tracking
	activeConnections atomic.Int64
	totalConnections  atomic.Int64
	totalRequests     atomic.Int64
	connections       map[net.Conn]struct{}
	connMu            sync.Mutex

	// Middleware
	middleware []Middleware

	mu sync.RWMutex
}

// Middleware is a function that wraps a handler
type Middleware func(Handler) Handler

// New creates a new HTTP server with the provided configuration
func New(config *Config, handler Handler) (*Server, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if handler == nil {
		return nil, errors.New("handler cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create request parser
	parser := request.NewParser()
	parser.SetMaxHeaderSize(int64(config.MaxHeaderBytes))
	parser.SetMaxBodySize(config.MaxBodyBytes)

	server := &Server{
		config:        config,
		handler:       handler,
		requestParser: parser,
		ctx:           ctx,
		cancel:        cancel,
		shutdownCh:    make(chan struct{}),
		middleware:    make([]Middleware, 0),
		connections:   make(map[net.Conn]struct{}),
	}

	server.running.Store(false)
	server.activeConnections.Store(0)
	server.totalConnections.Store(0)
	server.totalRequests.Store(0)

	return server, nil
}

// Use adds a middleware to the server
func (s *Server) Use(middleware Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.middleware = append(s.middleware, middleware)
}

// Start starts the HTTP server
func (s *Server) Start() error {
	if s.running.Load() {
		return errors.New("server is already running")
	}

	// Create listener
	var listener net.Listener
	var err error

	if s.config.TLS != nil {
		listener, err = tls.Listen("tcp", s.config.Addr, s.config.TLS)
	} else {
		listener, err = net.Listen("tcp", s.config.Addr)
	}

	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	s.listener = listener
	s.running.Store(true)

	// Start worker pool
	s.workerPool = pool.NewWorkerPool(s.config.MaxWorkers, s.config.QueueSize)
	s.workerPool.Start()

	log.Printf("Server listening on %s", s.config.Addr)

	// Start accepting connections
	s.wg.Add(1)
	s.acceptConnections()
	return nil
}

// acceptConnections accepts incoming connections
func (s *Server) acceptConnections() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.shutdownCh:
				return
			default:
				// Log error and continue
				log.Printf("Accept error: %v", err)
				continue
			}
		}

		// Check connection limit
		if s.activeConnections.Load() >= int64(s.config.MaxConnections) {
			log.Printf("Max connections reached, rejecting connection")
			conn.Close()
			continue
		}

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection handles a single connection
func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()

	s.activeConnections.Add(1)
	s.totalConnections.Add(1)
	defer s.activeConnections.Add(-1)
	s.trackConnection(conn)
	defer s.untrackConnection(conn)

	defer conn.Close()

	// Set initial read timeout
	conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))

	reader := bufio.NewReader(conn)

	for {
		// Check if server is shutting down
		select {
		case <-s.shutdownCh:
			return
		default:
		}

		// Parse request
		req, err := s.requestParser.Parse(reader, conn.RemoteAddr().String())
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
				return
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return
			}
			select {
			case <-s.shutdownCh:
				return
			default:
			}
			s.sendBadRequest(conn, err.Error())
			return
		}

		s.totalRequests.Add(1)

		// Set write timeout
		conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))

		// Create response writer
		writer := response.NewWriter(conn)
		if req.Method == "HEAD" {
			writer.SuppressBody()
		}

		// Enable compression if configured
		if s.config.EnableCompression {
			acceptEncoding := req.GetHeader("Accept-Encoding")
			if req.Method != "HEAD" && contains(acceptEncoding, "gzip") {
				writer.EnableCompression(s.config.CompressionLevel)
			}
		}

		// Apply middleware and handle request
		handler := s.handler
		for i := len(s.middleware) - 1; i >= 0; i-- {
			handler = s.middleware[i](handler)
		}

		done := make(chan struct{})
		submitted := s.workerPool.Submit(func() {
			defer close(done)
			s.serveRequest(handler, writer, req)
		})
		if !submitted {
			_ = response.WriteError(writer, response.StatusServiceUnavailable, "Service Unavailable")
			if err := writer.Close(); err != nil {
				log.Printf("Error flushing service unavailable response: %v", err)
			}
			return
		}

		select {
		case <-done:
		case <-s.shutdownCh:
			return
		}

		// Finalize response
		if err := writer.Close(); err != nil {
			log.Printf("Error flushing response: %v", err)
			return
		}

		// Check if connection should be kept alive
		connectionHeader := req.GetHeader("Connection")
		if !s.config.EnableKeepAlive ||
			contains(connectionHeader, "close") ||
			req.ProtoMajor < 1 ||
			(req.ProtoMajor == 1 && req.ProtoMinor < 1) {
			return
		}

		// Set idle timeout for keep-alive
		conn.SetReadDeadline(time.Now().Add(s.config.IdleTimeout))
	}
}

// sendBadRequest sends a 400 Bad Request response
func (s *Server) sendBadRequest(conn net.Conn, message string) {
	resp := fmt.Sprintf("HTTP/1.1 400 Bad Request\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n%s\n", message)
	conn.Write([]byte(resp))
}

// sendServiceUnavailable sends a 503 Service Unavailable response
func (s *Server) sendServiceUnavailable(conn net.Conn) {
	resp := "HTTP/1.1 503 Service Unavailable\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nService Unavailable\n"
	conn.Write([]byte(resp))
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if !s.running.Load() {
		return errors.New("server is not running")
	}

	log.Println("Shutting down server...")

	// Signal shutdown
	close(s.shutdownCh)
	s.running.Store(false)

	// Close listener to stop accepting new connections
	if err := s.listener.Close(); err != nil {
		log.Printf("Error closing listener: %v", err)
	}
	s.interruptConnections()

	// Wait for all connections to complete or timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		if s.workerPool != nil {
			s.workerPool.Shutdown()
		}
		close(done)
	}()

	select {
	case <-ctx.Done():
		s.cancel()
		return ctx.Err()
	case <-done:
		log.Println("Server shut down gracefully")
		return nil
	}
}

// Stats returns server statistics
func (s *Server) Stats() ServerStats {
	stats := ServerStats{
		ActiveConnections: s.activeConnections.Load(),
		TotalConnections:  s.totalConnections.Load(),
		TotalRequests:     s.totalRequests.Load(),
	}

	if s.workerPool != nil {
		stats.WorkerPoolStats = s.workerPool.Stats()
	}

	return stats
}

// ServerStats contains server statistics
type ServerStats struct {
	ActiveConnections int64
	TotalConnections  int64
	TotalRequests     int64
	WorkerPoolStats   pool.PoolStats
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func (s *Server) serveRequest(handler Handler, writer response.ResponseWriter, req *request.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("Request handler panic: %v\n%s", recovered, debug.Stack())

			baseWriter, ok := response.Unwrap(writer).(*response.Writer)
			if ok && !baseWriter.HeaderWritten() {
				_ = response.WriteError(writer, response.StatusInternalServerError, "Internal Server Error")
			}
		}
	}()

	handler.ServeHTTP(writer, req)
}

func (s *Server) trackConnection(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.connections[conn] = struct{}{}
}

func (s *Server) untrackConnection(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	delete(s.connections, conn)
}

func (s *Server) interruptConnections() {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	for conn := range s.connections {
		_ = conn.SetReadDeadline(time.Now())
	}
}
