package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireAPIKey(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := APIKeyFromContext(r.Context())
		w.Header().Set("X-Got-Key", key)
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAPIKey(next)

	t.Run("missing header returns 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("non-bearer auth returns 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Basic abc123")
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("empty token returns 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer ")
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("valid bearer token passes through", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer peer1.20260313T120000Z.abcdef")
		handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "peer1.20260313T120000Z.abcdef", w.Header().Get("X-Got-Key"))
	})
}

func TestAPIKeyFromContext_Empty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	require.Empty(t, APIKeyFromContext(r.Context()))
}
