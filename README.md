# HTTP Server - Production-Grade HTTP/1.1 Server from Scratch

A high-performance, production-grade HTTP/1.1 server built from scratch in Go with zero external dependencies for core functionality.

## Features

### Core HTTP Server
- **HTTP/1.1 Protocol**: Full RFC 7230-7235 compliance
- **High Performance**: Optimized for 100k+ req/s throughput
- **Worker Pool**: Configurable worker pool for concurrent request handling
- **Connection Pooling**: Efficient connection reuse with keep-alive support
- **Graceful Shutdown**: Clean shutdown with zero request loss

### Routing
- **Radix Tree Router**: O(log n) route matching with efficient prefix matching
- **Path Parameters**: Extract parameters from URL paths (`:id`, `:name`)
- **Wildcard Routes**: Support for wildcard paths (`*filepath`)
- **HTTP Methods**: Full support for GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS
- **Route Groups**: Organize routes with common prefixes and middleware

### Middleware
- **Logger**: Request/response logging with timing
- **Recovery**: Panic recovery with stack traces
- **CORS**: Cross-Origin Resource Sharing with customizable policies
- **Rate Limiting**: Token bucket rate limiting per IP
- **Authentication**: Basic Auth, Bearer Token, and API Key auth
- **Compression**: Automatic gzip compression

### Advanced Features
- **Static File Server**: Efficient file serving with:
  - ETag caching
  - Range requests (partial content)
  - Directory listing
  - Path traversal protection
- **WebSocket Support**: Full WebSocket protocol implementation
  - Frame parsing/writing
  - Ping/pong heartbeat
  - Message fragmentation
  - Clean close handshake
- **TLS/HTTPS**: Secure connections with TLS 1.2+

### Performance Optimizations
- **Buffer Pooling**: Reusable buffers to reduce GC pressure
- **Zero-Copy**: Efficient file serving using kernel-level operations
- **Chunked Encoding**: Streaming responses for large payloads
- **Efficient Parsing**: Streaming HTTP parser without loading entire request

## Installation

```bash
go get github.com/sanskar/http-server
```

## Validation

Current repository validation includes:
- `go build ./...`
- `go test ./...`
- `go test ./... -race`
- `govulncheck ./...`
- `make bench`
- `make fuzz-smoke`
- `make fuzz-long`
- `make soak-smoke`
- `make soak-long`

Benchmarks live in `tests/benchmark`, fuzz harnesses cover request parsing, router dispatch, and static path validation, and `tests/soak` provides a real network soak suite that exercises `GET`, `HEAD`, param routing, request bodies, gzip responses, and malformed traffic under concurrency.

The module now pins `toolchain go1.26.3` so repository validation runs against a patched Go toolchain rather than relying on the host's default runtime.

Current benchmark snapshot from the repository benchmark suite:
- parser simple GET: about `1.0µs/op`, `5480 B/op`, `19 allocs/op`
- chunked response writer: about `374ns/op`, `731 B/op`, `8 allocs/op`
- compressed response writer: about `27µs/op`, `939 B/op`, `13 allocs/op`

## Quick Start

### Basic Hello World

```go
package main

import (
    "log"
    "github.com/sanskar/http-server/pkg/httpserver"
)

func main() {
    server := httpserver.New()

    server.GET("/", func(w httpserver.ResponseWriter, r *httpserver.Request) {
        w.WriteString("Hello, World!")
    })

    log.Fatal(server.Listen(":8080"))
}
```

### With Middleware

```go
server := httpserver.New()

// Add global middleware
server.Use(httpserver.Logger())
server.Use(httpserver.Recovery())
server.Use(httpserver.CORS())
server.Use(httpserver.RateLimit(100, 200)) // 100 req/s, burst 200

server.GET("/", func(w httpserver.ResponseWriter, r *httpserver.Request) {
    w.WriteString("Hello, World!")
})

server.Listen(":8080")
```

### Path Parameters

```go
server.GET("/users/:id", func(w httpserver.ResponseWriter, r *httpserver.Request) {
    userID := r.PathParams["id"]
    w.WriteString("User ID: " + userID)
})
```

### JSON API

```go
server.POST("/api/users", func(w httpserver.ResponseWriter, r *httpserver.Request) {
    // Read request body
    body := make([]byte, r.ContentLength)
    r.Body.Read(body)

    // Process and respond
    response := `{"status": "success", "id": 123}`
    httpserver.WriteJSON(w, httpserver.StatusCreated, []byte(response))
})
```

### Route Groups

```go
api := server.Group("/api/v1")
api.Use(httpserver.BasicAuth("admin", "secret"))

api.GET("/users", handleGetUsers)
api.POST("/users", handleCreateUser)
api.GET("/users/:id", handleGetUser)
api.PUT("/users/:id", handleUpdateUser)
api.DELETE("/users/:id", handleDeleteUser)
```

### Static Files

```go
// Serve files from ./public directory at /static/*
server.Static("/static", "./public")
```

### WebSocket

```go
server.GET("/ws", func(w httpserver.ResponseWriter, r *httpserver.Request) {
    ws, err := httpserver.UpgradeWebSocket(w, r)
    if err != nil {
        return
    }
    defer ws.Close()

    for {
        message, err := ws.ReadMessage()
        if err != nil {
            break
        }

        // Echo message back
        ws.WriteText(string(message))
    }
})
```

### HTTPS/TLS

```go
server.ListenTLS(":443", "cert.pem", "key.pem")
```

## Configuration

```go
config := &server.Config{
    Addr:               ":8080",
    ReadTimeout:        30 * time.Second,
    WriteTimeout:       30 * time.Second,
    IdleTimeout:        60 * time.Second,
    MaxHeaderBytes:     1 << 20, // 1 MB
    MaxBodyBytes:       10 << 20, // 10 MB
    MaxWorkers:         runtime.NumCPU() * 2,
    QueueSize:          100,
    MaxConnections:     10000,
    EnableKeepAlive:    true,
    EnableCompression:  true,
    CompressionLevel:   6,
}

server := httpserver.NewWithConfig(config)
```

## Middleware

### Built-in Middleware

#### Logger
```go
server.Use(httpserver.Logger())
// Output: [GET] /api/users 192.168.1.1 - 200 (1.234ms)
```

#### Recovery
```go
server.Use(httpserver.Recovery())
// Catches panics and returns 500 Internal Server Error
```

#### CORS
```go
// Default CORS (allows all origins)
server.Use(httpserver.CORS())

// Custom CORS
corsConfig := &middleware.CORSConfig{
    AllowOrigins:     []string{"https://example.com"},
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
    AllowHeaders:     []string{"Content-Type", "Authorization"},
    AllowCredentials: true,
    MaxAge:           3600,
}
server.Use(httpserver.CORSWithConfig(corsConfig))
```

#### Rate Limiting
```go
// 100 requests per second, burst of 200
server.Use(httpserver.RateLimit(100, 200))
```

#### Authentication
```go
// Basic Auth
server.Use(httpserver.BasicAuth("username", "password"))

// Bearer Token
tokens := map[string]bool{
    "secret-token-123": true,
    "another-token":    true,
}
server.Use(httpserver.BearerAuth(tokens))

// API Key
apiKeys := map[string]bool{
    "key-123": true,
    "key-456": true,
}
server.Use(httpserver.APIKeyAuth(apiKeys, "X-API-Key"))
```

#### Compression
```go
// Gzip compression (level 1-9)
server.Use(httpserver.Compression(6))
```

### Custom Middleware

```go
func CustomMiddleware() httpserver.Middleware {
    return func(next httpserver.Handler) httpserver.Handler {
        return httpserver.HandlerFunc(func(w httpserver.ResponseWriter, r *httpserver.Request) {
            // Before request
            start := time.Now()

            // Call next handler
            next.ServeHTTP(w, r)

            // After request
            duration := time.Since(start)
            log.Printf("Request took %v", duration)
        })
    }
}

server.Use(CustomMiddleware())
```

## Examples

See the `examples/` directory for complete examples:

- **basic_usage.go**: Simple Hello World server
- **rest_api.go**: Full REST API with CRUD operations
- **websocket_chat.go**: Real-time WebSocket chat application

### Running Examples

```bash
# Basic usage
go run examples/basic_usage.go

# REST API
go run examples/rest_api.go

# WebSocket chat
go run examples/websocket_chat.go
```

## Architecture

The server is built with a modular architecture:

```
HTTP-Server/
├── internal/
│   ├── request/      # HTTP request parser
│   ├── response/     # HTTP response writer
│   ├── router/       # Radix tree router
│   ├── server/       # Core server & connection handling
│   ├── middleware/   # Built-in middleware
│   ├── websocket/    # WebSocket implementation
│   └── pool/         # Worker & buffer pools
├── pkg/
│   └── httpserver/   # Public API
└── examples/         # Example applications
```

### Request Flow

```
Client → TCP Listener → Worker Pool → Request Parser →
Router → Middleware Chain → Handler → Response Writer →
Client
```

## Performance

Benchmarks on 8-core CPU:

| Operation | Throughput | Latency (p99) |
|-----------|------------|---------------|
| Simple GET | 100k req/s | < 1ms |
| JSON Response | 80k req/s | < 2ms |
| Static File (1KB) | 50k req/s | < 3ms |
| WebSocket Frame | 200k frames/s | < 500µs |

## Security

- **Input Validation**: Request size limits, header validation
- **Path Traversal Prevention**: Safe file path handling
- **Rate Limiting**: DoS protection
- **TLS/HTTPS**: Secure connections
- **Security Headers**: X-Content-Type-Options, X-Frame-Options, etc.

## Testing

The project includes comprehensive tests:

```bash
# Run all tests
go test ./...

# With verbose output
go test ./... -v

# With race detector
go test ./... -race

# Benchmarks
go test -bench=. ./tests/benchmark/
```

## Graceful Shutdown

```go
// Start server in goroutine
go func() {
    if err := server.Listen(":8080"); err != nil {
        log.Fatal(err)
    }
}()

// Wait for interrupt signal
c := make(chan os.Signal, 1)
signal.Notify(c, os.Interrupt, syscall.SIGTERM)
<-c

// Graceful shutdown with 30 second timeout
server.Shutdown(30 * time.Second)
```

## Contributing

Contributions are welcome! This project demonstrates:

- Production-grade HTTP server implementation
- Clean, modular architecture
- Comprehensive testing
- Performance optimization techniques
- Security best practices

## License

MIT License

## Acknowledgments

Built from scratch as a learning project to understand:
- HTTP/1.1 protocol internals
- Network programming in Go
- Concurrent request handling
- Performance optimization
- Production-grade software engineering

---

**Note**: While this server is production-quality in terms of code quality and features, for production use cases, consider using battle-tested servers like the Go standard library's `net/http` package. This project is primarily educational and demonstrates building a complete HTTP server from first principles.
