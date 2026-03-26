package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__MatchHistory": []any{
			map[string]any{"_docID": "m-1", "matchId": "mid-1", "hostId": "h-1", "indexerId": "i-1", "matchType": "tip", "matchedAt": "2026-01-01T00:00:00Z", "clearingPrice": 5.5},
		},
	})}
	s := NewMatchStore(db)
	rec, err := s.Create(context.Background(), &MatchHistoryRecord{
		MatchID: "mid-1", HostID: "h-1", IndexerID: "i-1",
		MatchType: "tip", MatchedAt: "2026-01-01T00:00:00Z", ClearingPrice: 5.5,
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "m-1", rec.DocID)
	assert.Equal(t, "mid-1", rec.MatchID)
	assert.Equal(t, 5.5, rec.ClearingPrice)
}

func TestMatchStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewMatchStore(db)
	rec, err := s.Create(context.Background(), &MatchHistoryRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestMatchStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__MatchHistory": []any{},
	})}
	s := NewMatchStore(db)
	rec, err := s.Create(context.Background(), &MatchHistoryRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestMatchStore_ListByHost_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__MatchHistory": []any{
			map[string]any{"_docID": "m-1", "hostId": "h-1"},
			map[string]any{"_docID": "m-2", "hostId": "h-1"},
		},
	})}
	s := NewMatchStore(db)
	recs, err := s.ListByHost(context.Background(), "h-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestMatchStore_ListByHost_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__MatchHistory": []any{},
	})}
	s := NewMatchStore(db)
	recs, err := s.ListByHost(context.Background(), "h-none")
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestMatchStore_ListByHost_Error(t *testing.T) {
	db := &mockDB{result: errResult("db down")}
	s := NewMatchStore(db)
	recs, err := s.ListByHost(context.Background(), "h-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestMatchStore_ListByHostAndIndexer_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__MatchHistory": []any{
			map[string]any{"_docID": "m-3", "hostId": "h-1", "indexerId": "i-2"},
		},
	})}
	s := NewMatchStore(db)
	recs, err := s.ListByHostAndIndexer(context.Background(), "h-1", "i-2")
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestMatchStore_ListByHostAndIndexer_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewMatchStore(db)
	recs, err := s.ListByHostAndIndexer(context.Background(), "h-1", "i-2")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestMatchStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewMatchStore(db)
	recs, err := s.ListByHost(context.Background(), "h-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestMatchStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewMatchStore(db)
	rec, err := s.Create(context.Background(), &MatchHistoryRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestMatchStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewMatchStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestMatchStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewMatchStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__MatchHistory", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestMatchStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "m-1"}},
	})}
	s := NewMatchStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__MatchHistory", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
