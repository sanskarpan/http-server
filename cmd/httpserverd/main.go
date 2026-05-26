package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sanskarpan/http-server/pkg/httpserver"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var requestCounter atomic.Uint64

func main() {
	logger := log.New(os.Stdout, "", 0)

	config, shutdownTimeout, tlsCertFile, tlsKeyFile, staticDir, staticURLPath, err := loadConfig()
	if err != nil {
		logger.Fatalf("configuration error: %v", err)
	}

	srv := httpserver.NewWithConfig(config)
	ready := &atomic.Bool{}
	startedAt := time.Now().UTC()

	srv.Use(requestIDMiddleware())
	srv.Use(jsonAccessLogMiddleware(logger))
	srv.Use(httpserver.Recovery())

	registerOperationalRoutes(srv, ready, startedAt)
	registerApplicationRoutes(srv)

	if staticDir != "" {
		srv.Static(staticURLPath, staticDir)
	}

	errCh := make(chan error, 1)
	ready.Store(true)

	go func() {
		var listenErr error
		if tlsCertFile != "" || tlsKeyFile != "" {
			if tlsCertFile == "" || tlsKeyFile == "" {
				listenErr = errors.New("both HTTP_SERVER_TLS_CERT_FILE and HTTP_SERVER_TLS_KEY_FILE must be set together")
			} else {
				listenErr = srv.ListenTLS(config.Addr, tlsCertFile, tlsKeyFile)
			}
		} else {
			listenErr = srv.Listen(config.Addr)
		}
		errCh <- listenErr
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		ready.Store(false)
		logger.Printf(`{"level":"info","event":"shutdown_signal","signal":%q}`, sig.String())
		if err := srv.Shutdown(shutdownTimeout); err != nil {
			logger.Fatalf(`{"level":"error","event":"shutdown_failed","error":%q}`, err.Error())
		}
	case err := <-errCh:
		if err != nil {
			logger.Fatalf(`{"level":"error","event":"server_exit","error":%q}`, err.Error())
		}
	}
}

func registerApplicationRoutes(srv *httpserver.Server) {
	srv.GET("/", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		payload := map[string]any{
			"name":    "httpserverd",
			"version": version,
			"status":  "ok",
		}
		writeJSON(w, httpserver.StatusOK, payload)
	})
}

func registerOperationalRoutes(srv *httpserver.Server, ready *atomic.Bool, startedAt time.Time) {
	srv.GET("/healthz/live", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		writeJSON(w, httpserver.StatusOK, map[string]any{
			"status":  "live",
			"version": version,
			"commit":  commit,
		})
	})

	srv.GET("/healthz/ready", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		statusCode := httpserver.StatusOK
		status := "ready"
		if !ready.Load() {
			statusCode = httpserver.StatusServiceUnavailable
			status = "draining"
		}

		stats := srv.Stats()
		writeJSON(w, statusCode, map[string]any{
			"status":             status,
			"active_connections": stats.ActiveConnections,
			"total_requests":     stats.TotalRequests,
		})
	})

	srv.GET("/metrics", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		stats := srv.Stats()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		w.Header()["Content-Type"] = []string{"text/plain; version=0.0.4; charset=utf-8"}
		w.WriteHeader(httpserver.StatusOK)

		lines := []string{
			"# HELP httpserver_build_info Build metadata for the running binary.",
			"# TYPE httpserver_build_info gauge",
			fmt.Sprintf("httpserver_build_info{version=%q,commit=%q,date=%q} 1", version, commit, date),
			"# HELP httpserver_uptime_seconds Process uptime in seconds.",
			"# TYPE httpserver_uptime_seconds gauge",
			fmt.Sprintf("httpserver_uptime_seconds %.0f", time.Since(startedAt).Seconds()),
			"# HELP httpserver_active_connections Current active connections.",
			"# TYPE httpserver_active_connections gauge",
			fmt.Sprintf("httpserver_active_connections %d", stats.ActiveConnections),
			"# HELP httpserver_total_connections_total Lifetime accepted connections.",
			"# TYPE httpserver_total_connections_total counter",
			fmt.Sprintf("httpserver_total_connections_total %d", stats.TotalConnections),
			"# HELP httpserver_total_requests_total Lifetime processed requests.",
			"# TYPE httpserver_total_requests_total counter",
			fmt.Sprintf("httpserver_total_requests_total %d", stats.TotalRequests),
			"# HELP httpserver_worker_pool_max_workers Configured worker pool size.",
			"# TYPE httpserver_worker_pool_max_workers gauge",
			fmt.Sprintf("httpserver_worker_pool_max_workers %d", stats.WorkerPoolStats.MaxWorkers),
			"# HELP httpserver_worker_pool_active_workers Active workers.",
			"# TYPE httpserver_worker_pool_active_workers gauge",
			fmt.Sprintf("httpserver_worker_pool_active_workers %d", stats.WorkerPoolStats.ActiveWorkers),
			"# HELP httpserver_worker_pool_queue_size Current queued jobs.",
			"# TYPE httpserver_worker_pool_queue_size gauge",
			fmt.Sprintf("httpserver_worker_pool_queue_size %d", stats.WorkerPoolStats.QueueSize),
			"# HELP httpserver_worker_pool_queue_capacity Worker queue capacity.",
			"# TYPE httpserver_worker_pool_queue_capacity gauge",
			fmt.Sprintf("httpserver_worker_pool_queue_capacity %d", stats.WorkerPoolStats.QueueCapacity),
			"# HELP httpserver_worker_pool_total_jobs_total Lifetime submitted jobs.",
			"# TYPE httpserver_worker_pool_total_jobs_total counter",
			fmt.Sprintf("httpserver_worker_pool_total_jobs_total %d", stats.WorkerPoolStats.TotalJobs),
			"# HELP httpserver_worker_pool_completed_jobs_total Lifetime completed jobs.",
			"# TYPE httpserver_worker_pool_completed_jobs_total counter",
			fmt.Sprintf("httpserver_worker_pool_completed_jobs_total %d", stats.WorkerPoolStats.Completed),
			"# HELP go_memstats_alloc_bytes Allocated heap objects.",
			"# TYPE go_memstats_alloc_bytes gauge",
			fmt.Sprintf("go_memstats_alloc_bytes %d", mem.Alloc),
			"# HELP go_memstats_heap_inuse_bytes Heap memory in use.",
			"# TYPE go_memstats_heap_inuse_bytes gauge",
			fmt.Sprintf("go_memstats_heap_inuse_bytes %d", mem.HeapInuse),
			"# HELP go_goroutines Current goroutine count.",
			"# TYPE go_goroutines gauge",
			fmt.Sprintf("go_goroutines %d", runtime.NumGoroutine()),
		}

		_, _ = w.WriteString(strings.Join(lines, "\n") + "\n")
	})
}

func writeJSON(w httpserver.ResponseWriter, statusCode int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		_ = httpserver.WriteError(w, httpserver.StatusInternalServerError, "failed to marshal response")
		return
	}
	_ = httpserver.WriteJSON(w, statusCode, data)
}

func requestIDMiddleware() httpserver.Middleware {
	return func(next httpserver.Handler) httpserver.Handler {
		return httpserver.HandlerFunc(func(w httpserver.ResponseWriter, r *httpserver.Request) {
			requestID := strings.TrimSpace(r.GetHeader("X-Request-ID"))
			if requestID == "" {
				requestID = newRequestID()
			}
			w.Header()["X-Request-ID"] = []string{requestID}
			next.ServeHTTP(w, r)
		})
	}
}

func jsonAccessLogMiddleware(logger *log.Logger) httpserver.Middleware {
	return func(next httpserver.Handler) httpserver.Handler {
		return httpserver.HandlerFunc(func(w httpserver.ResponseWriter, r *httpserver.Request) {
			start := time.Now().UTC()
			next.ServeHTTP(w, r)

			record := map[string]any{
				"level":             "info",
				"event":             "http_request",
				"timestamp":         start.Format(time.RFC3339Nano),
				"duration_ms":       time.Since(start).Milliseconds(),
				"method":            r.Method,
				"path":              r.URL.Path,
				"query":             r.URL.RawQuery,
				"remote_addr":       r.RemoteAddr,
				"user_agent":        r.GetHeader("User-Agent"),
				"request_id":        firstHeaderValue(w.Header()["X-Request-ID"]),
				"status":            w.Status(),
				"response_bytes":    w.Written(),
				"content_length":    r.ContentLength,
				"accept_encoding":   r.GetHeader("Accept-Encoding"),
				"content_type":      r.GetHeader("Content-Type"),
				"response_encoding": firstHeaderValue(w.Header()["Content-Encoding"]),
			}

			data, err := json.Marshal(record)
			if err != nil {
				logger.Printf(`{"level":"error","event":"access_log_marshal_failed","error":%q}`, err.Error())
				return
			}
			logger.Println(string(data))
		})
	}
}

func newRequestID() string {
	id := requestCounter.Add(1)
	return fmt.Sprintf("req-%d-%d", time.Now().UTC().UnixNano(), id)
}

func firstHeaderValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func loadConfig() (*httpserver.Config, time.Duration, string, string, string, string, error) {
	config := httpserver.DefaultConfig()

	config.Addr = envString("HTTP_SERVER_ADDR", config.Addr)
	config.ReadTimeout = envDuration("HTTP_SERVER_READ_TIMEOUT", config.ReadTimeout)
	config.WriteTimeout = envDuration("HTTP_SERVER_WRITE_TIMEOUT", config.WriteTimeout)
	config.IdleTimeout = envDuration("HTTP_SERVER_IDLE_TIMEOUT", config.IdleTimeout)
	config.ShutdownTimeout = envDuration("HTTP_SERVER_SHUTDOWN_TIMEOUT", config.ShutdownTimeout)
	config.MaxHeaderBytes = envInt("HTTP_SERVER_MAX_HEADER_BYTES", config.MaxHeaderBytes)
	config.MaxBodyBytes = envInt64("HTTP_SERVER_MAX_BODY_BYTES", config.MaxBodyBytes)
	config.MaxWorkers = envInt("HTTP_SERVER_MAX_WORKERS", config.MaxWorkers)
	config.QueueSize = envInt("HTTP_SERVER_QUEUE_SIZE", config.QueueSize)
	config.MaxConnections = envInt("HTTP_SERVER_MAX_CONNECTIONS", config.MaxConnections)
	config.MaxIdleConnections = envInt("HTTP_SERVER_MAX_IDLE_CONNECTIONS", config.MaxIdleConnections)
	config.ReadBufferSize = envInt("HTTP_SERVER_READ_BUFFER_SIZE", config.ReadBufferSize)
	config.WriteBufferSize = envInt("HTTP_SERVER_WRITE_BUFFER_SIZE", config.WriteBufferSize)
	config.EnableKeepAlive = envBool("HTTP_SERVER_ENABLE_KEEP_ALIVE", config.EnableKeepAlive)
	config.EnableCompression = envBool("HTTP_SERVER_ENABLE_COMPRESSION", config.EnableCompression)
	config.CompressionLevel = envInt("HTTP_SERVER_COMPRESSION_LEVEL", config.CompressionLevel)

	tlsCertFile := envString("HTTP_SERVER_TLS_CERT_FILE", "")
	tlsKeyFile := envString("HTTP_SERVER_TLS_KEY_FILE", "")
	staticDir := envString("HTTP_SERVER_STATIC_DIR", "")
	staticURLPath := envString("HTTP_SERVER_STATIC_URL_PATH", "/static")

	if staticURLPath == "" || staticURLPath[0] != '/' {
		return nil, 0, "", "", "", "", errors.New("HTTP_SERVER_STATIC_URL_PATH must start with '/'")
	}

	if err := config.Validate(); err != nil {
		return nil, 0, "", "", "", "", err
	}

	return config, config.ShutdownTimeout, tlsCertFile, tlsKeyFile, staticDir, staticURLPath, nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
