# Long-run Validation Baseline

This document records longer-running validation that exceeds the default smoke suite.

## Commands

- `FUZZ_TIME=15s make fuzz-long`
- `HTTP_SERVER_SOAK_DURATION=45s HTTP_SERVER_SOAK_CONCURRENCY=64 make soak-long`

## Scope

- parser fuzzing
- router dispatch fuzzing
- static path validation fuzzing
- concurrent real-network soak traffic covering health, HEAD, param routing, body echo, gzip, and malformed traffic

## Current baseline

The baseline is updated whenever the release-readiness pass is rerun. The current committed baseline was captured on 2026-05-26 with the commands above.

### Fuzz baseline

- `internal/request`: 15s, passed, reached 2,429,433 executions with 83 total interesting inputs
- `internal/router`: 15s, passed, reached 3,830,652 executions with 216 total interesting inputs
- `internal/server`: 15s, passed, reached 3,803,613 executions with 166 total interesting inputs

### Soak baseline

- duration: `45s`
- concurrency: `64`
- malformed workers: `2`
- result: passed
- total requests: `1,896,650`
- failures: `0`
- average latency: `1.518142ms`
- max latency: `447.764333ms`
- scenario distribution:
  - health: `379,327`
  - head: `379,333`
  - params: `379,329`
  - echo: `379,332`
  - gzip: `379,329`

## Findings from the latest run

- The long soak initially exposed a production bug: workers were tied to connection lifetime, so idle keep-alive sockets could starve request processing under sustained load.
- The server now schedules work per request instead of per connection, which removes that starvation path and keeps long-run soak traffic stable.
