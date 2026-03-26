package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComparisonStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__ComparisonResult": []any{
			map[string]any{"_docID": "cr-1", "comparisonId": "cmp-1", "sessionId": "s-1", "blockNumber": float64(5), "outcome": "clean_delivery", "claimId": "cl-1", "attestationId": "att-1", "comparedAt": "2026-01-01T00:00:00Z"},
		},
	})}
	s := NewComparisonStore(db)
	rec, err := s.Create(context.Background(), &ComparisonResultRecord{
		ComparisonID: "cmp-1", SessionID: "s-1", BlockNumber: 5,
		Outcome: "clean_delivery", ClaimID: "cl-1", AttestationID: "att-1",
		ComparedAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "cr-1", rec.DocID)
	assert.Equal(t, "cmp-1", rec.ComparisonID)
}

func TestComparisonStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewComparisonStore(db)
	rec, err := s.Create(context.Background(), &ComparisonResultRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestComparisonStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__ComparisonResult": []any{},
	})}
	s := NewComparisonStore(db)
	rec, err := s.Create(context.Background(), &ComparisonResultRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestComparisonStore_ListBySession_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__ComparisonResult": []any{
			map[string]any{"_docID": "cr-1", "sessionId": "s-1"},
			map[string]any{"_docID": "cr-2", "sessionId": "s-1"},
		},
	})}
	s := NewComparisonStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestComparisonStore_ListBySession_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__ComparisonResult": []any{},
	})}
	s := NewComparisonStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestComparisonStore_ListBySession_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewComparisonStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestComparisonStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewComparisonStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestComparisonStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewComparisonStore(db)
	rec, err := s.Create(context.Background(), &ComparisonResultRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestComparisonStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewComparisonStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestComparisonStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewComparisonStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__ComparisonResult", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestComparisonStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "cr-1"}},
	})}
	s := NewComparisonStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__ComparisonResult", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
