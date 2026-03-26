package store

import (
	"context"
	"testing"

	"github.com/sourcenetwork/defradb/client"
	"github.com/sourcenetwork/defradb/client/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sequentialMockDB cycles through a list of pre-configured results per call.
type sequentialMockDB struct {
	results []*client.RequestResult
	calls   int
}

func (m *sequentialMockDB) ExecRequest(_ context.Context, _ string, _ ...options.Enumerable[options.ExecRequestOptions]) *client.RequestResult {
	if m.calls >= len(m.results) {
		return okResult(map[string]any{})
	}
	r := m.results[m.calls]
	m.calls++
	return r
}

func TestProbeStore_Insert_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__ProbeResult": []any{
			map[string]any{"_docID": "pr-1"},
		},
	})}
	s := NewProbeStore(db)
	err := s.Insert(context.Background(), &ProbeResultRecord{
		IndexerID: "idx-1", ProbedAt: "2026-01-01T00:00:00Z", Success: true, Tip: 100, LatencyMs: 42,
	})
	require.NoError(t, err)
}

func TestProbeStore_Insert_Error(t *testing.T) {
	db := &mockDB{result: errResult("write error")}
	s := NewProbeStore(db)
	err := s.Insert(context.Background(), &ProbeResultRecord{IndexerID: "idx-1"})
	require.Error(t, err)
}

func TestProbeStore_ListByIndexer_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__ProbeResult": []any{
			map[string]any{"_docID": "pr-1", "indexerId": "idx-1", "probedAt": "2026-01-01T00:00:00Z", "success": true},
			map[string]any{"_docID": "pr-2", "indexerId": "idx-1", "probedAt": "2026-01-02T00:00:00Z", "success": false},
		},
	})}
	s := NewProbeStore(db)
	recs, err := s.ListByIndexer(context.Background(), "idx-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestProbeStore_ListByIndexer_Error(t *testing.T) {
	db := &mockDB{result: errResult("query failed")}
	s := NewProbeStore(db)
	recs, err := s.ListByIndexer(context.Background(), "idx-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestProbeStore_PruneOldest_NoPruneNeeded(t *testing.T) {
	// Three records, limit 5 — nothing deleted.
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__ProbeResult": []any{
			map[string]any{"_docID": "pr-1", "indexerId": "idx-1", "probedAt": "2026-01-01T00:00:00Z"},
			map[string]any{"_docID": "pr-2", "indexerId": "idx-1", "probedAt": "2026-01-02T00:00:00Z"},
			map[string]any{"_docID": "pr-3", "indexerId": "idx-1", "probedAt": "2026-01-03T00:00:00Z"},
		},
	})}
	s := NewProbeStore(db)
	err := s.PruneOldest(context.Background(), "idx-1", 5)
	require.NoError(t, err)
}

func TestProbeStore_PruneOldest_PrunesExcess(t *testing.T) {
	// Five records, limit 3 — two oldest deleted (pr-1, pr-2).
	listResult := okResult(map[string]any{
		"Scheduler__ProbeResult": []any{
			map[string]any{"_docID": "pr-1", "probedAt": "2026-01-01T00:00:00Z"},
			map[string]any{"_docID": "pr-2", "probedAt": "2026-01-02T00:00:00Z"},
			map[string]any{"_docID": "pr-3", "probedAt": "2026-01-03T00:00:00Z"},
			map[string]any{"_docID": "pr-4", "probedAt": "2026-01-04T00:00:00Z"},
			map[string]any{"_docID": "pr-5", "probedAt": "2026-01-05T00:00:00Z"},
		},
	})
	deleteResult := okResult(map[string]any{})
	db := &sequentialMockDB{results: []*client.RequestResult{
		listResult,
		deleteResult,
		deleteResult,
	}}
	s := NewProbeStore(db)
	err := s.PruneOldest(context.Background(), "idx-1", 3)
	require.NoError(t, err)
	assert.Equal(t, 3, db.calls)
}

func TestProbeStore_PruneOldest_ErrorFromListByIndexer(t *testing.T) {
	db := &mockDB{result: errResult("list failed")}
	s := NewProbeStore(db)
	err := s.PruneOldest(context.Background(), "idx-1", 3)
	require.Error(t, err)
}

func TestProbeStore_RecentSuccessRate_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__ProbeResult": []any{},
	})}
	s := NewProbeStore(db)
	rate, err := s.RecentSuccessRate(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 0.0, rate)
}

func TestProbeStore_RecentSuccessRate_WithData(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__ProbeResult": []any{
			map[string]any{"_docID": "pr-1", "probedAt": "2026-01-01T00:00:00Z", "success": true},
			map[string]any{"_docID": "pr-2", "probedAt": "2026-01-02T00:00:00Z", "success": true},
			map[string]any{"_docID": "pr-3", "probedAt": "2026-01-03T00:00:00Z", "success": false},
			map[string]any{"_docID": "pr-4", "probedAt": "2026-01-04T00:00:00Z", "success": true},
		},
	})}
	s := NewProbeStore(db)
	// limit 3: takes pr-2(ok), pr-3(fail), pr-4(ok) → 2/3
	rate, err := s.RecentSuccessRate(context.Background(), 3)
	require.NoError(t, err)
	assert.InDelta(t, 2.0/3.0, rate, 1e-9)
}

func TestProbeStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewProbeStore(db)
	recs, err := s.ListByIndexer(context.Background(), "idx-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestProbeStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-an-object")}
	s := NewProbeStore(db)
	recs, err := s.queryMany(context.Background(), `query { Scheduler__ProbeResult { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestProbeStore_PruneOldest_DeleteError(t *testing.T) {
	listResult := okResult(map[string]any{
		"Scheduler__ProbeResult": []any{
			map[string]any{"_docID": "pr-1", "probedAt": "2026-01-01T00:00:00Z"},
			map[string]any{"_docID": "pr-2", "probedAt": "2026-01-02T00:00:00Z"},
		},
	})
	db := &sequentialMockDB{results: []*client.RequestResult{
		listResult,
		errResult("delete failed"),
	}}
	s := NewProbeStore(db)
	err := s.PruneOldest(context.Background(), "idx-1", 1)
	require.Error(t, err)
}

func TestProbeStore_RecentSuccessRate_Error(t *testing.T) {
	db := &mockDB{result: errResult("query failed")}
	s := NewProbeStore(db)
	rate, err := s.RecentSuccessRate(context.Background(), 10)
	require.Error(t, err)
	assert.Equal(t, 0.0, rate)
}

func TestSortProbeResultsAsc(t *testing.T) {
	recs := []ProbeResultRecord{
		{DocID: "c", ProbedAt: "2026-01-03T00:00:00Z"},
		{DocID: "a", ProbedAt: "2026-01-01T00:00:00Z"},
		{DocID: "b", ProbedAt: "2026-01-02T00:00:00Z"},
	}
	sortProbeResultsAsc(recs)
	assert.Equal(t, "a", recs[0].DocID)
	assert.Equal(t, "b", recs[1].DocID)
	assert.Equal(t, "c", recs[2].DocID)
}
