# HTTP Server

A hardened HTTP/1.1 server implementation in Go with a custom parser, router, response writer, static file server, and WebSocket support.

This repository is oriented toward production use, but it only claims what is currently validated in-tree. It does not claim blanket RFC compliance or universal throughput numbers beyond the documented benchmark and soak baselines.

## Current validation baseline

The repository currently validates:
- `go build ./...`
- `go test ./...`
- `go test ./... -race`
- `govulncheck ./...`
- `make bench-smoke`
- `make fuzz-smoke`
- `make soak-smoke`
- longer-run `fuzz` and `soak` baselines documented in [docs/VALIDATION_LONG_RUN.md](docs/VALIDATION_LONG_RUN.md)

Current local benchmark snapshots live in `tests/benchmark`. Treat them as regression baselines for this codebase, not as a promise of throughput in every deployment environment.

## Install

```bash
go get github.com/sanskarpan/http-server
```

## Quick start

```go
package main

import (
	"log"

	"github.com/sanskarpan/http-server/pkg/httpserver"
)

func main() {
	srv := httpserver.New()
	srv.GET("/", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		w.WriteString("hello")
	})

	log.Fatal(srv.Listen(":8080"))
}
```

## Production reference binary

The repository includes `cmd/httpserverd`, a reference deployment binary with:
- JSON access logs
- `GET /healthz/live`
- `GET /healthz/ready`
- `GET /metrics` in Prometheus text format
- graceful shutdown on `SIGINT` and `SIGTERM`
- env-driven runtime configuration

Run it locally:

```bash
go run ./cmd/httpserverd
```

Run it with Docker:

```bash
docker build -t http-server:local .
docker run --rm -p 8080:8080 http-server:local
```

## Public API highlights

- `httpserver.New()` and `httpserver.NewWithConfig(...)`
- route registration for `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `HEAD`, and `OPTIONS`
- route groups and middleware chaining
- static file serving with `Static(...)` and `StaticFS(...)`
- built-in middleware for recovery, CORS, rate limiting, auth, and compression
- `Server.Stats()` for runtime metrics

## Configuration

```go
config := httpserver.DefaultConfig()
config.Addr = ":8080"
config.ReadTimeout = 30 * time.Second
config.WriteTimeout = 30 * time.Second
config.IdleTimeout = 60 * time.Second
config.MaxHeaderBytes = 1 << 20
config.MaxBodyBytes = 10 << 20

srv := httpserver.NewWithConfig(config)
```

The production binary accepts the same settings through environment variables. See [deploy/production.env.example](deploy/production.env.example).

## Examples

```bash
go run ./examples/basic_usage
go run ./examples/rest_api
go run ./examples/websocket_chat
```

## Operations and release docs

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [docs/OPERATIONS.md](docs/OPERATIONS.md)
- [docs/VALIDATION_LONG_RUN.md](docs/VALIDATION_LONG_RUN.md)
- [docs/RELEASING.md](docs/RELEASING.md)
- [docs/GITHUB_ADMIN.md](docs/GITHUB_ADMIN.md)

## Limitations and honesty points

- The server is validated against its test, fuzz, soak, and benchmark suites, not a complete external RFC certification corpus.
- The checked-in long-run validation is still single-host repository validation, not distributed production traffic replay.
- Branch protection and required check enforcement depend on GitHub plan capabilities for the hosting repository. The exact desired policy is documented in [docs/GITHUB_ADMIN.md](docs/GITHUB_ADMIN.md).
