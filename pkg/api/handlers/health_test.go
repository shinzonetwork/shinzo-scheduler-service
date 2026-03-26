package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock implementations for health handler ---

type mockNodeCounter struct {
	count int
	err   error
}

func (m *mockNodeCounter) Count(_ context.Context, _ string) (int, error) {
	return m.count, m.err
}

type mockSubStatusLister struct {
	records []store.SubscriptionRecord
	err     error
}

func (m *mockSubStatusLister) ListByStatus(_ context.Context, _ string) ([]store.SubscriptionRecord, error) {
	return m.records, m.err
}

func TestHealth_ReturnsOK(t *testing.T) {
	h := &HealthHandler{
		indexerSt: &mockNodeCounter{},
		hostSt:    &mockNodeCounter{},
		subSt:     &mockSubStatusLister{},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	h.Health(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

func TestStats_ReturnsCounts(t *testing.T) {
	h := &HealthHandler{
		indexerSt: &mockNodeCounter{count: 3},
		hostSt:    &mockNodeCounter{count: 2},
		subSt: &mockSubStatusLister{records: []store.SubscriptionRecord{
			{Status: store.StatusActive},
			{Status: store.StatusActive},
		}},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	h.Stats(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.InDelta(t, 3, body["active_indexers"], 0)
	assert.InDelta(t, 2, body["active_hosts"], 0)
}

// statusAwareLister returns an error only when called with the matching status.
type statusAwareLister struct {
	failOn string
	err    error
}

func (m *statusAwareLister) ListByStatus(_ context.Context, status string) ([]store.SubscriptionRecord, error) {
	if status == m.failOn {
		return nil, m.err
	}
	return nil, nil
}

func TestStats_IndexerCountError(t *testing.T) {
	h := &HealthHandler{
		indexerSt: &mockNodeCounter{err: errors.New("db error")},
		hostSt:    &mockNodeCounter{},
		subSt:     &mockSubStatusLister{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	h.Stats(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestStats_HostCountError(t *testing.T) {
	h := &HealthHandler{
		indexerSt: &mockNodeCounter{count: 1},
		hostSt:    &mockNodeCounter{err: errors.New("db error")},
		subSt:     &mockSubStatusLister{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	h.Stats(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestStats_CountSubsActiveError(t *testing.T) {
	h := &HealthHandler{
		indexerSt: &mockNodeCounter{count: 1},
		hostSt:    &mockNodeCounter{count: 1},
		subSt:     &mockSubStatusLister{err: errors.New("db error")},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	h.Stats(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestStats_CountSubsPendingError(t *testing.T) {
	h := &HealthHandler{
		indexerSt: &mockNodeCounter{count: 1},
		hostSt:    &mockNodeCounter{count: 1},
		subSt:     &statusAwareLister{failOn: store.StatusPending, err: errors.New("db error")},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	h.Stats(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNewHealthHandler(t *testing.T) {
	h := NewHealthHandler(nil, nil, nil)
	assert.NotNil(t, h)
}
