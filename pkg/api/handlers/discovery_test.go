package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDiscoverer struct {
	tipResults      []discovery.IndexerMatch
	snapshotResults []discovery.IndexerMatch
	err             error
}

func (m *mockDiscoverer) FindForTip(_ context.Context, _ discovery.TipQuery) ([]discovery.IndexerMatch, error) {
	return m.tipResults, m.err
}

func (m *mockDiscoverer) FindForSnapshot(_ context.Context, _ discovery.SnapshotQuery) ([]discovery.IndexerMatch, error) {
	return m.snapshotResults, m.err
}

func TestDiscoveryHandler_Indexers_MissingParams(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/indexers", nil)
	h.Indexers(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDiscoveryHandler_Indexers_Success(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{
		tipResults: []discovery.IndexerMatch{{PeerID: "peer1", Chain: "eth", Network: "mainnet"}},
	}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/indexers?chain=eth&network=mainnet", nil)
	h.Indexers(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var results []discovery.IndexerMatch
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &results))
	require.Len(t, results, 1)
	assert.Equal(t, "peer1", results[0].PeerID)
}

func TestDiscoveryHandler_Snapshots_MissingParams(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/snapshots?chain=eth&network=mainnet", nil)
	h.Snapshots(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code) // missing block_from/block_to
}

func TestDiscoveryHandler_Snapshots_Success(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{
		snapshotResults: []discovery.IndexerMatch{{PeerID: "peer-snap"}},
	}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/snapshots?chain=eth&network=mainnet&block_from=100&block_to=200", nil)
	h.Snapshots(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var results []discovery.IndexerMatch
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &results))
	assert.Len(t, results, 1)
}

func TestDiscoveryHandler_Match_RoutesToSnapshot(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{
		snapshotResults: []discovery.IndexerMatch{{PeerID: "snap-peer"}},
	}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/match?chain=eth&network=mainnet&block_from=100&block_to=200", nil)
	h.Match(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var results []discovery.IndexerMatch
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &results))
	assert.Len(t, results, 1)
	assert.Equal(t, "snap-peer", results[0].PeerID)
}

func TestDiscoveryHandler_Match_RoutesToTip(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{
		tipResults: []discovery.IndexerMatch{{PeerID: "tip-peer"}},
	}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/match?chain=eth&network=mainnet", nil)
	h.Match(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var results []discovery.IndexerMatch
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &results))
	assert.Len(t, results, 1)
	assert.Equal(t, "tip-peer", results[0].PeerID)
}

func TestDiscoveryHandler_Indexers_Error(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{err: errors.New("db error")}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/indexers?chain=eth&network=mainnet", nil)
	h.Indexers(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDiscoveryHandler_Snapshots_Error(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{err: errors.New("db error")}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/snapshots?chain=eth&network=mainnet&block_from=100&block_to=200", nil)
	h.Snapshots(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDiscoveryHandler_Snapshots_MissingChainNetwork(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/snapshots?block_from=100&block_to=200", nil)
	h.Snapshots(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- parseBudget edge cases ---

func TestParseBudget_OnlyTipParam(t *testing.T) {
	q := map[string][]string{"max_tip_per_1k": {"1.5"}}
	b := parseBudget(q)
	require.NotNil(t, b)
	assert.Equal(t, 1.5, b.MaxTipPer1kBlocks)
	assert.Equal(t, 0.0, b.MaxSnapshotPerRange)
}

func TestParseBudget_OnlySnapshotParam(t *testing.T) {
	q := map[string][]string{"max_snapshot_per_range": {"3.0"}}
	b := parseBudget(q)
	require.NotNil(t, b)
	assert.Equal(t, 0.0, b.MaxTipPer1kBlocks)
	assert.Equal(t, 3.0, b.MaxSnapshotPerRange)
}

func TestParseBudget_NeitherSet(t *testing.T) {
	q := map[string][]string{}
	b := parseBudget(q)
	assert.Nil(t, b)
}

// --- getFirst edge cases ---

func TestGetFirst_MissingKey(t *testing.T) {
	q := map[string][]string{"other": {"val"}}
	result := getFirst(q, "missing")
	assert.Equal(t, "", result)
}

func TestNewDiscoveryHandler(t *testing.T) {
	h := NewDiscoveryHandler(nil)
	assert.NotNil(t, h)
}

func TestDiscoveryHandler_Snapshots_BlockFromGteBlockTo(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/snapshots?chain=eth&network=mainnet&block_from=200&block_to=100", nil)
	h.Snapshots(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDiscoveryHandler_Snapshots_BlockFromEqualsBlockTo(t *testing.T) {
	h := &DiscoveryHandler{disc: &mockDiscoverer{}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/discover/snapshots?chain=eth&network=mainnet&block_from=100&block_to=100", nil)
	h.Snapshots(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
