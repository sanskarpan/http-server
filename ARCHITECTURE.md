# HTTP Server Architecture - Comprehensive Design Document

## Table of Contents
1. [System Overview](#system-overview)
2. [Core Architecture](#core-architecture)
3. [Component Design](#component-design)
4. [Concurrency Model](#concurrency-model)
5. [Data Flow](#data-flow)
6. [Performance Strategy](#performance-strategy)
7. [Security Considerations](#security-considerations)
8. [Testing Strategy](#testing-strategy)
9. [Implementation Phases](#implementation-phases)

---

## 1. System Overview

### Vision
Build a production-grade HTTP/1.1 server from scratch in Go that:
- Handles 10,000+ concurrent connections
- Supports all standard HTTP features
- Provides extensible middleware architecture
- Achieves sub-millisecond response times
- Zero external dependencies for core functionality

### Key Features
- **HTTP/1.1 Protocol**: Full RFC 7230-7235 compliance
- **TLS/HTTPS**: Secure connections with TLS 1.2+
- **WebSocket**: Upgrade support (RFC 6455)
- **Keep-Alive**: Connection reuse
- **Compression**: Gzip, deflate support
- **Static Files**: Efficient file serving with caching
- **Middleware**: Pluggable middleware chain
- **Routing**: Pattern-based URL routing with path parameters
- **Rate Limiting**: Token bucket algorithm
- **Graceful Shutdown**: Zero request loss on shutdown

---

## 2. Core Architecture

### 2.1 Layered Architecture

```
┌─────────────────────────────────────────────────────┐
│                  Application Layer                   │
│         (User Handlers, Business Logic)              │
└─────────────────────────────────────────────────────┘
                         ↕
┌─────────────────────────────────────────────────────┐
│                  Middleware Layer                    │
│   (Logging, Auth, CORS, Rate Limiting, Compression) │
└─────────────────────────────────────────────────────┘
                         ↕
┌─────────────────────────────────────────────────────┐
│                   Router Layer                       │
│          (URL Matching, Path Parameters)             │
└─────────────────────────────────────────────────────┘
                         ↕
┌─────────────────────────────────────────────────────┐
│                  Protocol Layer                      │
│         (Request Parser, Response Writer)            │
└─────────────────────────────────────────────────────┘
                         ↕
┌─────────────────────────────────────────────────────┐
│                 Connection Layer                     │
│       (TCP/TLS Sockets, Connection Pooling)          │
└─────────────────────────────────────────────────────┘
                         ↕
┌─────────────────────────────────────────────────────┐
│                  Transport Layer                     │
│                   (TCP/IP Stack)                     │
└─────────────────────────────────────────────────────┘
```

### 2.2 Component Diagram

```
                    ┌──────────────┐
                    │    Server    │
                    │   (Listen)   │
                    └──────┬───────┘
                           │
              ┌────────────┴────────────┐
              │                         │
         ┌────▼─────┐           ┌──────▼──────┐
         │ Acceptor │           │   Worker    │
         │  (TCP)   │           │    Pool     │
         └────┬─────┘           └──────┬──────┘
              │                        │
              │                 ┌──────▼──────┐
              │                 │   Worker    │
              │                 │  Goroutine  │
              │                 └──────┬──────┘
              │                        │
         ┌────▼─────────────────────────▼─────┐
         │         Connection Handler          │
         │  ┌──────────────────────────────┐  │
         │  │    Request Parser            │  │
         │  └──────────┬───────────────────┘  │
         │             │                       │
         │  ┌──────────▼───────────────────┐  │
         │  │      Middleware Chain        │  │
         │  │  ┌────────────────────────┐  │  │
         │  │  │ Logger → Auth → CORS   │  │  │
         │  │  │  → RateLimit → Gzip    │  │  │
         │  │  └────────┬───────────────┘  │  │
         │  └───────────┼──────────────────┘  │
         │              │                      │
         │  ┌───────────▼──────────────────┐  │
         │  │         Router               │  │
         │  │  (Pattern Matching)          │  │
         │  └───────────┬──────────────────┘  │
         │              │                      │
         │  ┌───────────▼──────────────────┐  │
         │  │        Handler               │  │
         │  │    (User Code)               │  │
         │  └───────────┬──────────────────┘  │
         │              │                      │
         │  ┌───────────▼──────────────────┐  │
         │  │    Response Writer           │  │
         │  └──────────────────────────────┘  │
         └────────────────────────────────────┘
```

---

## 3. Component Design

### 3.1 Server Component

**Responsibilities:**
- Accept incoming connections
- Manage connection lifecycle
- Graceful shutdown coordination
- TLS certificate management

**Key Structures:**
```go
type Server struct {
    Addr           string
    Handler        Handler
    TLSConfig      *tls.Config
    ReadTimeout    time.Duration
    WriteTimeout   time.Duration
    IdleTimeout    time.Duration
    MaxHeaderBytes int

    listener       net.Listener
    workerPool     *WorkerPool
    shutdownCh     chan struct{}
    wg             sync.WaitGroup
    mu             sync.RWMutex

    middleware     []Middleware
    router         *Router
}
```

**Design Decisions:**
- Use `net.Listener` interface for flexibility (TCP/Unix socket)
- Worker pool to limit concurrent goroutines
- Graceful shutdown with WaitGroup tracking
- Configurable timeouts prevent resource exhaustion

### 3.2 Request Parser

**Responsibilities:**
- Parse HTTP/1.1 request line
- Parse headers (RFC 7230 compliant)
- Parse body (chunked/content-length)
- Validate protocol compliance
- Handle malformed requests

**Key Algorithms:**
1. **Streaming Parser**: Parse headers line-by-line without loading entire request
2. **Zero-Copy Body**: Use io.Reader interface, avoid buffering large bodies
3. **Header Validation**: Reject requests exceeding size limits early

**Error Handling:**
- 400 Bad Request: Malformed syntax
- 413 Payload Too Large: Body/headers exceed limits
- 414 URI Too Long: Request URI exceeds limits
- 505 HTTP Version Not Supported: Non-HTTP/1.x requests

### 3.3 Response Writer

**Responsibilities:**
- Build HTTP responses
- Handle status codes
- Manage headers
- Support chunked transfer encoding
- Compression (gzip/deflate)

**Key Structures:**
```go
type ResponseWriter struct {
    conn           net.Conn
    statusCode     int
    headers        map[string][]string
    bodyWriter     io.Writer
    headerWritten  bool
    contentLength  int64

    compressionLevel int
    compressor      io.WriteCloser
}
```

**Design Decisions:**
- Lazy header writing (only when body is written)
- Support chunked encoding for streaming responses
- Automatic Content-Length calculation when possible
- Compression negotiation via Accept-Encoding

### 3.4 Router

**Responsibilities:**
- URL pattern matching
- Path parameter extraction
- HTTP method routing
- Route priority/ordering

**Matching Algorithm:**
```
Priority (highest to lowest):
1. Exact static routes: /users/profile
2. Parametric routes:   /users/:id
3. Wildcard routes:     /files/*filepath
4. Catch-all:           /*
```

**Key Structures:**
```go
type Router struct {
    trees      map[string]*node  // Method -> root node
    middleware []Middleware
    notFound   Handler
}

type node struct {
    path       string
    isParam    bool       // :id
    isWildcard bool       // *path
    handler    Handler
    children   []*node
    indices    string     // First chars of children
}
```

**Design Decisions:**
- Radix tree for efficient prefix matching
- Per-method trees (separate GET/POST trees)
- Path parameters stored in request context
- Support trailing slash normalization

### 3.5 Middleware System

**Chain Architecture:**
```go
type Middleware func(Handler) Handler
type Handler func(ResponseWriter, *Request)

// Middleware chain execution
middleware1(middleware2(middleware3(handler)))
```

**Built-in Middlewares:**

1. **Logger Middleware**
   - Log request method, path, status, duration
   - Structured logging (JSON format)
   - Performance: ~10µs overhead

2. **CORS Middleware**
   - Handle preflight OPTIONS requests
   - Set Access-Control-* headers
   - Origin validation with wildcards

3. **Rate Limiter Middleware**
   - Token bucket algorithm
   - Per-IP rate limiting
   - Configurable burst size
   - 429 Too Many Requests response

4. **Compression Middleware**
   - Gzip/deflate encoding
   - Automatic negotiation via Accept-Encoding
   - Minimum size threshold (1KB)
   - Skip already compressed content types

5. **Auth Middleware**
   - Basic authentication
   - Bearer token validation
   - JWT support (optional)
   - 401 Unauthorized response

6. **Recovery Middleware**
   - Panic recovery
   - 500 Internal Server Error response
   - Stack trace logging
   - Prevent connection drops

### 3.6 Connection Pool

**Responsibilities:**
- Reuse connections (keep-alive)
- Limit concurrent connections
- Connection timeout management
- Idle connection cleanup

**Key Structures:**
```go
type ConnectionPool struct {
    maxConnections int
    activeConns    int64  // atomic
    idleConns      chan net.Conn
    cleanupTicker  *time.Ticker
    mu             sync.RWMutex
}
```

**Design Decisions:**
- Semaphore pattern for connection limiting
- Channel-based idle connection queue
- Periodic cleanup of stale connections
- Configurable idle timeout

### 3.7 Worker Pool

**Responsibilities:**
- Limit concurrent request handlers
- Queue overflow requests
- Graceful degradation under load

**Key Structures:**
```go
type WorkerPool struct {
    maxWorkers int
    jobs       chan job
    wg         sync.WaitGroup
}

type job struct {
    conn    net.Conn
    handler func(net.Conn)
}
```

**Design Decisions:**
- Fixed-size worker pool
- Buffered job channel (2x worker count)
- Fast rejection when queue full (503 Service Unavailable)

### 3.8 Static File Server

**Responsibilities:**
- Serve files from filesystem
- Support Range requests (partial content)
- ETag generation and validation
- Directory listing (optional)
- Content-Type detection

**Key Features:**
- **Caching Strategy**:
  - ETag: SHA-256 of (mtime + size)
  - If-None-Match → 304 Not Modified
  - If-Modified-Since → 304 Not Modified

- **Range Requests**:
  - Parse Range header (bytes=start-end)
  - 206 Partial Content response
  - Content-Range header

- **Security**:
  - Path traversal prevention (../)
  - Hidden file protection (.git, .env)
  - Symlink following control

### 3.9 WebSocket Support

**Responsibilities:**
- HTTP → WebSocket upgrade
- Frame parsing/writing
- Ping/pong heartbeat
- Message fragmentation
- Close handshake

**Upgrade Handshake:**
```
Client → Server:
GET /ws HTTP/1.1
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==
Sec-WebSocket-Version: 13

Server → Client:
HTTP/1.1 101 Switching Protocols
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Accept: HSmrc0sMlYUkAGmm5OPpG2HaGWk=
```

**Key Structures:**
```go
type WebSocket struct {
    conn       net.Conn
    reader     *bufio.Reader
    writer     *bufio.Writer

    readDeadline  time.Time
    writeDeadline time.Time

    mu sync.Mutex
}

type Frame struct {
    Fin     bool
    Opcode  byte
    Masked  bool
    Payload []byte
}
```

---

## 4. Concurrency Model

### 4.1 Goroutine Management

**Three-Level Concurrency:**

1. **Acceptor Goroutine** (1 instance)
   - Accept incoming connections
   - Submit to worker pool
   - Never blocks

2. **Worker Goroutines** (fixed pool size)
   - Handle connections end-to-end
   - Parse request → Execute handlers → Write response
   - Configurable: defaults to runtime.NumCPU() * 2

3. **Handler Goroutines** (optional)
   - Long-running handlers can spawn additional goroutines
   - User responsibility

### 4.2 Synchronization Primitives

**Usage Guidelines:**

| Primitive | Use Case | Example |
|-----------|----------|---------|
| `sync.Mutex` | Protect mutable state | Connection pool active count |
| `sync.RWMutex` | Read-heavy workloads | Router tree lookups |
| `sync.WaitGroup` | Wait for goroutines | Graceful shutdown |
| `atomic.Int64` | Simple counters | Request count, bytes sent |
| `chan` | Job queue | Worker pool jobs |

### 4.3 Resource Limits

**Configuration:**
```go
type ServerConfig struct {
    MaxConnections      int           // Limit concurrent connections
    MaxWorkers          int           // Worker pool size
    MaxHeaderBytes      int           // Per-request header limit
    MaxBodyBytes        int64         // Per-request body limit
    ReadTimeout         time.Duration // Socket read timeout
    WriteTimeout        time.Duration // Socket write timeout
    IdleTimeout         time.Duration // Keep-alive timeout
    ShutdownTimeout     time.Duration // Graceful shutdown deadline
}
```

**Defaults:**
- MaxConnections: 10,000
- MaxWorkers: NumCPU * 2
- MaxHeaderBytes: 1 MB
- MaxBodyBytes: 10 MB
- ReadTimeout: 30s
- WriteTimeout: 30s
- IdleTimeout: 60s
- ShutdownTimeout: 30s

---

## 5. Data Flow

### 5.1 Request Processing Flow

```
1. Accept Connection
   ↓
2. Read from Socket → Buffer
   ↓
3. Parse Request Line
   │  - Method
   │  - URL
   │  - HTTP Version
   ↓
4. Parse Headers
   │  - Key: Value pairs
   │  - Special: Host, Content-Length, Transfer-Encoding
   ↓
5. Parse Body (if present)
   │  - Content-Length: Fixed size
   │  - Transfer-Encoding: chunked
   ↓
6. Middleware Chain (pre-processing)
   │  - Logger: Record request
   │  - Auth: Validate credentials
   │  - CORS: Add headers
   │  - Rate Limit: Check quota
   │  - Compression: Setup encoder
   ↓
7. Router Matching
   │  - Find handler by method + path
   │  - Extract path parameters
   ↓
8. Execute Handler
   │  - User code
   │  - Generate response
   ↓
9. Middleware Chain (post-processing)
   │  - Compression: Encode body
   │  - Logger: Record response
   ↓
10. Write Response
    │  - Status line
    │  - Headers
    │  - Body
    ↓
11. Connection Handling
    │  - Keep-Alive: Return to pool
    │  - Close: Close socket
```

### 5.2 WebSocket Upgrade Flow

```
1. Receive HTTP Request
   ↓
2. Validate Upgrade Headers
   │  - Upgrade: websocket
   │  - Connection: Upgrade
   │  - Sec-WebSocket-Key: present
   │  - Sec-WebSocket-Version: 13
   ↓
3. Compute Sec-WebSocket-Accept
   │  - SHA-1(key + magic string)
   │  - Base64 encode
   ↓
4. Send 101 Switching Protocols
   ↓
5. Switch to WebSocket Protocol
   │  - Frame parsing
   │  - Ping/Pong
   │  - Message dispatch
   ↓
6. Handle WebSocket Messages
   ↓
7. Close Handshake
   │  - Send close frame
   │  - Wait for close frame
   │  - Close socket
```

---

## 6. Performance Strategy

### 6.1 Memory Optimization

**Buffer Pooling:**
```go
var bufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 4096)
    },
}
```

**Usage:**
- Request parsing buffers
- Response write buffers
- Compression buffers

**Benefit:** Reduce GC pressure, ~30% memory allocation reduction

### 6.2 Zero-Copy Techniques

**io.Copy for File Serving:**
```go
// Kernel-level copy, zero user-space copying
io.Copy(responseWriter, file)
```

**sendfile() System Call:**
- Direct kernel-to-kernel transfer
- Available via `net.TCPConn.ReadFrom()`
- ~2x faster than buffered copy

### 6.3 CPU Optimization

**Fast Path for Static Routes:**
```go
// O(1) map lookup for exact matches
if handler, ok := r.staticRoutes[path]; ok {
    return handler
}
// O(log n) tree traversal for patterns
return r.tree.match(path)
```

**Header Normalization Cache:**
```go
var headerCache = sync.Map{}

func normalizeHeader(key string) string {
    if normalized, ok := headerCache.Load(key); ok {
        return normalized.(string)
    }
    // ... normalize logic
    headerCache.Store(key, normalized)
    return normalized
}
```

### 6.4 Benchmarking Targets

| Operation | Target Latency | Target Throughput |
|-----------|---------------|-------------------|
| Parse Request | < 100µs | 100k req/s |
| Route Matching | < 10µs | 1M routes/s |
| Write Response | < 50µs | 200k req/s |
| Static File (1KB) | < 500µs | 20k req/s |
| Static File (1MB) | < 10ms | 1k req/s |
| WebSocket Frame | < 50µs | 200k frames/s |

---

## 7. Security Considerations

### 7.1 Input Validation

**Request Limits:**
- Maximum header size: 1 MB
- Maximum body size: 10 MB (configurable)
- Maximum URI length: 8 KB
- Maximum headers: 100

**Validation Checks:**
- HTTP method whitelist
- Header name format (RFC 7230)
- Content-Length vs actual body size
- Transfer-Encoding validation

### 7.2 DoS Prevention

**Rate Limiting:**
- Token bucket per IP
- Default: 100 req/s, burst 200
- Configurable per-route

**Connection Limits:**
- Max concurrent connections
- Max connections per IP
- Slow client timeout (slow loris protection)

**Request Timeouts:**
- Read timeout: 30s
- Write timeout: 30s
- Idle timeout: 60s

### 7.3 Path Traversal Prevention

**Static File Serving:**
```go
func safePath(root, requested string) (string, error) {
    path := filepath.Join(root, filepath.Clean("/"+requested))

    // Ensure path is within root
    if !strings.HasPrefix(path, root) {
        return "", errors.New("path traversal detected")
    }

    return path, nil
}
```

**Blocked Patterns:**
- `../` sequences
- `.git/`, `.env`, `.ssh/`
- Null bytes
- Windows device names (CON, PRN, etc.)

### 7.4 TLS Configuration

**Recommended Settings:**
```go
tlsConfig := &tls.Config{
    MinVersion:   tls.VersionTLS12,
    CipherSuites: []uint16{
        tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
        tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
    },
    PreferServerCipherSuites: true,
}
```

### 7.5 Header Security

**Default Security Headers:**
```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Strict-Transport-Security: max-age=31536000; includeSubDomains
Content-Security-Policy: default-src 'self'
```

---

## 8. Testing Strategy

### 8.1 Unit Tests

**Coverage Target: 90%+**

**Components to Test:**
1. Request Parser
   - Valid requests
   - Malformed requests
   - Edge cases (empty headers, large bodies)
   - All HTTP methods
   - HTTP/1.0 and HTTP/1.1

2. Response Writer
   - Status codes
   - Header writing
   - Chunked encoding
   - Compression

3. Router
   - Static routes
   - Parametric routes
   - Wildcard routes
   - Method routing
   - 404 handling

4. Middleware
   - Chain execution order
   - Early termination
   - Context passing

### 8.2 Integration Tests

**Test Scenarios:**
1. **End-to-End Request Flow**
   - Send HTTP request via net.Dial
   - Verify correct response

2. **Concurrent Requests**
   - 1000 parallel requests
   - Verify no data races
   - Check resource cleanup

3. **Keep-Alive**
   - Multiple requests on same connection
   - Verify connection reuse

4. **WebSocket Upgrade**
   - HTTP → WebSocket
   - Bidirectional messaging
   - Close handshake

5. **TLS/HTTPS**
   - Certificate validation
   - Cipher suite negotiation

6. **Static File Serving**
   - Range requests
   - ETag validation
   - 304 Not Modified
   - Directory listing

7. **Graceful Shutdown**
   - In-flight requests complete
   - New requests rejected
   - Timeout handling

### 8.3 Benchmark Tests

**Performance Benchmarks:**
```go
BenchmarkRequestParse
BenchmarkRouterMatch
BenchmarkResponseWrite
BenchmarkMiddlewareChain
BenchmarkStaticFile1KB
BenchmarkStaticFile1MB
BenchmarkCompression
BenchmarkWebSocketFrame
```

**Load Tests:**
- 10k concurrent connections
- 100k req/s throughput
- Measure: latency p50, p95, p99

### 8.4 Stress Tests

**Chaos Engineering:**
1. **Resource Exhaustion**
   - Memory limits
   - File descriptor limits
   - CPU saturation

2. **Network Issues**
   - Slow clients
   - Connection drops
   - Malformed packets

3. **Edge Cases**
   - Extremely large headers/bodies
   - Rapid open/close cycles
   - Invalid UTF-8

---

## 9. Implementation Phases

### Phase 1: Core Protocol (Week 1)
- [x] Request parser
- [ ] Response writer
- [ ] Basic TCP server
- [ ] Simple handler interface
- [ ] Unit tests for protocol layer

**Deliverable:** HTTP/1.1 echo server

### Phase 2: Router & Middleware (Week 1)
- [ ] Router with radix tree
- [ ] Middleware chain
- [ ] Logger middleware
- [ ] Recovery middleware
- [ ] Unit tests for routing

**Deliverable:** Routed server with logging

### Phase 3: Advanced Features (Week 2)
- [ ] Static file server
- [ ] Compression middleware
- [ ] CORS middleware
- [ ] Rate limiting middleware
- [ ] Integration tests

**Deliverable:** Production-ready feature set

### Phase 4: Performance (Week 2)
- [ ] Worker pool
- [ ] Connection pooling
- [ ] Buffer pooling
- [ ] Zero-copy optimizations
- [ ] Benchmark tests

**Deliverable:** 100k req/s capable server

### Phase 5: WebSocket & TLS (Week 3)
- [ ] WebSocket upgrade
- [ ] WebSocket frame parsing
- [ ] TLS support
- [ ] HTTPS server
- [ ] WebSocket tests

**Deliverable:** Full-featured server

### Phase 6: Polish & Documentation (Week 3)
- [ ] Graceful shutdown
- [ ] Configuration validation
- [ ] Example applications
- [ ] API documentation
- [ ] Performance tuning guide
- [ ] Deployment guide

**Deliverable:** Production-ready release

---

## 10. Success Metrics

### Performance
- ✅ 100k req/s on 8-core CPU
- ✅ < 1ms p99 latency for simple requests
- ✅ 10k concurrent WebSocket connections
- ✅ < 100 MB memory for 10k idle connections

### Reliability
- ✅ Zero crashes under load
- ✅ Graceful degradation at capacity
- ✅ 100% of in-flight requests complete on shutdown

### Quality
- ✅ 90%+ test coverage
- ✅ Zero data races
- ✅ Zero memory leaks
- ✅ RFC 7230-7235 compliant

### Usability
- ✅ < 10 lines for "Hello World"
- ✅ Intuitive API
- ✅ Clear error messages
- ✅ Comprehensive examples

---

## 11. API Examples

### Simple Server
```go
package main

import (
    "github.com/sanskarpan/http-server/pkg/httpserver"
)

func main() {
    server := httpserver.New()

    server.GET("/", func(w httpserver.ResponseWriter, r *httpserver.Request) {
        w.WriteString("Hello, World!")
    })

    server.Listen(":8080")
}
```

### With Middleware
```go
server := httpserver.New()

// Global middleware
server.Use(httpserver.Logger())
server.Use(httpserver.Recovery())
server.Use(httpserver.CORS())

// Route with middleware
server.GET("/api/users",
    httpserver.RateLimit(100),
    httpserver.Auth("Bearer"),
    handleGetUsers,
)
```

### Static Files
```go
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
        msg, err := ws.ReadMessage()
        if err != nil {
            break
        }
        ws.WriteMessage(msg)
    }
})
```

---

## 12. File Structure

```
HTTP-Server/
├── cmd/
│   ├── server/              # Main server binary
│   │   └── main.go
│   └── example/             # Example applications
│       ├── basic.go
│       ├── middleware.go
│       ├── static.go
│       └── websocket.go
├── internal/
│   ├── server/              # Core server
│   │   ├── server.go
│   │   └── config.go
│   ├── request/             # Request parser
│   │   └── request.go
│   ├── response/            # Response writer
│   │   └── response.go
│   ├── router/              # Router
│   │   ├── router.go
│   │   └── tree.go
│   ├── middleware/          # Middleware
│   │   ├── logger.go
│   │   ├── cors.go
│   │   ├── ratelimit.go
│   │   ├── compression.go
│   │   ├── auth.go
│   │   └── recovery.go
│   ├── websocket/           # WebSocket
│   │   ├── websocket.go
│   │   └── frame.go
│   └── pool/                # Resource pools
│       ├── worker.go
│       ├── connection.go
│       └── buffer.go
├── pkg/
│   └── httpserver/          # Public API
│       ├── server.go
│       ├── handler.go
│       └── middleware.go
├── tests/
│   ├── unit/
│   ├── integration/
│   └── benchmark/
├── examples/
│   ├── basic_usage.go
│   ├── middleware_example.go
│   └── websocket_chat.go
├── docs/
│   ├── ARCHITECTURE.md      # This file
│   ├── API.md
│   └── PERFORMANCE.md
├── go.mod
├── go.sum
├── README.md
├── Makefile
└── .gitignore
```

---

## Conclusion

This architecture provides a solid foundation for building a production-grade HTTP server. The modular design allows for incremental development and testing, while the performance optimizations ensure it can handle high load. The comprehensive security measures protect against common attacks, and the extensive testing strategy ensures reliability.

**Next Steps:**
1. Review and approve architecture
2. Begin Phase 1 implementation
3. Set up CI/CD pipeline
4. Create project board for task tracking

