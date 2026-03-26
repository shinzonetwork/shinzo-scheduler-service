package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__DeliveryClaim": []any{
			map[string]any{"_docID": "c-1", "claimId": "cid-1", "sessionId": "s-1", "indexerId": "i-1", "blockNumber": float64(100), "status": "pending"},
		},
	})}
	s := NewClaimStore(db)
	rec, err := s.Create(context.Background(), &DeliveryClaimRecord{
		ClaimID: "cid-1", SessionID: "s-1", IndexerID: "i-1",
		BlockNumber: 100, Status: "pending",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "c-1", rec.DocID)
	assert.Equal(t, "cid-1", rec.ClaimID)
}

func TestClaimStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewClaimStore(db)
	rec, err := s.Create(context.Background(), &DeliveryClaimRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestClaimStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__DeliveryClaim": []any{},
	})}
	s := NewClaimStore(db)
	rec, err := s.Create(context.Background(), &DeliveryClaimRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestClaimStore_GetBySessionAndBlock_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__DeliveryClaim": []any{
			map[string]any{"_docID": "c-1", "sessionId": "s-1", "blockNumber": float64(42)},
		},
	})}
	s := NewClaimStore(db)
	rec, err := s.GetBySessionAndBlock(context.Background(), "s-1", 42)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "c-1", rec.DocID)
}

func TestClaimStore_GetBySessionAndBlock_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__DeliveryClaim": []any{},
	})}
	s := NewClaimStore(db)
	rec, err := s.GetBySessionAndBlock(context.Background(), "s-1", 99)
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestClaimStore_GetBySessionAndBlock_Error(t *testing.T) {
	db := &mockDB{result: errResult("db error")}
	s := NewClaimStore(db)
	rec, err := s.GetBySessionAndBlock(context.Background(), "s-1", 99)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestClaimStore_ListBySession_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__DeliveryClaim": []any{
			map[string]any{"_docID": "c-1", "sessionId": "s-1"},
			map[string]any{"_docID": "c-2", "sessionId": "s-1"},
		},
	})}
	s := NewClaimStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestClaimStore_ListBySession_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewClaimStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestClaimStore_ListPending_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__DeliveryClaim": []any{
			map[string]any{"_docID": "c-3", "status": "pending"},
		},
	})}
	s := NewClaimStore(db)
	recs, err := s.ListPending(context.Background())
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestClaimStore_ListPending_Error(t *testing.T) {
	db := &mockDB{result: errResult("db down")}
	s := NewClaimStore(db)
	recs, err := s.ListPending(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestClaimStore_UpdateStatus_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewClaimStore(db)
	err := s.UpdateStatus(context.Background(), "c-1", "verified")
	require.NoError(t, err)
}

func TestClaimStore_UpdateStatus_Error(t *testing.T) {
	db := &mockDB{result: errResult("update failed")}
	s := NewClaimStore(db)
	err := s.UpdateStatus(context.Background(), "c-1", "verified")
	require.Error(t, err)
}

func TestClaimStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewClaimStore(db)
	err := s.Update(context.Background(), "c-1", map[string]any{"status": "verified"})
	require.NoError(t, err)
}

func TestClaimStore_Update_Error(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewClaimStore(db)
	err := s.Update(context.Background(), "c-1", map[string]any{"status": "verified"})
	require.Error(t, err)
}

func TestClaimStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewClaimStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestClaimStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewClaimStore(db)
	rec, err := s.Create(context.Background(), &DeliveryClaimRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestClaimStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewClaimStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestClaimStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewClaimStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__DeliveryClaim", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestClaimStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "c-1"}},
	})}
	s := NewClaimStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__DeliveryClaim", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
