package discovery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockIndexerLister implements indexerLister for testing.
type mockIndexerLister struct {
	records []store.IndexerRecord
	err     error
}

func (m *mockIndexerLister) ListActive(_ context.Context, chain, network string) ([]store.IndexerRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []store.IndexerRecord
	for _, r := range m.records {
		if r.Chain == chain && r.Network == network {
			out = append(out, r)
		}
	}
	return out, nil
}

// mockMatchLister implements matchHistoryLister for testing.
type mockMatchLister struct {
	records []store.MatchHistoryRecord
	err     error
}

func (m *mockMatchLister) ListByHost(_ context.Context, _ string) ([]store.MatchHistoryRecord, error) {
	return m.records, m.err
}

func newTestDiscoverer(mock *mockIndexerLister) *Discoverer {
	return NewDiscoverer(mock, nil, DiscovererConfig{})
}

func newTestDiscovererWithConfig(mock *mockIndexerLister, cfg DiscovererConfig) *Discoverer {
	return NewDiscoverer(mock, nil, cfg)
}

func makeIndexer(peerID string, reliability float64, tip int, chain, network string) store.IndexerRecord {
	return store.IndexerRecord{
		DocID:            "doc-" + peerID,
		PeerID:           peerID,
		HTTPUrl:          "http://localhost:8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/4001",
		Chain:            chain,
		Network:          network,
		CurrentTip:       tip,
		ReliabilityScore: reliability,
		Status:           store.StatusActive,
		SnapshotRanges:   "[]",
		Pricing:          `{"tipPer1kBlocks":0.1,"snapshotPerRange":1.0}`,
		LastHeartbeat:    time.Now().UTC().Format(time.RFC3339),
	}
}

func makeIndexerWithSnapshots(peerID string, reliability float64, ranges []store.SnapshotRange) store.IndexerRecord {
	raw, _ := json.Marshal(ranges)
	rec := makeIndexer(peerID, reliability, 0, "eth", "mainnet")
	rec.SnapshotRanges = string(raw)
	return rec
}

// --- FindForTip ---

func TestFindForTip_ReturnsMatchingChainNetwork(t *testing.T) {
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("peer-eth", 0.9, 100, "eth", "mainnet"),
		makeIndexer("peer-sol", 0.8, 100, "sol", "mainnet"),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "peer-eth", results[0].PeerID)
}

func TestFindForTip_MinReliabilityFilter(t *testing.T) {
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("high", 0.9, 100, "eth", "mainnet"),
		makeIndexer("low", 0.3, 100, "eth", "mainnet"),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet", MinReliability: 0.5})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "high", results[0].PeerID)
}

func TestFindForTip_SortedByReliabilityThenTip(t *testing.T) {
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("b", 0.8, 200, "eth", "mainnet"),
		makeIndexer("a", 0.9, 100, "eth", "mainnet"),
		makeIndexer("c", 0.8, 100, "eth", "mainnet"),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet"})
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "a", results[0].PeerID)
	assert.Equal(t, "b", results[1].PeerID)
	assert.Equal(t, "c", results[2].PeerID)
}

func TestFindForTip_LimitApplied(t *testing.T) {
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("p1", 0.9, 100, "eth", "mainnet"),
		makeIndexer("p2", 0.8, 100, "eth", "mainnet"),
		makeIndexer("p3", 0.7, 100, "eth", "mainnet"),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet", Limit: 2})
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestFindForTip_ZeroMinReliabilityIncludesAll(t *testing.T) {
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("p1", 0.0, 100, "eth", "mainnet"),
		makeIndexer("p2", 0.5, 100, "eth", "mainnet"),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet"})
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestFindForTip_PropagatesStoreError(t *testing.T) {
	mock := &mockIndexerLister{err: assert.AnError}
	d := newTestDiscoverer(mock)
	_, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet"})
	assert.Error(t, err)
}

// --- Staleness filtering ---

func TestFindForTip_StaleIndexerExcluded(t *testing.T) {
	fresh := makeIndexer("fresh", 0.9, 100, "eth", "mainnet")
	stale := makeIndexer("stale", 0.9, 100, "eth", "mainnet")
	stale.LastHeartbeat = time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)

	mock := &mockIndexerLister{records: []store.IndexerRecord{fresh, stale}}
	d := newTestDiscovererWithConfig(mock, DiscovererConfig{StalenessWindow: 2 * time.Minute})
	results, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "fresh", results[0].PeerID)
}

// --- Tip exclusion ---

func TestFindForTip_TipLagExclusion(t *testing.T) {
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("synced", 0.9, 1000, "eth", "mainnet"),
		makeIndexer("behind", 0.9, 900, "eth", "mainnet"), // 100 blocks behind > 50 threshold
	}}
	d := newTestDiscovererWithConfig(mock, DiscovererConfig{TipExclusionThreshold: 50})
	results, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "synced", results[0].PeerID)
}

func TestFindForTip_TipLagWithinThreshold(t *testing.T) {
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("a", 0.9, 1000, "eth", "mainnet"),
		makeIndexer("b", 0.9, 970, "eth", "mainnet"),
	}}
	d := newTestDiscovererWithConfig(mock, DiscovererConfig{TipExclusionThreshold: 50})
	results, err := d.FindForTip(context.Background(), TipQuery{Chain: "eth", Network: "mainnet"})
	require.NoError(t, err)
	assert.Len(t, results, 2) // 30 blocks behind, within threshold
}

// --- Price filtering ---

func TestFindForTip_PriceOverlapFilter(t *testing.T) {
	cheap := makeIndexer("cheap", 0.9, 100, "eth", "mainnet")
	cheap.Pricing = `{"tipPer1kBlocks":0.05,"snapshotPerRange":1.0}`
	expensive := makeIndexer("expensive", 0.9, 100, "eth", "mainnet")
	expensive.Pricing = `{"tipPer1kBlocks":5.0,"snapshotPerRange":1.0}`

	mock := &mockIndexerLister{records: []store.IndexerRecord{cheap, expensive}}
	d := newTestDiscoverer(mock)
	budget := &store.HostBudget{MaxTipPer1kBlocks: 1.0}
	results, err := d.FindForTip(context.Background(), TipQuery{
		Chain: "eth", Network: "mainnet", HostBudget: budget,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "cheap", results[0].PeerID)
	assert.True(t, results[0].ClearingPrice > 0)
}

// --- FindForSnapshot ---

func TestFindForSnapshot_OverlappingRange(t *testing.T) {
	ranges := []store.SnapshotRange{{Start: 100, End: 200}}
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexerWithSnapshots("snap", 0.9, ranges),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 150, BlockTo: 250,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Len(t, results[0].Snapshots, 1)
	assert.False(t, results[0].SnapshotCreationRequired)
}

func TestFindForSnapshot_NonOverlappingFallback(t *testing.T) {
	// Indexer with non-overlapping snapshot should appear as fallback.
	ranges := []store.SnapshotRange{{Start: 300, End: 400}}
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexerWithSnapshots("snap", 0.9, ranges),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 100, BlockTo: 200,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].SnapshotCreationRequired)
}

func TestFindForSnapshot_SnapshotHoldersRankFirst(t *testing.T) {
	withSnap := makeIndexerWithSnapshots("with", 0.5, []store.SnapshotRange{{Start: 100, End: 200}})
	noSnap := makeIndexer("without", 0.9, 100, "eth", "mainnet")

	mock := &mockIndexerLister{records: []store.IndexerRecord{noSnap, withSnap}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 100, BlockTo: 200,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "with", results[0].PeerID)
	assert.False(t, results[0].SnapshotCreationRequired)
	assert.Equal(t, "without", results[1].PeerID)
	assert.True(t, results[1].SnapshotCreationRequired)
}

func TestFindForSnapshot_ExactBoundaryMatch(t *testing.T) {
	ranges := []store.SnapshotRange{{Start: 100, End: 200}}
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexerWithSnapshots("snap", 0.9, ranges),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 200, BlockTo: 300,
	})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.False(t, results[0].SnapshotCreationRequired)
}

func TestFindForSnapshot_SortedByReliability(t *testing.T) {
	ranges := []store.SnapshotRange{{Start: 0, End: 1000}}
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexerWithSnapshots("low", 0.4, ranges),
		makeIndexerWithSnapshots("high", 0.9, ranges),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 100, BlockTo: 200,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "high", results[0].PeerID)
}

func TestFindForSnapshot_ListError(t *testing.T) {
	mock := &mockIndexerLister{err: assert.AnError}
	d := newTestDiscoverer(mock)
	_, err := d.FindForSnapshot(context.Background(), SnapshotQuery{Chain: "eth", Network: "mainnet"})
	assert.Error(t, err)
}

func TestFindForSnapshot_StaleExcluded(t *testing.T) {
	ranges := []store.SnapshotRange{{Start: 100, End: 200}}
	fresh := makeIndexerWithSnapshots("fresh", 0.9, ranges)
	stale := makeIndexerWithSnapshots("stale", 0.9, ranges)
	stale.LastHeartbeat = time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)

	mock := &mockIndexerLister{records: []store.IndexerRecord{fresh, stale}}
	d := newTestDiscovererWithConfig(mock, DiscovererConfig{StalenessWindow: 2 * time.Minute})
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 100, BlockTo: 200,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "fresh", results[0].PeerID)
}

// --- overlappingRanges (internal) ---

func TestOverlappingRanges(t *testing.T) {
	ranges := []store.SnapshotRange{
		{Start: 100, End: 200},
		{Start: 300, End: 400},
	}
	raw, _ := json.Marshal(ranges)

	tests := []struct {
		name    string
		from    int
		to      int
		wantLen int
	}{
		{"fully inside first range", 120, 180, 1},
		{"overlaps start of first", 50, 150, 1},
		{"between ranges", 210, 290, 0},
		{"covers both ranges", 50, 500, 2},
		{"empty ranges JSON", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "empty ranges JSON" {
				got := overlappingRanges("[]", tt.from, tt.to)
				assert.Len(t, got, 0)
				return
			}
			got := overlappingRanges(string(raw), tt.from, tt.to)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

func TestOverlappingRanges_EmptyString(t *testing.T) {
	got := overlappingRanges("", 0, 100)
	assert.Nil(t, got)
}

func TestOverlappingRanges_InvalidJSON(t *testing.T) {
	got := overlappingRanges("not-json", 0, 100)
	assert.Nil(t, got)
}

// --- Diversity weighting ---

func TestFindForTip_DiversityReducesRecentMatchScore(t *testing.T) {
	// Pool > 3 so diversity weighting kicks in (liquidity preservation).
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("recent", 0.9, 100, "eth", "mainnet"),
		makeIndexer("fresh", 0.9, 100, "eth", "mainnet"),
		makeIndexer("filler1", 0.5, 100, "eth", "mainnet"),
		makeIndexer("filler2", 0.5, 100, "eth", "mainnet"),
	}}
	matchMock := &mockMatchLister{records: []store.MatchHistoryRecord{
		{IndexerID: "recent", MatchedAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)},
	}}
	d := NewDiscoverer(mock, matchMock, DiscovererConfig{
		DiversityEnabled: true,
		RecencyWindow:    24 * time.Hour,
	})
	results, err := d.FindForTip(context.Background(), TipQuery{
		Chain: "eth", Network: "mainnet", HostID: "host-1",
	})
	require.NoError(t, err)
	require.Len(t, results, 4)
	// "fresh" should rank first because "recent" has a diversity penalty.
	assert.Equal(t, "fresh", results[0].PeerID)
}

// --- isStale edge cases ---

func TestIsStale_EmptyLastHeartbeat(t *testing.T) {
	d := newTestDiscovererWithConfig(&mockIndexerLister{}, DiscovererConfig{StalenessWindow: 2 * time.Minute})
	idx := makeIndexer("p1", 0.9, 100, "eth", "mainnet")
	idx.LastHeartbeat = ""
	assert.False(t, d.isStale(idx, time.Now()))
}

func TestIsStale_UnparseableHeartbeat(t *testing.T) {
	d := newTestDiscovererWithConfig(&mockIndexerLister{}, DiscovererConfig{StalenessWindow: 2 * time.Minute})
	idx := makeIndexer("p1", 0.9, 100, "eth", "mainnet")
	idx.LastHeartbeat = "not-a-timestamp"
	assert.True(t, d.isStale(idx, time.Now()))
}

func TestIsStale_ZeroStalenessWindow(t *testing.T) {
	d := newTestDiscovererWithConfig(&mockIndexerLister{}, DiscovererConfig{StalenessWindow: 0})
	idx := makeIndexer("p1", 0.9, 100, "eth", "mainnet")
	idx.LastHeartbeat = time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	assert.False(t, d.isStale(idx, time.Now()))
}

// --- loadDiversityMap edge cases ---

func TestLoadDiversityMap_MatchStError(t *testing.T) {
	matchMock := &mockMatchLister{err: assert.AnError}
	d := NewDiscoverer(&mockIndexerLister{}, matchMock, DiscovererConfig{
		DiversityEnabled: true,
		RecencyWindow:    24 * time.Hour,
	})
	result := d.loadDiversityMap(context.Background(), "host-1")
	assert.Nil(t, result)
}

func TestLoadDiversityMap_EmptyHostID(t *testing.T) {
	matchMock := &mockMatchLister{records: []store.MatchHistoryRecord{
		{IndexerID: "idx-1", MatchedAt: time.Now().UTC().Format(time.RFC3339)},
	}}
	d := NewDiscoverer(&mockIndexerLister{}, matchMock, DiscovererConfig{
		DiversityEnabled: true,
		RecencyWindow:    24 * time.Hour,
	})
	result := d.loadDiversityMap(context.Background(), "")
	assert.Nil(t, result)
}

func TestLoadDiversityMap_DiversityDisabled(t *testing.T) {
	matchMock := &mockMatchLister{records: []store.MatchHistoryRecord{
		{IndexerID: "idx-1", MatchedAt: time.Now().UTC().Format(time.RFC3339)},
	}}
	d := NewDiscoverer(&mockIndexerLister{}, matchMock, DiscovererConfig{
		DiversityEnabled: false,
		RecencyWindow:    24 * time.Hour,
	})
	result := d.loadDiversityMap(context.Background(), "host-1")
	assert.Nil(t, result)
}

func TestLoadDiversityMap_UnparseableMatchedAt(t *testing.T) {
	matchMock := &mockMatchLister{records: []store.MatchHistoryRecord{
		{IndexerID: "idx-1", MatchedAt: "garbage-time"},
		{IndexerID: "idx-2", MatchedAt: time.Now().UTC().Format(time.RFC3339)},
	}}
	d := NewDiscoverer(&mockIndexerLister{}, matchMock, DiscovererConfig{
		DiversityEnabled: true,
		RecencyWindow:    24 * time.Hour,
	})
	result := d.loadDiversityMap(context.Background(), "host-1")
	// idx-1 skipped due to unparseable time, idx-2 present
	assert.Len(t, result, 1)
	_, ok := result["idx-2"]
	assert.True(t, ok)
	_, ok = result["idx-1"]
	assert.False(t, ok)
}

// --- parsePricing edge cases ---

func TestParsePricing_EmptyString(t *testing.T) {
	result := parsePricing("")
	assert.Nil(t, result)
}

func TestParsePricing_EmptyObject(t *testing.T) {
	result := parsePricing("{}")
	assert.Nil(t, result)
}

func TestParsePricing_InvalidJSON(t *testing.T) {
	result := parsePricing("not-json{")
	assert.Nil(t, result)
}

// --- FindForSnapshot with price filter and clearing price ---

func TestFindForSnapshot_PriceFilterAndClearingPrice(t *testing.T) {
	cheap := makeIndexerWithSnapshots("cheap", 0.9, []store.SnapshotRange{{Start: 100, End: 200}})
	cheap.Pricing = `{"tipPer1kBlocks":0.05,"snapshotPerRange":1.0}`
	expensive := makeIndexerWithSnapshots("expensive", 0.9, []store.SnapshotRange{{Start: 100, End: 200}})
	expensive.Pricing = `{"tipPer1kBlocks":0.05,"snapshotPerRange":50.0}`

	mock := &mockIndexerLister{records: []store.IndexerRecord{cheap, expensive}}
	d := newTestDiscoverer(mock)
	budget := &store.HostBudget{MaxSnapshotPerRange: 5.0}
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 100, BlockTo: 200,
		HostBudget: budget,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "cheap", results[0].PeerID)
	assert.True(t, results[0].ClearingPrice > 0)
}

// --- FindForSnapshot with staleness filtering ---

func TestFindForSnapshot_LimitApplied(t *testing.T) {
	ranges := []store.SnapshotRange{{Start: 0, End: 1000}}
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexerWithSnapshots("a", 0.9, ranges),
		makeIndexerWithSnapshots("b", 0.8, ranges),
		makeIndexerWithSnapshots("c", 0.7, ranges),
	}}
	d := newTestDiscoverer(mock)
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 100, BlockTo: 200, Limit: 2,
	})
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestFindForSnapshot_StalenessFilteringExcludesUnparseable(t *testing.T) {
	good := makeIndexerWithSnapshots("good", 0.9, []store.SnapshotRange{{Start: 100, End: 200}})
	bad := makeIndexerWithSnapshots("bad", 0.9, []store.SnapshotRange{{Start: 100, End: 200}})
	bad.LastHeartbeat = "unparseable"

	mock := &mockIndexerLister{records: []store.IndexerRecord{good, bad}}
	d := newTestDiscovererWithConfig(mock, DiscovererConfig{StalenessWindow: 2 * time.Minute})
	results, err := d.FindForSnapshot(context.Background(), SnapshotQuery{
		Chain: "eth", Network: "mainnet", BlockFrom: 100, BlockTo: 200,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "good", results[0].PeerID)
}

func TestFindForTip_DiversitySkippedForSmallPool(t *testing.T) {
	// Pool size <= 3, diversity should be skipped (liquidity preservation).
	mock := &mockIndexerLister{records: []store.IndexerRecord{
		makeIndexer("recent", 0.9, 100, "eth", "mainnet"),
		makeIndexer("other", 0.8, 100, "eth", "mainnet"),
	}}
	matchMock := &mockMatchLister{records: []store.MatchHistoryRecord{
		{IndexerID: "recent", MatchedAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)},
	}}
	d := NewDiscoverer(mock, matchMock, DiscovererConfig{
		DiversityEnabled: true,
		RecencyWindow:    24 * time.Hour,
	})
	results, err := d.FindForTip(context.Background(), TipQuery{
		Chain: "eth", Network: "mainnet", HostID: "host-1",
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	// "recent" should still be first because pool is small (<=3), diversity skipped.
	assert.Equal(t, "recent", results[0].PeerID)
}
