package soak

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sanskar/http-server/internal/server"
	"github.com/sanskar/http-server/pkg/httpserver"
)

type scenario struct {
	name string
	run  func(context.Context, *http.Client, string) error
}

func TestServerSoak(t *testing.T) {
	if os.Getenv("HTTP_SERVER_RUN_SOAK") != "1" {
		t.Skip("set HTTP_SERVER_RUN_SOAK=1 to run soak validation")
	}

	duration := envDuration("HTTP_SERVER_SOAK_DURATION", 3*time.Second)
	concurrency := envInt("HTTP_SERVER_SOAK_CONCURRENCY", 24)
	malformedWorkers := envInt("HTTP_SERVER_SOAK_MALFORMED_WORKERS", 2)

	baseURL, shutdown := startSoakServer(t)
	defer shutdown()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableCompression:  true,
			MaxIdleConns:        concurrency * 2,
			MaxIdleConnsPerHost: concurrency,
			MaxConnsPerHost:     concurrency * 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	scenarios := []scenario{
		{
			name: "health",
			run: func(ctx context.Context, client *http.Client, baseURL string) error {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
				if err != nil {
					return err
				}
				resp, err := client.Do(req)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected health status: %d", resp.StatusCode)
				}
				if string(body) != "ok" {
					return fmt.Errorf("unexpected health body: %q", string(body))
				}
				return nil
			},
		},
		{
			name: "head",
			run: func(ctx context.Context, client *http.Client, baseURL string) error {
				req, err := http.NewRequestWithContext(ctx, http.MethodHead, baseURL+"/health", nil)
				if err != nil {
					return err
				}
				resp, err := client.Do(req)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected head status: %d", resp.StatusCode)
				}
				if len(body) != 0 {
					return fmt.Errorf("expected empty HEAD body, got %d bytes", len(body))
				}
				return nil
			},
		},
		{
			name: "params",
			run: func(ctx context.Context, client *http.Client, baseURL string) error {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/users/123/posts/456?view=full", nil)
				if err != nil {
					return err
				}
				resp, err := client.Do(req)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected params status: %d", resp.StatusCode)
				}
				if string(body) != "123:456:full" {
					return fmt.Errorf("unexpected params body: %q", string(body))
				}
				return nil
			},
		},
		{
			name: "echo",
			run: func(ctx context.Context, client *http.Client, baseURL string) error {
				payload := strings.Repeat("echo-payload-", 64)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/echo", strings.NewReader(payload))
				if err != nil {
					return err
				}
				req.Header.Set("Content-Type", "text/plain")
				resp, err := client.Do(req)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected echo status: %d", resp.StatusCode)
				}
				if string(body) != payload {
					return fmt.Errorf("unexpected echo body length: %d", len(body))
				}
				return nil
			},
		},
		{
			name: "gzip",
			run: func(ctx context.Context, client *http.Client, baseURL string) error {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/gzip", nil)
				if err != nil {
					return err
				}
				req.Header.Set("Accept-Encoding", "gzip")
				resp, err := client.Do(req)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected gzip status: %d", resp.StatusCode)
				}
				if resp.Header.Get("Content-Encoding") != "gzip" {
					return fmt.Errorf("expected gzip encoding, got %q", resp.Header.Get("Content-Encoding"))
				}

				gzipReader, err := gzip.NewReader(resp.Body)
				if err != nil {
					return err
				}
				defer gzipReader.Close()

				body, err := io.ReadAll(gzipReader)
				if err != nil {
					return err
				}
				if string(body) != strings.Repeat("compressed-response-", 256) {
					return fmt.Errorf("unexpected gzip payload length: %d", len(body))
				}
				return nil
			},
		},
	}

	var total atomic.Uint64
	var failures atomic.Uint64
	var totalLatency atomic.Uint64
	var maxLatency atomic.Int64

	scenarioCounts := make([]atomic.Uint64, len(scenarios))
	errCh := make(chan error, concurrency+malformedWorkers)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var workers sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workers.Add(1)
		go func(workerID int) {
			defer workers.Done()

			iteration := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				sc := scenarios[(workerID+iteration)%len(scenarios)]
				start := time.Now()
				err := sc.run(ctx, client, baseURL)
				latency := time.Since(start)
				if err != nil && isExpectedContextShutdown(ctx, err) {
					return
				}

				total.Add(1)
				totalLatency.Add(uint64(latency))
				scenarioCounts[(workerID+iteration)%len(scenarios)].Add(1)
				updateMaxLatency(&maxLatency, latency)

				if err != nil {
					failures.Add(1)
					select {
					case errCh <- fmt.Errorf("%s: %w", sc.name, err):
					default:
					}
				}

				iteration++
			}
		}(i)
	}

	for i := 0; i < malformedWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if err := sendMalformedRequest(ctx, baseURL); err != nil {
					failures.Add(1)
					select {
					case errCh <- fmt.Errorf("malformed: %w", err):
					default:
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		}()
	}

	workers.Wait()

	close(errCh)
	if len(errCh) > 0 {
		var messages []string
		for err := range errCh {
			messages = append(messages, err.Error())
			if len(messages) == 5 {
				break
			}
		}
		t.Fatalf("soak validation failed: %s", strings.Join(messages, "; "))
	}

	totalRequests := total.Load()
	if totalRequests == 0 {
		t.Fatal("soak validation executed zero requests")
	}

	avgLatency := time.Duration(totalLatency.Load() / totalRequests)
	t.Logf(
		"soak completed: duration=%s requests=%d failures=%d avg_latency=%s max_latency=%s health=%d head=%d params=%d echo=%d gzip=%d",
		duration,
		totalRequests,
		failures.Load(),
		avgLatency,
		time.Duration(maxLatency.Load()),
		scenarioCounts[0].Load(),
		scenarioCounts[1].Load(),
		scenarioCounts[2].Load(),
		scenarioCounts[3].Load(),
		scenarioCounts[4].Load(),
	)
}

func startSoakServer(t *testing.T) (string, func()) {
	t.Helper()

	addr := reserveLocalAddress(t)
	cfg := server.DefaultConfig()
	cfg.Addr = addr
	cfg.EnableCompression = true
	cfg.CompressionLevel = 6
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	cfg.IdleTimeout = 15 * time.Second
	cfg.MaxWorkers = 64
	cfg.QueueSize = 512

	srv := httpserver.NewWithConfig(cfg)
	srv.GET("/health", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		_, _ = w.WriteString("ok")
	})
	srv.GET("/users/:id/posts/:postID", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		_, _ = w.WriteString(r.PathParams["id"] + ":" + r.PathParams["postID"] + ":" + r.Query.Get("view"))
	})
	srv.POST("/echo", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	})
	srv.GET("/gzip", func(w httpserver.ResponseWriter, r *httpserver.Request) {
		_, _ = w.WriteString(strings.Repeat("compressed-response-", 256))
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Listen(addr)
	}()

	baseURL := "http://" + addr
	waitForServer(t, baseURL+"/health")

	return baseURL, func() {
		if err := srv.Shutdown(5 * time.Second); err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("server exited with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for soak server shutdown")
		}
	}
}

func waitForServer(t *testing.T, healthURL string) {
	t.Helper()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready", healthURL)
}

func reserveLocalAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve local address: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func sendMalformedRequest(ctx context.Context, baseURL string) error {
	address := strings.TrimPrefix(baseURL, "http://")
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}

	if _, err := io.WriteString(conn, "POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: -1\r\n\r\n"); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		if isExpectedContextShutdown(ctx, err) || isExpectedMalformedReadError(err) {
			return nil
		}
		return err
	}
	if statusLine == "" {
		return nil
	}
	if !strings.HasPrefix(statusLine, "HTTP/1.1 400 Bad Request") {
		return fmt.Errorf("unexpected malformed response: %q", statusLine)
	}
	return nil
}

func isExpectedMalformedReadError(err error) bool {
	return err == io.EOF || strings.Contains(err.Error(), "use of closed network connection")
}

func isExpectedContextShutdown(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() == nil {
		return false
	}
	return err == context.Canceled || err == context.DeadlineExceeded || strings.Contains(err.Error(), "context deadline exceeded")
}

func updateMaxLatency(target *atomic.Int64, latency time.Duration) {
	latencyNanos := latency.Nanoseconds()
	for {
		current := target.Load()
		if latencyNanos <= current {
			return
		}
		if target.CompareAndSwap(current, latencyNanos) {
			return
		}
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
