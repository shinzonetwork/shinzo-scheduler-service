package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// tokenBucket is a shared per-key token bucket implementation.
type tokenBucket struct {
	tokens float64
	last   time.Time
	mu     sync.Mutex
}

func newTokenBuckets() (map[string]*tokenBucket, sync.Mutex) {
	return make(map[string]*tokenBucket), sync.Mutex{}
}

func consumeToken(buckets map[string]*tokenBucket, mu *sync.Mutex, key string, rate float64, burst int) bool {
	mu.Lock()
	b, ok := buckets[key]
	if !ok {
		b = &tokenBucket{tokens: float64(burst), last: time.Now()}
		buckets[key] = b
	}
	mu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * rate
	if b.tokens > float64(burst) {
		b.tokens = float64(burst)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimit provides a simple per-IP token bucket limiter.
// Each IP gets burst tokens and refills at rate tokens/second.
func RateLimit(rate float64, burst int) func(http.Handler) http.Handler {
	buckets, mu := newTokenBuckets()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			if !consumeToken(buckets, &mu, ip, rate, burst) {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitByKey provides a per-API-key token bucket limiter. The key is
// extracted from the Bearer token set by RequireAPIKey. Falls back to the
// full token string when the peer ID cannot be parsed.
func RateLimitByKey(rate float64, burst int) func(http.Handler) http.Handler {
	buckets, mu := newTokenBuckets()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := APIKeyFromContext(r.Context())
			key := apiKey
			// Extract just the peer ID portion (first segment before the first dot).
			if parts := strings.SplitN(apiKey, ".", 3); len(parts) == 3 {
				key = parts[0]
			}
			if key == "" {
				next.ServeHTTP(w, r) // no key — let auth middleware handle rejection
				return
			}
			if !consumeToken(buckets, &mu, key, rate, burst) {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
