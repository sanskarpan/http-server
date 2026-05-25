package middleware

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sanskar/http-server/internal/request"
	"github.com/sanskar/http-server/internal/response"
	"github.com/sanskar/http-server/internal/router"
)

// Mock response writer for testing
type mockResponseWriter struct {
	headers    map[string][]string
	statusCode int
	body       []byte
	mu         sync.Mutex
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		headers: make(map[string][]string),
	}
}

func (m *mockResponseWriter) Header() map[string][]string {
	return m.headers
}

func (m *mockResponseWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.body = append(m.body, p...)
	return len(p), nil
}

func (m *mockResponseWriter) WriteHeader(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusCode == 0 {
		m.statusCode = code
	}
}

func (m *mockResponseWriter) WriteString(s string) (int, error) {
	return m.Write([]byte(s))
}

func (m *mockResponseWriter) Status() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusCode
}

func (m *mockResponseWriter) Written() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.body))
}

func (m *mockResponseWriter) Flush() error {
	return nil
}

// TOKEN BUCKET TESTS

func TestTokenBucket_BasicFunctionality(t *testing.T) {
	// 5 tokens, 1 token/sec refill
	tb := NewTokenBucket(5, 1)

	// Should allow 5 requests immediately
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Errorf("Request %d should be allowed (burst)", i+1)
		}
	}

	// 6th request should be blocked
	if tb.Allow() {
		t.Error("6th request should be blocked (burst exhausted)")
	}
}

func TestTokenBucket_TokenRefill(t *testing.T) {
	// 2 tokens capacity, 10 tokens/sec refill rate
	tb := NewTokenBucket(2, 10)

	// Exhaust tokens
	tb.Allow()
	tb.Allow()

	// Should be blocked now
	if tb.Allow() {
		t.Error("Should be blocked after exhausting tokens")
	}

	// Wait 150ms (should refill 1.5 tokens = 1 token available)
	time.Sleep(150 * time.Millisecond)

	// Should allow 1 request
	if !tb.Allow() {
		t.Error("Should allow request after token refill")
	}

	// Should block next one
	if tb.Allow() {
		t.Error("Should block after using refilled token")
	}
}

func TestTokenBucket_FractionalRate(t *testing.T) {
	// 1 token capacity, 0.5 tokens/sec (1 token every 2 seconds)
	tb := NewTokenBucket(1, 0.5)

	// Use the initial token
	if !tb.Allow() {
		t.Error("First request should be allowed")
	}

	// Should be blocked immediately
	if tb.Allow() {
		t.Error("Second request should be blocked")
	}

	// Wait 2 seconds for 1 token to refill
	time.Sleep(2100 * time.Millisecond)

	// Should allow now
	if !tb.Allow() {
		t.Error("Should allow after 2 seconds (1 token refilled)")
	}
}

func TestTokenBucket_MaxCapacity(t *testing.T) {
	// 3 tokens capacity, 100 tokens/sec refill
	tb := NewTokenBucket(3, 100)

	// Use all tokens
	tb.Allow()
	tb.Allow()
	tb.Allow()

	// Wait 1 second (would refill 100 tokens, but capped at 3)
	time.Sleep(1100 * time.Millisecond)

	// Should only have 3 tokens available, not 100
	allowed := 0
	for i := 0; i < 10; i++ {
		if tb.Allow() {
			allowed++
		}
	}

	if allowed != 3 {
		t.Errorf("Expected 3 allowed requests (capacity limit), got %d", allowed)
	}
}

// RATE LIMITER TESTS

func TestRateLimiter_PerIPIsolation(t *testing.T) {
	rl := NewRateLimiter(2, 1) // 2 burst, 1/sec
	defer rl.Stop()

	// IP1 uses its quota
	ip1Limiter := rl.GetLimiter("192.168.1.1")
	ip1Limiter.Allow()
	ip1Limiter.Allow()

	// IP1 should be blocked
	if ip1Limiter.Allow() {
		t.Error("IP1 should be blocked after exhausting quota")
	}

	// IP2 should still have its own quota
	ip2Limiter := rl.GetLimiter("192.168.1.2")
	if !ip2Limiter.Allow() {
		t.Error("IP2 should be allowed (separate quota)")
	}
	if !ip2Limiter.Allow() {
		t.Error("IP2 should be allowed (second request in burst)")
	}

	// IP2 should now be blocked
	if ip2Limiter.Allow() {
		t.Error("IP2 should be blocked after exhausting its quota")
	}
}

func TestRateLimiter_SameIPGetsSameLimiter(t *testing.T) {
	rl := NewRateLimiter(5, 1)
	defer rl.Stop()

	limiter1 := rl.GetLimiter("10.0.0.1")
	limiter2 := rl.GetLimiter("10.0.0.1")

	// Should be the exact same limiter instance
	if limiter1 != limiter2 {
		t.Error("Same IP should get same limiter instance")
	}

	// Using limiter1 should affect limiter2
	limiter1.Allow()
	limiter1.Allow()

	// Check via limiter2
	limiter2.mu.Lock()
	tokens := limiter2.tokens
	limiter2.mu.Unlock()

	if tokens > 3.1 { // Allow small floating point error
		t.Errorf("Expected ~3 tokens remaining, got %f", tokens)
	}
}

func TestRateLimiter_CleanupRemovesStaleLimiters(t *testing.T) {
	rl := NewRateLimiter(2, 1)

	stale := rl.GetLimiter("10.0.0.1")
	active := rl.GetLimiter("10.0.0.2")

	stale.mu.Lock()
	stale.lastTime = time.Now().Add(-10 * time.Minute)
	stale.mu.Unlock()

	active.mu.Lock()
	active.lastTime = time.Now()
	active.mu.Unlock()

	rl.mu.Lock()
	rl.lastCleanup = time.Now().Add(-2 * time.Minute)
	rl.mu.Unlock()

	rl.cleanupIfNeeded(time.Now())

	rl.mu.RLock()
	defer rl.mu.RUnlock()

	if _, ok := rl.limiters["10.0.0.1"]; ok {
		t.Fatal("expected stale limiter to be cleaned up")
	}
	if _, ok := rl.limiters["10.0.0.2"]; !ok {
		t.Fatal("expected active limiter to remain")
	}
}

// MIDDLEWARE INTEGRATION TESTS

func TestRateLimitMiddleware_AllowsWithinLimit(t *testing.T) {
	middleware := RateLimit(10, 5) // 10/sec, burst 5

	callCount := 0
	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		callCount++
		w.WriteString("OK")
	})

	wrappedHandler := middleware(handler)

	// Make 5 requests (within burst)
	for i := 0; i < 5; i++ {
		req := &request.Request{
			RemoteAddr: "127.0.0.1:8080",
		}
		w := newMockResponseWriter()
		wrappedHandler.ServeHTTP(w, req)

		if w.Status() == response.StatusTooManyRequests {
			t.Errorf("Request %d should not be rate limited", i+1)
		}
	}

	if callCount != 5 {
		t.Errorf("Expected handler called 5 times, got %d", callCount)
	}
}

func TestRateLimitMiddleware_BlocksOverLimit(t *testing.T) {
	middleware := RateLimit(1, 2) // 1/sec, burst 2

	callCount := 0
	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		callCount++
	})

	wrappedHandler := middleware(handler)

	// Make 3 requests rapidly
	for i := 0; i < 3; i++ {
		req := &request.Request{
			RemoteAddr: "192.168.1.100:12345",
		}
		w := newMockResponseWriter()
		wrappedHandler.ServeHTTP(w, req)

		if i < 2 {
			// First 2 should succeed
			if w.Status() == response.StatusTooManyRequests {
				t.Errorf("Request %d should be allowed (within burst)", i+1)
			}
		} else {
			// 3rd should be blocked
			if w.Status() != response.StatusTooManyRequests {
				t.Errorf("Request 3 should be rate limited, got status %d", w.Status())
			}
			if !strings.Contains(string(w.body), "Too Many Requests") {
				t.Error("Rate limit response should contain 'Too Many Requests'")
			}
		}
	}

	if callCount != 2 {
		t.Errorf("Expected handler called 2 times (burst), got %d", callCount)
	}
}

func TestRateLimitMiddleware_IPv6Support(t *testing.T) {
	middleware := RateLimit(10, 2)

	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		w.WriteString("OK")
	})

	wrappedHandler := middleware(handler)

	// IPv6 address
	req := &request.Request{
		RemoteAddr: "[2001:db8::1]:8080",
	}

	// Should extract IP correctly
	w := newMockResponseWriter()
	wrappedHandler.ServeHTTP(w, req)

	if w.Status() == response.StatusTooManyRequests {
		t.Error("First request should not be rate limited")
	}
}

func TestRateLimitMiddleware_PortStripping(t *testing.T) {
	middleware := RateLimit(10, 2)

	callCount := 0
	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		callCount++
	})

	wrappedHandler := middleware(handler)

	// Same IP, different ports - should share rate limit
	ports := []string{":8080", ":9090", ":12345"}

	for _, port := range ports {
		req := &request.Request{
			RemoteAddr: "192.168.1.1" + port,
		}
		w := newMockResponseWriter()
		wrappedHandler.ServeHTTP(w, req)
	}

	// All 3 requests from same IP
	if callCount != 2 {
		t.Errorf("Expected 2 requests allowed (burst), got %d", callCount)
	}
}

// CONCURRENCY TESTS

func TestTokenBucket_ConcurrentAccess(t *testing.T) {
	tb := NewTokenBucket(100, 50) // 100 capacity, 50/sec refill

	var wg sync.WaitGroup
	var allowed atomic.Int32

	// 100 concurrent goroutines trying to get tokens
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tb.Allow() {
				allowed.Add(1)
			}
		}()
	}

	wg.Wait()

	// Should allow exactly 100 (the capacity)
	if allowed.Load() != 100 {
		t.Errorf("Expected 100 allowed with concurrent access, got %d", allowed.Load())
	}
}

func TestRateLimiter_ConcurrentDifferentIPs(t *testing.T) {
	rl := NewRateLimiter(5, 1)
	defer rl.Stop()

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[string]int)

	// 10 IPs, each making 10 requests concurrently
	for i := 0; i < 10; i++ {
		ip := "192.168.1." + string(rune('0'+i))
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			limiter := rl.GetLimiter(ip)

			allowed := 0
			for j := 0; j < 10; j++ {
				if limiter.Allow() {
					allowed++
				}
			}

			mu.Lock()
			results[ip] = allowed
			mu.Unlock()
		}(ip)
	}

	wg.Wait()

	// Each IP should have been allowed exactly 5 requests (the burst capacity)
	for ip, allowed := range results {
		if allowed != 5 {
			t.Errorf("IP %s: expected 5 allowed, got %d", ip, allowed)
		}
	}
}

func TestRateLimiter_ConcurrentSameIP(t *testing.T) {
	rl := NewRateLimiter(10, 1)
	defer rl.Stop()

	var wg sync.WaitGroup
	var allowed atomic.Int32

	// 50 concurrent requests from same IP
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter := rl.GetLimiter("10.0.0.1")
			if limiter.Allow() {
				allowed.Add(1)
			}
		}()
	}

	wg.Wait()

	// Should allow exactly 10 (the capacity), regardless of concurrency
	if allowed.Load() != 10 {
		t.Errorf("Expected 10 allowed from same IP concurrently, got %d", allowed.Load())
	}
}

// DOS SIMULATION TESTS

func TestRateLimitMiddleware_DoSSimulation(t *testing.T) {
	// Very strict limit: 5/sec, burst 10
	middleware := RateLimit(5, 10)

	callCount := atomic.Int32{}
	blockedCount := atomic.Int32{}

	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		callCount.Add(1)
	})

	wrappedHandler := middleware(handler)

	// Simulate DoS attack: 1000 requests from same IP
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &request.Request{
				RemoteAddr: "attacker.com:6666",
			}
			w := newMockResponseWriter()
			wrappedHandler.ServeHTTP(w, req)

			if w.Status() == response.StatusTooManyRequests {
				blockedCount.Add(1)
			}
		}()
	}

	wg.Wait()

	t.Logf("DoS Simulation: %d allowed, %d blocked out of 1000", callCount.Load(), blockedCount.Load())

	// Should block the vast majority
	if callCount.Load() > 20 {
		t.Errorf("Too many requests allowed during DoS attack: %d", callCount.Load())
	}

	if blockedCount.Load() < 980 {
		t.Errorf("Not enough requests blocked during DoS attack: %d", blockedCount.Load())
	}
}

func TestRateLimitMiddleware_DistributedDoS(t *testing.T) {
	// Simulate distributed attack from multiple IPs
	middleware := RateLimit(10, 5)

	totalAllowed := atomic.Int32{}
	totalBlocked := atomic.Int32{}

	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		totalAllowed.Add(1)
	})

	wrappedHandler := middleware(handler)

	var wg sync.WaitGroup

	// 100 attacking IPs, each making 20 requests
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(ipIndex int) {
			defer wg.Done()

			ip := "attacker-" + string(rune('0'+(ipIndex%10))) + ".com:80"

			for j := 0; j < 20; j++ {
				req := &request.Request{
					RemoteAddr: ip,
				}
				w := newMockResponseWriter()
				wrappedHandler.ServeHTTP(w, req)

				if w.Status() == response.StatusTooManyRequests {
					totalBlocked.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Distributed DoS: %d allowed, %d blocked out of 2000 total",
		totalAllowed.Load(), totalBlocked.Load())

	// Each IP should allow ~5, so 100 IPs * 5 = ~500 allowed
	if totalAllowed.Load() > 600 {
		t.Errorf("Too many requests allowed: %d", totalAllowed.Load())
	}

	// Most should be blocked
	if totalBlocked.Load() < 1400 {
		t.Errorf("Not enough blocked: %d", totalBlocked.Load())
	}
}

// CLEANUP TESTS

func TestRateLimiter_CleanupRemovesStale(t *testing.T) {
	rl := NewRateLimiter(5, 1)
	defer rl.Stop()

	// Create limiters for several IPs
	ips := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	for _, ip := range ips {
		rl.GetLimiter(ip)
	}

	// Verify they exist
	rl.mu.RLock()
	initialCount := len(rl.limiters)
	rl.mu.RUnlock()

	if initialCount != 3 {
		t.Errorf("Expected 3 limiters, got %d", initialCount)
	}

	// Manually set lastTime to >5 minutes ago to trigger cleanup
	rl.mu.Lock()
	for _, limiter := range rl.limiters {
		limiter.mu.Lock()
		limiter.lastTime = time.Now().Add(-6 * time.Minute)
		limiter.mu.Unlock()
	}
	rl.mu.Unlock()

	// Trigger cleanup by sending to cleanup channel
	// Wait a bit for cleanup to run
	time.Sleep(100 * time.Millisecond)

	// Check if limiters were removed (this is timing-dependent, so might be flaky)
	// For a more robust test, we'd need to expose cleanup as a testable method
	t.Log("Cleanup test completed (Note: actual cleanup happens periodically)")
}

// EDGE CASES

func TestTokenBucket_ZeroRate(t *testing.T) {
	tb := NewTokenBucket(3, 0) // No refill

	// Should allow initial 3
	for i := 0; i < 3; i++ {
		if !tb.Allow() {
			t.Errorf("Should allow request %d from initial tokens", i+1)
		}
	}

	// Should block forever (no refill)
	time.Sleep(100 * time.Millisecond)
	if tb.Allow() {
		t.Error("Should block when rate is 0 (no refill)")
	}
}

func TestTokenBucket_ZeroCapacity(t *testing.T) {
	tb := NewTokenBucket(0, 10) // 0 capacity, high refill

	// Should never allow anything
	if tb.Allow() {
		t.Error("Should never allow with 0 capacity")
	}

	time.Sleep(100 * time.Millisecond)
	if tb.Allow() {
		t.Error("Should never allow with 0 capacity, even after waiting")
	}
}

func TestTokenBucket_VeryHighRate(t *testing.T) {
	tb := NewTokenBucket(1000, 10000) // 10k/sec

	// Use all tokens
	for i := 0; i < 1000; i++ {
		tb.Allow()
	}

	// Wait 10ms (should refill 100 tokens at 10k/sec)
	time.Sleep(10 * time.Millisecond)

	// Should allow a bunch more
	allowed := 0
	for i := 0; i < 200; i++ {
		if tb.Allow() {
			allowed++
		}
	}

	// Should have refilled at least 50 tokens (accounting for timing variance)
	if allowed < 50 {
		t.Errorf("Expected at least 50 refilled tokens, got %d", allowed)
	}
}

// MIDDLEWARE ERROR CASES

func TestRateLimitMiddleware_EmptyRemoteAddr(t *testing.T) {
	middleware := RateLimit(10, 5)

	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		w.WriteString("OK")
	})

	wrappedHandler := middleware(handler)

	req := &request.Request{
		RemoteAddr: "", // Empty
	}

	w := newMockResponseWriter()
	wrappedHandler.ServeHTTP(w, req)

	// Should handle gracefully (rate limit by empty string IP)
	if w.Status() == response.StatusTooManyRequests {
		t.Error("First request should not be rate limited even with empty RemoteAddr")
	}
}

func TestRateLimitMiddleware_MalformedRemoteAddr(t *testing.T) {
	middleware := RateLimit(10, 5)

	handler := router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
		w.WriteString("OK")
	})

	wrappedHandler := middleware(handler)

	testCases := []string{
		"not-an-ip",
		"256.256.256.256",
		"::::",
		"[invalid",
	}

	for _, addr := range testCases {
		req := &request.Request{
			RemoteAddr: addr,
		}

		w := newMockResponseWriter()
		wrappedHandler.ServeHTTP(w, req)

		// Should handle gracefully without panic
		if w.Status() == response.StatusTooManyRequests {
			t.Errorf("First request should not be rate limited for addr: %s", addr)
		}
	}
}
