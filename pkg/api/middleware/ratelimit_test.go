package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRateLimit_AllowsWithinBurst(t *testing.T) {
	handler := RateLimit(100, 5)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should be allowed", i+1)
	}
}

func TestRateLimit_BlocksAfterBurst(t *testing.T) {
	burst := 3
	handler := RateLimit(0, burst)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < burst; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:5000"
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Next request should be rate-limited
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5000"
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestRateLimitByKey_AllowsWithinBurst(t *testing.T) {
	handler := RateLimitByKey(100, 5)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer peer1.ts.sig")
		r = r.WithContext(context.WithValue(r.Context(), apiKeyContextKey, "peer1.ts.sig"))
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should pass", i+1)
	}
}

func TestRateLimitByKey_BlocksAfterBurst(t *testing.T) {
	handler := RateLimitByKey(0, 2)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), apiKeyContextKey, "peer2.ts.sig"))
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), apiKeyContextKey, "peer2.ts.sig"))
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestRateLimitByKey_EmptyKeyPassthrough(t *testing.T) {
	called := false
	handler := RateLimitByKey(0, 1)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// no key in context
	handler.ServeHTTP(w, r)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimitByKey_SamePeerDifferentTokenSuffix(t *testing.T) {
	handler := RateLimitByKey(0, 1)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// First request with one token variant — consumes the single burst slot.
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodGet, "/", nil)
	r1 = r1.WithContext(context.WithValue(r1.Context(), apiKeyContextKey, "peerX.ts1.sigA"))
	handler.ServeHTTP(w1, r1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Second request with different suffix but same peerID — should be limited.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2 = r2.WithContext(context.WithValue(r2.Context(), apiKeyContextKey, "peerX.ts2.sigB"))
	handler.ServeHTTP(w2, r2)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
}

func TestRateLimit_DifferentIPsIndependent(t *testing.T) {
	burst := 1
	handler := RateLimit(0, burst)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, ip := range []string{"1.1.1.1:80", "2.2.2.2:80"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = ip
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code, "first request from %s should pass", ip)
	}
}
