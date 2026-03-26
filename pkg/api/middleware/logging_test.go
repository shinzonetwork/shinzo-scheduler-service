package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestLogging_PassesThrough(t *testing.T) {
	log := zap.NewNop().Sugar()
	handler := Logging(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ping", nil)
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestLogging_CapturesCustomStatus(t *testing.T) {
	log := zap.NewNop().Sugar()
	handler := Logging(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/fail", nil)
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
