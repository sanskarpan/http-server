package middleware

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sanskarpan/http-server/internal/request"
	"github.com/sanskarpan/http-server/internal/response"
	"github.com/sanskarpan/http-server/internal/router"
)

// TokenBucket implements the token bucket algorithm
type TokenBucket struct {
	tokens   float64
	capacity float64
	rate     float64 // tokens per second
	lastTime time.Time
	mu       sync.Mutex
}

// NewTokenBucket creates a new token bucket
func NewTokenBucket(capacity, rate float64) *TokenBucket {
	return &TokenBucket{
		tokens:   capacity,
		capacity: capacity,
		rate:     rate,
		lastTime: time.Now(),
	}
}

// Allow checks if a request is allowed
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Add tokens based on elapsed time
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	// Check if we have at least one token
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}

	return false
}

// RateLimiter manages rate limiters per IP
type RateLimiter struct {
	limiters     map[string]*TokenBucket
	capacity     float64
	rate         float64
	mu           sync.RWMutex
	lastCleanup  time.Time
	cleanupEvery time.Duration
	staleAfter   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(capacity, rate float64) *RateLimiter {
	rl := &RateLimiter{
		limiters:     make(map[string]*TokenBucket),
		capacity:     capacity,
		rate:         rate,
		lastCleanup:  time.Now(),
		cleanupEvery: time.Minute,
		staleAfter:   5 * time.Minute,
	}

	return rl
}

// GetLimiter returns a limiter for an IP
func (rl *RateLimiter) GetLimiter(ip string) *TokenBucket {
	rl.cleanupIfNeeded(time.Now())

	rl.mu.RLock()
	limiter, exists := rl.limiters[ip]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	limiter, exists = rl.limiters[ip]
	if exists {
		return limiter
	}

	// Create new limiter
	limiter = NewTokenBucket(rl.capacity, rl.rate)
	rl.limiters[ip] = limiter
	return limiter
}

// Stop stops the rate limiter cleanup
func (rl *RateLimiter) Stop() {
	// No background cleanup is running.
}

func (rl *RateLimiter) cleanupIfNeeded(now time.Time) {
	rl.mu.RLock()
	shouldCleanup := now.Sub(rl.lastCleanup) >= rl.cleanupEvery
	rl.mu.RUnlock()
	if !shouldCleanup {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if now.Sub(rl.lastCleanup) < rl.cleanupEvery {
		return
	}

	for ip, limiter := range rl.limiters {
		limiter.mu.Lock()
		lastSeen := limiter.lastTime
		limiter.mu.Unlock()
		if now.Sub(lastSeen) > rl.staleAfter {
			delete(rl.limiters, ip)
		}
	}

	rl.lastCleanup = now
}

// RateLimit returns a rate limiting middleware
func RateLimit(requestsPerSecond float64, burst int) router.Middleware {
	limiter := NewRateLimiter(float64(burst), requestsPerSecond)

	return func(next router.Handler) router.Handler {
		return router.HandlerFunc(func(w response.ResponseWriter, r *request.Request) {
			// Extract IP from RemoteAddr
			ip := r.RemoteAddr
			if host, _, err := net.SplitHostPort(ip); err == nil {
				ip = host
			} else if strings.HasPrefix(ip, "[") && strings.Contains(ip, "]") {
				ip = strings.TrimPrefix(strings.SplitN(ip, "]", 2)[0], "[")
			}

			// Get limiter for this IP
			bucket := limiter.GetLimiter(ip)

			// Check if request is allowed
			if !bucket.Allow() {
				w.Header()["Content-Type"] = []string{"text/plain"}
				w.WriteHeader(response.StatusTooManyRequests)
				w.WriteString("Too Many Requests\n")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
