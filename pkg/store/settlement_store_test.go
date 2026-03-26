package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettlementStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Settlement": []any{
			map[string]any{"_docID": "st-1", "settlementId": "set-1", "batchId": "b-1", "sessionId": "s-1", "blocksVerified": float64(50), "indexerAmount": float64(1.5), "hostRefund": float64(0.5), "closeReason": "host_initiated", "txHash": "0xabc", "status": "pending", "settledAt": "2026-01-01T00:00:00Z"},
		},
	})}
	s := NewSettlementStore(db)
	rec, err := s.Create(context.Background(), &SettlementRecord{
		SettlementID: "set-1", BatchID: "b-1", SessionID: "s-1",
		BlocksVerified: 50, IndexerAmount: 1.5, HostRefund: 0.5,
		CloseReason: "host_initiated", TxHash: "0xabc",
		Status: "pending", SettledAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "st-1", rec.DocID)
	assert.Equal(t, "set-1", rec.SettlementID)
}

func TestSettlementStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewSettlementStore(db)
	rec, err := s.Create(context.Background(), &SettlementRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSettlementStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Settlement": []any{},
	})}
	s := NewSettlementStore(db)
	rec, err := s.Create(context.Background(), &SettlementRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSettlementStore_ListBySession_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Settlement": []any{
			map[string]any{"_docID": "st-1", "sessionId": "s-1"},
			map[string]any{"_docID": "st-2", "sessionId": "s-1"},
		},
	})}
	s := NewSettlementStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestSettlementStore_ListBySession_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Settlement": []any{},
	})}
	s := NewSettlementStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestSettlementStore_ListBySession_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewSettlementStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestSettlementStore_ListByBatch_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Settlement": []any{
			map[string]any{"_docID": "st-1", "batchId": "b-1"},
		},
	})}
	s := NewSettlementStore(db)
	recs, err := s.ListByBatch(context.Background(), "b-1")
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestSettlementStore_ListByBatch_Error(t *testing.T) {
	db := &mockDB{result: errResult("db down")}
	s := NewSettlementStore(db)
	recs, err := s.ListByBatch(context.Background(), "b-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestSettlementStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewSettlementStore(db)
	err := s.Update(context.Background(), "st-1", map[string]any{"status": "settled"})
	require.NoError(t, err)
}

func TestSettlementStore_Update_Error(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewSettlementStore(db)
	err := s.Update(context.Background(), "st-1", map[string]any{"status": "settled"})
	require.Error(t, err)
}

func TestSettlementStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewSettlementStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestSettlementStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewSettlementStore(db)
	rec, err := s.Create(context.Background(), &SettlementRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSettlementStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewSettlementStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestSettlementStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewSettlementStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Settlement", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSettlementStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "st-1"}},
	})}
	s := NewSettlementStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Settlement", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
