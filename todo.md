# HTTP Server Audit Todo

This file tracks the production issues found during the codebase audit and the fix work applied.

## Completed Fixes

- [x] Server lifecycle was broken.
  Root cause: `Listen()` delegated to `Start()`, and `Start()` returned immediately after spawning the accept loop. Normal example usage therefore exited the process instead of keeping the server alive.
  Impact: the primary public API did not actually serve requests reliably.
  Fixed in: `internal/server/server.go`.

- [x] Response writer had transport-level correctness bugs.
  Root cause: the writer mixed finalization, flushing, compression, and content-length handling in ways that could deadlock or emit malformed responses.
  Impact: gzip responses were incorrect, header-only responses could disappear, and some code paths could hang.
  Fixed in: `internal/response/response.go`.

- [x] WebSocket upgrade and close behavior was broken.
  Root cause: upgrade depended on a concrete writer instead of unwrapping middleware layers; close logic marked the socket closed before emitting the close frame.
  Impact: handshake failures behind middleware and protocol-incorrect close behavior.
  Fixed in: `internal/websocket/websocket.go`, `internal/middleware/logger.go`.

- [x] Static file mounting and shutdown behavior were incorrect.
  Root cause: static serving used the full routed path rather than the wildcard suffix; graceful shutdown waited on idle keep-alive sockets until timeout.
  Impact: mounted static routes served the wrong files; shutdown could stall.
  Fixed in: `internal/server/static.go`, `internal/server/server.go`.

- [x] Request parsing was incomplete and under-validated.
  Root cause: the parser accepted invalid `Content-Length` combinations and lacked chunked request body support.
  Impact: malformed requests were accepted, and valid HTTP/1.1 chunked bodies were unsupported.
  Fixed in: `internal/request/request.go`.

- [x] Examples and build layout were invalid.
  Root cause: multiple example programs lived in the same package, so `go build ./...` failed.
  Impact: the module did not build cleanly.
  Fixed in: `examples/*`, `Makefile`.

- [x] Several middleware issues reduced correctness and security.
  Root cause: invalid CORS `Max-Age` formatting, panic details leaked to clients, and brittle IP extraction in rate limiting.
  Impact: incorrect CORS headers, information leakage, and inconsistent rate-limiter keys.
  Fixed in: `internal/middleware/cors.go`, `internal/middleware/recovery.go`, `internal/middleware/ratelimit.go`.

## Additional Fixes Completed

- [x] Response `Flush()` semantics were unsafe for edge cases.
  Root cause: the implementation conflated "flush buffered bytes now" with "finish the response body", which could break incremental writes and duplicate finalization.
  Edge cases covered:
  - handlers calling `Flush()` before returning
  - upgraded or header-only responses
  - repeated `Close()` calls after finalization
  Fixed in: `internal/response/response.go`, `internal/server/server.go`, `internal/response/response_test.go`.

- [x] Generic `HEAD` semantics were incomplete.
  Root cause: `HEAD` only worked when explicitly registered, and body suppression was not enforced for ordinary handlers.
  Edge cases covered:
  - `HEAD` falling back to `GET`
  - header-only semantics even when handlers write bodies
  - group-level `HEAD` and `OPTIONS` helpers
  Fixed in: `internal/router/router.go`, `internal/response/response.go`, `internal/server/server.go`, `pkg/httpserver/server.go`, `tests/integration/server_test.go`.

- [x] Router returned `404` where `405 Method Not Allowed` was required.
  Root cause: lookup only checked the current method tree and fell straight to `notFound`.
  Edge cases covered:
  - path exists for another method
  - `Allow` header population
  Fixed in: `internal/router/router.go`, `internal/router/router_test.go`.

- [x] Parser line reading was vulnerable to oversized-line memory pressure.
  Root cause: `ReadBytes('\n')` could grow unbounded before header-size checks rejected the request.
  Edge cases covered:
  - extremely long request lines
  - single oversized header lines
  - missing `Host` on HTTP/1.1
  Fixed in: `internal/request/request.go`, `internal/request/request_test.go`.

- [x] Rate limiter middleware leaked a background goroutine per construction.
  Root cause: each middleware instance allocated a ticker-based cleanup loop that was never tied to server shutdown.
  Edge cases covered:
  - embedded library usage
  - repeated test/server construction
  Fixed in: `internal/middleware/ratelimit.go`, `internal/middleware/ratelimit_test.go`.

- [x] Authentication middleware used plain string comparison.
  Root cause: secrets and tokens were compared with ordinary equality.
  Edge cases covered:
  - repeated auth attempts against static credentials
  Fixed in: `internal/middleware/auth.go`.

- [x] Worker pool was not panic-safe or shutdown-safe as a reusable component.
  Root cause: task panics could take down worker goroutines, and `Submit()` on a closed pool could panic.
  Edge cases covered:
  - panicking tasks
  - repeated shutdown
  - submissions after shutdown
  Fixed in: `internal/pool/worker.go`, `internal/pool/worker_test.go`.

- [x] CORS policy handling was permissive and incomplete.
  Root cause: disallowed preflights still short-circuited with `204`, wildcard-plus-credentials behavior was not standards-compliant, and cache `Vary` headers were missing.
  Edge cases covered:
  - credentialed browser requests
  - disallowed origins
  - disallowed requested headers
  Fixed in: `internal/middleware/cors.go`, `internal/middleware/cors_test.go`.

## Final Follow-Up

- [x] Compression no longer buffers whole responses before sending.
  Root cause: gzip handling previously finalized only at end-of-response, so `Flush()` had no meaning for compressed bodies and large responses paid unnecessary memory cost.
  Edge cases covered:
  - multiple writes with `Flush()` between them
  - chunked streaming of compressed bytes
  - repeated `Close()` after finalization
  - avoiding compression on `HEAD`
  Fixed in: `internal/response/response.go`, `internal/response/response_test.go`, `internal/server/server.go`, `internal/middleware/compression.go`.

- [x] Nested param route registration still panicked under deeper route shapes.
  Root cause: the router mutated `path` while iterating over it during param-node insertion, which left stale indexes and caused slice-bound panics for routes such as `/users/:id/posts/:postID`.
  Edge cases covered:
  - multiple param segments
  - mixed static/param route registration
  - benchmark-driven registration paths
  Fixed in: `internal/router/tree.go`, `internal/router/router_test.go`, `tests/benchmark/benchmark_test.go`.

- [x] Parser allocation pressure was still too high on the common request path.
  Root cause: the parser eagerly allocated path-param/query structures, canonicalized headers with string-splitting, parsed request lines with `strings.Split`, and always paid the generic `url.Parse` cost even for ordinary origin-form paths.
  Edge cases covered:
  - escaped request targets still preserving decoded `Path` and `RawPath`
  - query parsing only when present
  - oversized request lines still rejected
  Fixed in: `internal/request/request.go`, `internal/request/request_test.go`, `tests/benchmark/benchmark_test.go`.

- [x] Compressed responses were materially more expensive than necessary.
  Root cause: gzip writers and buffered writers were recreated for every response, chunk framing used `fmt.Sprintf`, and header writing allocated more than necessary.
  Edge cases covered:
  - repeated compressed responses reusing pooled gzip state safely
  - repeated `Close()` calls after pooled-resource release
  - chunked compressed and uncompressed responses preserving framing
  Fixed in: `internal/response/response.go`, `internal/response/response_test.go`, `tests/benchmark/benchmark_test.go`.

- [x] Production-confidence validation lacked repeatable soak/load and longer fuzz workflows.
  Root cause: the repo only had smoke-level fuzzing and microbenchmarks, so longer-running network validation depended on ad hoc commands.
  Edge cases covered:
  - concurrent `GET`, `HEAD`, param, echo, and gzip flows under load
  - malformed request traffic mixed into soak runs
  - configurable longer fuzz campaigns for parser, router, and static-path validation
  Fixed in: `tests/soak/soak_test.go`, `Makefile`.

- [x] Security hardening gaps remained in request parsing, static file serving, and WebSocket handling.
  Root cause: the code accepted ambiguous authority / transfer-encoding combinations, static serving trusted symlink resolution and emitted unescaped directory listings, and WebSocket accepted cross-origin upgrades plus protocol-invalid client frames.
  Edge cases covered:
  - multiple `Host` headers and absolute-form host mismatch
  - unsupported transfer-encoding chains
  - symlink escapes outside static roots
  - hidden-file leakage and XSS in directory listings
  - invalid WebSocket keys, cross-origin upgrades, unmasked frames, fragmented control frames, and oversized payloads
  Fixed in: `internal/request/request.go`, `internal/server/static.go`, `internal/websocket/websocket.go`, related tests.

- [x] The repository toolchain itself was pinned below current Go security fixes.
  Root cause: `govulncheck` reported standard-library vulnerabilities in `net`, `crypto/tls`, and `crypto/x509` from the active Go runtime.
  Edge cases covered:
  - runtime/toolchain CVEs outside the application code
  Fixed in: `go.mod` via `toolchain go1.26.3`, validated with `govulncheck ./...`.

## Current Status

No open validated correctness, reliability, measured hot-path optimization, or known vulnerability issues remain from this audit backlog.
