package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockProbeRater struct {
	rate float64
	err  error
}

func (m *mockProbeRater) RecentSuccessRate(_ context.Context, _ int) (float64, error) {
	return m.rate, m.err
}

func TestMetricsHandler_ReturnsAllFields(t *testing.T) {
	h := &MetricsHandler{
		indexerSt: &mockNodeCounter{count: 5},
		hostSt:    &mockNodeCounter{count: 3},
		subSt:     &mockSubStatusLister{},
		probeSt:   &mockProbeRater{rate: 0.95},
		log:       zap.NewNop().Sugar(),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	h.Metrics(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp metricsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 5, resp.ActiveIndexers)
	assert.Equal(t, 3, resp.ActiveHosts)
	assert.InDelta(t, 0.95, resp.ProbeSuccessRate, 0.001)
}

func TestNewMetricsHandler(t *testing.T) {
	h := NewMetricsHandler(nil, nil, nil, nil, nil)
	assert.NotNil(t, h)
}
