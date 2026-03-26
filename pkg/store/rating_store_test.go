package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRatingStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Rating": []any{
			map[string]any{"_docID": "r-1"},
		},
	})}
	s := NewRatingStore(db)
	err := s.Create(context.Background(), &RatingRecord{
		IndexerID: "idx-1", HostID: "host-1", Score: 4.5, Comment: "good", RatedAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
}

func TestRatingStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewRatingStore(db)
	err := s.Create(context.Background(), &RatingRecord{IndexerID: "idx-1"})
	require.Error(t, err)
}

func TestRatingStore_ListByIndexer_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Rating": []any{
			map[string]any{"_docID": "r-1", "indexerId": "idx-1", "score": 4.0},
			map[string]any{"_docID": "r-2", "indexerId": "idx-1", "score": 3.5},
		},
	})}
	s := NewRatingStore(db)
	recs, err := s.ListByIndexer(context.Background(), "idx-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestRatingStore_ListByIndexer_Error(t *testing.T) {
	db := &mockDB{result: errResult("query failed")}
	s := NewRatingStore(db)
	recs, err := s.ListByIndexer(context.Background(), "idx-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestRatingStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewRatingStore(db)
	recs, err := s.ListByIndexer(context.Background(), "idx-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestRatingStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-an-object")}
	s := NewRatingStore(db)
	recs, err := s.ListByIndexer(context.Background(), "idx-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestRatingStore_ListByIndexer_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Rating": []any{},
	})}
	s := NewRatingStore(db)
	recs, err := s.ListByIndexer(context.Background(), "idx-1")
	require.NoError(t, err)
	assert.Empty(t, recs)
}
