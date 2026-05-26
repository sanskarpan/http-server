# Operations Guide

## Runtime endpoints

- `GET /healthz/live`
  - Returns `200` while the process is alive.
- `GET /healthz/ready`
  - Returns `200` when the server is accepting traffic.
  - Returns `503` while draining during shutdown.
- `GET /metrics`
  - Exposes a Prometheus-compatible text payload with server counters, worker-pool gauges, uptime, goroutines, and memory gauges.

## Log format

`cmd/httpserverd` emits one JSON object per request. Recommended fields:
- `timestamp`
- `level`
- `event`
- `request_id`
- `method`
- `path`
- `query`
- `status`
- `duration_ms`
- `response_bytes`
- `remote_addr`
- `user_agent`
- `content_type`
- `accept_encoding`
- `response_encoding`

The middleware preserves inbound `X-Request-ID` when present and generates one otherwise.

## Metrics surface

Current metrics exported by the reference binary:
- `httpserver_build_info`
- `httpserver_uptime_seconds`
- `httpserver_active_connections`
- `httpserver_total_connections_total`
- `httpserver_total_requests_total`
- `httpserver_worker_pool_max_workers`
- `httpserver_worker_pool_active_workers`
- `httpserver_worker_pool_queue_size`
- `httpserver_worker_pool_queue_capacity`
- `httpserver_worker_pool_total_jobs_total`
- `httpserver_worker_pool_completed_jobs_total`
- `go_memstats_alloc_bytes`
- `go_memstats_heap_inuse_bytes`
- `go_goroutines`

## Production recommendations

- Keep `ReadTimeout`, `WriteTimeout`, and `IdleTimeout` explicit.
- Set `MaxBodyBytes` and `MaxHeaderBytes` to values appropriate for the workload.
- Terminate TLS either in-process with `ListenTLS` or at a trusted edge proxy.
- Scrape `/metrics` and alert on:
  - sustained growth in `httpserver_active_connections`
  - worker queue saturation
  - rising goroutine count without traffic growth
  - readiness returning `503`

## Shutdown behavior

The reference binary marks readiness as draining before calling graceful shutdown. Traffic managers should stop routing new requests as soon as `/healthz/ready` returns `503`.
