package settlement

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock contradiction store ---

type mockContradictionStore struct {
	records      []store.ContradictionRecord
	listErr      error
	resolveErr   error
	resolvedDocs []string
}

func (m *mockContradictionStore) ListUnresolved(_ context.Context) ([]store.ContradictionRecord, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []store.ContradictionRecord
	for _, r := range m.records {
		if !r.Resolved {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *mockContradictionStore) MarkResolved(_ context.Context, docID string) error {
	if m.resolveErr != nil {
		return m.resolveErr
	}
	m.resolvedDocs = append(m.resolvedDocs, docID)
	for i := range m.records {
		if m.records[i].DocID == docID {
			m.records[i].Resolved = true
		}
	}
	return nil
}

// --- mock hub that tracks slash calls ---

type mockSlashHub struct {
	NoopBroadcaster
	slashCalls []MsgSlash
	slashErr   error
}

func (m *mockSlashHub) BroadcastSlash(_ context.Context, msg MsgSlash) (string, error) {
	m.slashCalls = append(m.slashCalls, msg)
	if m.slashErr != nil {
		return "", m.slashErr
	}
	return "tx-slash", nil
}

func contradictionCfg() config.SettlementConfig {
	return config.SettlementConfig{
		Enabled:                           true,
		ContradictionThreshold:            3,
		ContradictionCheckIntervalSeconds: 1,
	}
}

func makeContradiction(docID, indexerID, evidenceID string) store.ContradictionRecord {
	return store.ContradictionRecord{
		DocID:      docID,
		EvidenceID: evidenceID,
		IndexerID:  indexerID,
		ProbedAt:   time.Now().UTC().Format(time.RFC3339),
		Resolved:   false,
	}
}

func TestCheckAndSlash_NoUnresolved(t *testing.T) {
	st := &mockContradictionStore{}
	hub := &mockSlashHub{}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	aggr.CheckAndSlash(context.Background())

	assert.Empty(t, hub.slashCalls)
	assert.Empty(t, st.resolvedDocs)
}

func TestCheckAndSlash_BelowThreshold(t *testing.T) {
	st := &mockContradictionStore{
		records: []store.ContradictionRecord{
			makeContradiction("d1", "idx-1", "ev-1"),
			makeContradiction("d2", "idx-1", "ev-2"),
		},
	}
	hub := &mockSlashHub{}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	aggr.CheckAndSlash(context.Background())

	assert.Empty(t, hub.slashCalls, "should not slash when below threshold")
	assert.Empty(t, st.resolvedDocs)
}

func TestCheckAndSlash_AtThreshold(t *testing.T) {
	st := &mockContradictionStore{
		records: []store.ContradictionRecord{
			makeContradiction("d1", "idx-1", "ev-1"),
			makeContradiction("d2", "idx-1", "ev-2"),
			makeContradiction("d3", "idx-1", "ev-3"),
		},
	}
	hub := &mockSlashHub{}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	aggr.CheckAndSlash(context.Background())

	require.Len(t, hub.slashCalls, 1)
	assert.Equal(t, "idx-1", hub.slashCalls[0].IndexerID)
	assert.Contains(t, hub.slashCalls[0].Reason, "3 unresolved")

	// All evidence IDs should be in the comma-separated list.
	for _, evID := range []string{"ev-1", "ev-2", "ev-3"} {
		assert.True(t, strings.Contains(hub.slashCalls[0].EvidenceCID, evID),
			"expected %s in evidence CID", evID)
	}

	// All docs should be marked resolved.
	assert.ElementsMatch(t, []string{"d1", "d2", "d3"}, st.resolvedDocs)
}

func TestCheckAndSlash_MultipleIndexers(t *testing.T) {
	st := &mockContradictionStore{
		records: []store.ContradictionRecord{
			// idx-1: 3 contradictions (at threshold)
			makeContradiction("d1", "idx-1", "ev-1"),
			makeContradiction("d2", "idx-1", "ev-2"),
			makeContradiction("d3", "idx-1", "ev-3"),
			// idx-2: 2 contradictions (below threshold)
			makeContradiction("d4", "idx-2", "ev-4"),
			makeContradiction("d5", "idx-2", "ev-5"),
		},
	}
	hub := &mockSlashHub{}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	aggr.CheckAndSlash(context.Background())

	require.Len(t, hub.slashCalls, 1)
	assert.Equal(t, "idx-1", hub.slashCalls[0].IndexerID)

	// Only idx-1 docs should be resolved.
	assert.ElementsMatch(t, []string{"d1", "d2", "d3"}, st.resolvedDocs)
}

func TestCheckAndSlash_BroadcastFailure(t *testing.T) {
	st := &mockContradictionStore{
		records: []store.ContradictionRecord{
			makeContradiction("d1", "idx-1", "ev-1"),
			makeContradiction("d2", "idx-1", "ev-2"),
			makeContradiction("d3", "idx-1", "ev-3"),
		},
	}
	hub := &mockSlashHub{slashErr: fmt.Errorf("hub down")}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	aggr.CheckAndSlash(context.Background())

	require.Len(t, hub.slashCalls, 1)
	// Contradictions must NOT be marked resolved when broadcast fails.
	assert.Empty(t, st.resolvedDocs, "should not mark resolved on broadcast failure")
}

func TestCheckAndSlash_ListError(t *testing.T) {
	st := &mockContradictionStore{listErr: fmt.Errorf("db error")}
	hub := &mockSlashHub{}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	// Should not panic.
	aggr.CheckAndSlash(context.Background())

	assert.Empty(t, hub.slashCalls)
}

func TestCheckAndSlash_MarkResolvedError(t *testing.T) {
	st := &mockContradictionStore{
		records: []store.ContradictionRecord{
			makeContradiction("d1", "idx-1", "ev-1"),
			makeContradiction("d2", "idx-1", "ev-2"),
			makeContradiction("d3", "idx-1", "ev-3"),
		},
		resolveErr: fmt.Errorf("update failed"),
	}
	hub := &mockSlashHub{}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	// Should not panic; slash is still broadcast.
	aggr.CheckAndSlash(context.Background())

	require.Len(t, hub.slashCalls, 1)
}

func TestContradictionAggregator_StartStop(t *testing.T) {
	st := &mockContradictionStore{}
	hub := &mockSlashHub{}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	aggr.Start(ctx)
	// Let it run briefly, then stop gracefully.
	aggr.Stop()
}

func TestContradictionAggregator_TickerFires(t *testing.T) {
	st := &mockContradictionStore{
		records: []store.ContradictionRecord{
			makeContradiction("d1", "idx-1", "ev-1"),
			makeContradiction("d2", "idx-1", "ev-2"),
			makeContradiction("d3", "idx-1", "ev-3"),
		},
	}
	hub := &mockSlashHub{}
	cfg := contradictionCfg()
	cfg.ContradictionCheckIntervalSeconds = 1
	aggr := NewContradictionAggregator(st, hub, cfg, testLogger())

	ctx := context.Background()
	aggr.Start(ctx)
	time.Sleep(1500 * time.Millisecond)
	aggr.Stop()

	require.Len(t, hub.slashCalls, 1)
	assert.Equal(t, "idx-1", hub.slashCalls[0].IndexerID)
}

func TestContradictionAggregator_ContextCancellation(t *testing.T) {
	st := &mockContradictionStore{}
	hub := &mockSlashHub{}
	aggr := NewContradictionAggregator(st, hub, contradictionCfg(), testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	aggr.Start(ctx)
	cancel()
	// wg.Wait should return promptly after context cancellation.
	aggr.wg.Wait()
}
