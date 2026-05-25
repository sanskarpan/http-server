package server

import (
	"crypto/tls"
	"runtime"
	"time"
)

// Config contains server configuration
type Config struct {
	// Address to listen on (e.g., ":8080", "localhost:3000")
	Addr string

	// TLS configuration for HTTPS
	TLS *tls.Config

	// Timeouts
	ReadTimeout    time.Duration // Maximum duration for reading request
	WriteTimeout   time.Duration // Maximum duration for writing response
	IdleTimeout    time.Duration // Maximum duration for keep-alive connections
	ShutdownTimeout time.Duration // Maximum duration for graceful shutdown

	// Limits
	MaxHeaderBytes int   // Maximum size of request headers
	MaxBodyBytes   int64 // Maximum size of request body

	// Worker pool configuration
	MaxWorkers  int // Maximum number of concurrent workers
	QueueSize   int // Size of the job queue

	// Connection pool
	MaxConnections    int // Maximum number of concurrent connections
	MaxIdleConnections int // Maximum number of idle keep-alive connections

	// Buffer pool sizes
	ReadBufferSize  int
	WriteBufferSize int

	// Feature flags
	EnableKeepAlive   bool
	EnableCompression bool
	CompressionLevel  int // 1-9, where 9 is best compression
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	numCPU := runtime.NumCPU()

	return &Config{
		Addr: ":8080",

		// Timeouts
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 30 * time.Second,

		// Limits
		MaxHeaderBytes: 1 << 20, // 1 MB
		MaxBodyBytes:   10 << 20, // 10 MB

		// Worker pool
		MaxWorkers: numCPU * 2,
		QueueSize:  numCPU * 4,

		// Connection limits
		MaxConnections:    10000,
		MaxIdleConnections: 100,

		// Buffer sizes
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,

		// Features
		EnableKeepAlive:   true,
		EnableCompression: true,
		CompressionLevel:  6,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Addr == "" {
		c.Addr = ":8080"
	}

	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 30 * time.Second
	}

	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 30 * time.Second
	}

	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 60 * time.Second
	}

	if c.MaxHeaderBytes <= 0 {
		c.MaxHeaderBytes = 1 << 20
	}

	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = 10 << 20
	}

	if c.MaxWorkers <= 0 {
		c.MaxWorkers = runtime.NumCPU() * 2
	}

	if c.QueueSize <= 0 {
		c.QueueSize = c.MaxWorkers * 2
	}

	if c.MaxConnections <= 0 {
		c.MaxConnections = 10000
	}

	if c.CompressionLevel < 1 || c.CompressionLevel > 9 {
		c.CompressionLevel = 6
	}

	return nil
}
