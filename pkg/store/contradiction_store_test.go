package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContradictionStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Contradiction": []any{
			map[string]any{"_docID": "ct-1", "evidenceId": "ev-1", "indexerId": "i-1", "snapshotRange": "{}", "probedAt": "2026-01-01T00:00:00Z", "resolved": false},
		},
	})}
	s := NewContradictionStore(db)
	rec, err := s.Create(context.Background(), &ContradictionRecord{
		EvidenceID: "ev-1", IndexerID: "i-1", SnapshotRange: "{}",
		ProbedAt: "2026-01-01T00:00:00Z", Resolved: false,
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "ct-1", rec.DocID)
	assert.Equal(t, "ev-1", rec.EvidenceID)
}

func TestContradictionStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewContradictionStore(db)
	rec, err := s.Create(context.Background(), &ContradictionRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestContradictionStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Contradiction": []any{},
	})}
	s := NewContradictionStore(db)
	rec, err := s.Create(context.Background(), &ContradictionRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestContradictionStore_ListByIndexer_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Contradiction": []any{
			map[string]any{"_docID": "ct-1", "indexerId": "i-1"},
			map[string]any{"_docID": "ct-2", "indexerId": "i-1"},
		},
	})}
	s := NewContradictionStore(db)
	recs, err := s.ListByIndexer(context.Background(), "i-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestContradictionStore_ListByIndexer_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Contradiction": []any{},
	})}
	s := NewContradictionStore(db)
	recs, err := s.ListByIndexer(context.Background(), "i-1")
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestContradictionStore_ListByIndexer_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewContradictionStore(db)
	recs, err := s.ListByIndexer(context.Background(), "i-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestContradictionStore_ListUnresolved_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Contradiction": []any{
			map[string]any{"_docID": "ct-3", "resolved": false},
		},
	})}
	s := NewContradictionStore(db)
	recs, err := s.ListUnresolved(context.Background())
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestContradictionStore_ListUnresolved_Error(t *testing.T) {
	db := &mockDB{result: errResult("db down")}
	s := NewContradictionStore(db)
	recs, err := s.ListUnresolved(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestContradictionStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewContradictionStore(db)
	err := s.Update(context.Background(), "ct-1", map[string]any{"resolved": true})
	require.NoError(t, err)
}

func TestContradictionStore_Update_Error(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewContradictionStore(db)
	err := s.Update(context.Background(), "ct-1", map[string]any{"resolved": true})
	require.Error(t, err)
}

func TestContradictionStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewContradictionStore(db)
	recs, err := s.ListByIndexer(context.Background(), "i-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestContradictionStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewContradictionStore(db)
	rec, err := s.Create(context.Background(), &ContradictionRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestContradictionStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewContradictionStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestContradictionStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewContradictionStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Contradiction", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestContradictionStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "ct-1"}},
	})}
	s := NewContradictionStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Contradiction", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
