//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/dto"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/discovery"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	subpkg "github.com/shinzonetwork/shinzo-scheduler-service/pkg/subscription"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthEndpoint(t *testing.T) {
	h := newHarness(t)

	resp := h.doRequest(t, http.MethodGet, "/v1/health", nil, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	decodeJSON(t, resp, &body)
	assert.Equal(t, "ok", body["status"])
}

func TestStatsEndpoint(t *testing.T) {
	h := newHarness(t)

	// Seed some data so stats are non-trivial.
	h.registerIndexer(t, "idx-stats-1")
	h.registerHost(t, "host-stats-1")

	resp := h.doRequest(t, http.MethodGet, "/v1/stats", nil, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var stats dto.StatsResponse
	decodeJSON(t, resp, &stats)
	assert.GreaterOrEqual(t, stats.ActiveIndexers, 1)
	assert.GreaterOrEqual(t, stats.ActiveHosts, 1)
}

func TestIndexerRegistration(t *testing.T) {
	h := newHarness(t)
	peerID := "idx-reg-1"
	h.registerIndexer(t, peerID)

	resp := h.doRequest(t, http.MethodGet, "/v1/indexers/"+peerID, nil, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rec store.IndexerRecord
	decodeJSON(t, resp, &rec)
	assert.Equal(t, peerID, rec.PeerID)
	assert.Equal(t, store.StatusActive, rec.Status)
	// Public endpoint must not expose the API key hash.
	assert.Empty(t, rec.APIKeyHash)
}

func TestDiscovery_ReturnsActiveIndexers(t *testing.T) {
	h := newHarness(t)
	hostKey := h.registerHost(t, "host-disc-1")

	h.registerIndexer(t, "idx-disc-1")
	h.registerIndexer(t, "idx-disc-2")

	url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s", testChain, testNetwork)
	resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var matches []discovery.IndexerMatch
	decodeJSON(t, resp, &matches)
	assert.GreaterOrEqual(t, len(matches), 2, "expected at least 2 indexers")
}

func TestDiscovery_ExcludesStaleIndexers(t *testing.T) {
	h := newHarness(t)
	hostKey := h.registerHost(t, "host-stale-1")

	// Fresh indexer with recent heartbeat.
	h.registerIndexer(t, "idx-fresh-1")

	// Stale indexer: heartbeat older than StalenessWindowSeconds (120s).
	staleTime := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	h.registerIndexerWithOpts(t, &store.IndexerRecord{
		PeerID:           "idx-stale-1",
		HTTPUrl:          "http://idx-stale-1:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-stale-1",
		ReliabilityScore: 1.0,
		LastHeartbeat:    staleTime,
	})

	url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s", testChain, testNetwork)
	resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var matches []discovery.IndexerMatch
	decodeJSON(t, resp, &matches)

	found := false
	for _, m := range matches {
		if m.PeerID == "idx-stale-1" {
			found = true
		}
	}
	assert.False(t, found, "stale indexer should be excluded from discovery")
}

func TestDiscovery_TipLagExclusion(t *testing.T) {
	h := newHarness(t)
	hostKey := h.registerHost(t, "host-tip-1")

	// Leader at tip 1000.
	h.registerIndexerWithOpts(t, &store.IndexerRecord{
		PeerID:           "idx-leader",
		HTTPUrl:          "http://idx-leader:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-leader",
		CurrentTip:       1000,
		ReliabilityScore: 1.0,
	})

	// Lagging indexer: tip 900 (100 blocks behind, exceeds threshold of 50).
	h.registerIndexerWithOpts(t, &store.IndexerRecord{
		PeerID:           "idx-lagging",
		HTTPUrl:          "http://idx-lagging:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-lagging",
		CurrentTip:       900,
		ReliabilityScore: 1.0,
	})

	url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s", testChain, testNetwork)
	resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var matches []discovery.IndexerMatch
	decodeJSON(t, resp, &matches)

	for _, m := range matches {
		assert.NotEqual(t, "idx-lagging", m.PeerID, "indexer lagging >50 blocks should be excluded")
	}
}

func TestDiscovery_SnapshotPreference(t *testing.T) {
	h := newHarness(t)
	hostKey := h.registerHost(t, "host-snap-1")

	// Indexer with matching snapshot range.
	snapRanges, _ := json.Marshal([]store.SnapshotRange{
		{Start: 100, End: 200, File: "snap-100-200.tar.gz", SizeBytes: 1024},
	})
	h.registerIndexerWithOpts(t, &store.IndexerRecord{
		PeerID:           "idx-with-snap",
		HTTPUrl:          "http://idx-with-snap:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-with-snap",
		CurrentTip:       500,
		SnapshotRanges:   string(snapRanges),
		ReliabilityScore: 1.0,
	})

	// Indexer without snapshot.
	h.registerIndexerWithOpts(t, &store.IndexerRecord{
		PeerID:           "idx-no-snap",
		HTTPUrl:          "http://idx-no-snap:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-no-snap",
		CurrentTip:       500,
		ReliabilityScore: 1.0,
	})

	url := fmt.Sprintf("/v1/discover/snapshots?chain=%s&network=%s&block_from=100&block_to=200", testChain, testNetwork)
	resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var matches []discovery.IndexerMatch
	decodeJSON(t, resp, &matches)
	require.GreaterOrEqual(t, len(matches), 2)

	// Snapshot-holding indexer should appear before the fallback.
	assert.Equal(t, "idx-with-snap", matches[0].PeerID, "snapshot holder should be ranked first")
	assert.False(t, matches[0].SnapshotCreationRequired)
}

func TestDiscovery_SnapshotFallback(t *testing.T) {
	h := newHarness(t)
	hostKey := h.registerHost(t, "host-fb-1")

	// Only indexers without snapshots for the requested range.
	h.registerIndexerWithOpts(t, &store.IndexerRecord{
		PeerID:           "idx-fb-1",
		HTTPUrl:          "http://idx-fb-1:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-fb-1",
		CurrentTip:       500,
		ReliabilityScore: 1.0,
	})

	url := fmt.Sprintf("/v1/discover/snapshots?chain=%s&network=%s&block_from=300&block_to=400", testChain, testNetwork)
	resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var matches []discovery.IndexerMatch
	decodeJSON(t, resp, &matches)
	require.NotEmpty(t, matches)

	for _, m := range matches {
		assert.True(t, m.SnapshotCreationRequired, "all results should require snapshot creation")
	}
}

func TestDiscovery_PriceFilter(t *testing.T) {
	h := newHarness(t)
	hostKey := h.registerHost(t, "host-price-1")

	cheapPricing, _ := json.Marshal(store.Pricing{TipPer1kBlocks: 5.0, SnapshotPerRange: 2.0})
	h.registerIndexerWithOpts(t, &store.IndexerRecord{
		PeerID:           "idx-cheap",
		HTTPUrl:          "http://idx-cheap:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-cheap",
		CurrentTip:       500,
		Pricing:          string(cheapPricing),
		ReliabilityScore: 1.0,
	})

	expensivePricing, _ := json.Marshal(store.Pricing{TipPer1kBlocks: 500.0, SnapshotPerRange: 200.0})
	h.registerIndexerWithOpts(t, &store.IndexerRecord{
		PeerID:           "idx-expensive",
		HTTPUrl:          "http://idx-expensive:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-expensive",
		CurrentTip:       500,
		Pricing:          string(expensivePricing),
		ReliabilityScore: 1.0,
	})

	url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s&max_tip_per_1k=10&max_snapshot_per_range=5", testChain, testNetwork)
	resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var matches []discovery.IndexerMatch
	decodeJSON(t, resp, &matches)

	for _, m := range matches {
		assert.NotEqual(t, "idx-expensive", m.PeerID, "expensive indexer should be filtered out by budget")
	}
}

func TestSubscriptionLifecycle(t *testing.T) {
	h := newHarness(t)

	indexerPeer := "idx-sub-1"
	hostPeer := "host-sub-1"
	h.registerIndexer(t, indexerPeer)
	hostKey := h.registerHost(t, hostPeer)

	// 1. Create subscription (pending).
	createReq := subpkg.CreateRequest{
		HostID:    hostPeer,
		IndexerID: indexerPeer,
		SubType:   "tip",
	}
	resp := h.doRequest(t, http.MethodPost, "/v1/subscriptions", createReq, hostKey)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var sub store.SubscriptionRecord
	decodeJSON(t, resp, &sub)
	assert.Equal(t, store.StatusPending, sub.Status)
	assert.NotEmpty(t, sub.SubscriptionID)
	subID := sub.SubscriptionID

	// 2. Verify payment (activate).
	verifyReq := map[string]string{
		"subscription_id": subID,
		"payment_ref":     "test-payment-ref-001",
		"expires_at":      time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
	}
	resp = h.doRequest(t, http.MethodPost, "/v1/payments/verify", verifyReq, hostKey)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Verify subscription is now active.
	resp = h.doRequest(t, http.MethodGet, "/v1/subscriptions/"+subID, nil, hostKey)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var getResp map[string]json.RawMessage
	decodeJSON(t, resp, &getResp)
	var activeSub store.SubscriptionRecord
	require.NoError(t, json.Unmarshal(getResp["subscription"], &activeSub))
	assert.Equal(t, store.StatusActive, activeSub.Status)

	// 3. Cancel the subscription.
	resp = h.doRequest(t, http.MethodDelete, "/v1/subscriptions/"+subID, nil, hostKey)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Confirm cancelled.
	resp = h.doRequest(t, http.MethodGet, "/v1/subscriptions/"+subID, nil, hostKey)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	decodeJSON(t, resp, &getResp)
	var cancelledSub store.SubscriptionRecord
	require.NoError(t, json.Unmarshal(getResp["subscription"], &cancelledSub))
	assert.Equal(t, store.StatusCancelled, cancelledSub.Status)
}

func TestKeyRotation(t *testing.T) {
	h := newHarness(t)

	peerID := "idx-rotate-1"
	oldKey := h.registerIndexer(t, peerID)

	// Rotate with the old key.
	resp := h.doRequest(t, http.MethodPost, "/v1/auth/rotate-key", nil, oldKey)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rotateResp map[string]string
	decodeJSON(t, resp, &rotateResp)
	newKey := rotateResp["api_key"]
	require.NotEmpty(t, newKey)
	assert.NotEqual(t, oldKey, newKey)

	// Use the new key to access a protected endpoint.
	resp = h.doRequest(t, http.MethodGet, "/v1/indexers/"+peerID, nil, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}
