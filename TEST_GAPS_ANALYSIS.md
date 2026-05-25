# HTTP Server - Test Gaps Analysis

**Date**: May 25, 2026
**Status**: Most previously critical gaps are closed

## Closed Gaps

- rate limiting now has unit coverage
- static file serving now has security and behavior coverage
- WebSocket handshake and frame behavior now have coverage
- CORS, worker pool, compression, and recovery behavior now have coverage
- request parsing now includes chunked bodies, invalid headers, and oversized-line checks
- router coverage now includes `405`, nested params, and `HEAD` fallback
- benchmarks now exist
- fuzz harnesses now exist

## Remaining Gaps Worth Future Investment

These are no longer "repo is broken" gaps. They are next-level hardening opportunities.

### 1. Sustained Load / Soak Testing
- long-running throughput tests with thousands of concurrent connections
- memory-growth observation under repeated keep-alive traffic
- latency-percentile tracking under mixed route workloads

### 2. Deeper Fuzzing
- longer fuzz campaigns than the current smoke runs
- fuzzing for WebSocket frame parsing
- fuzzing for range-header parsing

### 3. Broader Protocol Compatibility
- `Expect: 100-continue`
- multiple `Range` support if desired
- stricter header validation and normalization rules

### 4. Observability Enhancements
- structured logging
- request IDs / correlation IDs
- exported metrics hooks

### 5. Performance Optimization Work
- reducing parser allocations
- reducing compressed-response allocation cost
- profiling route registration and deep param matching
