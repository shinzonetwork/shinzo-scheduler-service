package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLedgerStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__SessionLedger": []any{
			map[string]any{
				"_docID": "l-1", "ledgerId": "lid-1", "sessionId": "s-1",
				"hostId": "h-1", "indexerId": "i-1", "blocksVerified": float64(0),
				"creditRemaining": 100.0, "initialEscrow": 100.0,
				"pricePerBlock": 0.5, "lastComparedBlock": float64(0),
				"updatedAt": "2026-01-01T00:00:00Z",
			},
		},
	})}
	s := NewLedgerStore(db)
	rec, err := s.Create(context.Background(), &SessionLedgerRecord{
		LedgerID: "lid-1", SessionID: "s-1", HostID: "h-1", IndexerID: "i-1",
		CreditRemaining: 100, InitialEscrow: 100, PricePerBlock: 0.5,
		UpdatedAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "l-1", rec.DocID)
	assert.Equal(t, "lid-1", rec.LedgerID)
	assert.Equal(t, 100.0, rec.CreditRemaining)
}

func TestLedgerStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewLedgerStore(db)
	rec, err := s.Create(context.Background(), &SessionLedgerRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestLedgerStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__SessionLedger": []any{},
	})}
	s := NewLedgerStore(db)
	rec, err := s.Create(context.Background(), &SessionLedgerRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestLedgerStore_GetBySession_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__SessionLedger": []any{
			map[string]any{"_docID": "l-1", "sessionId": "s-1", "creditRemaining": 50.0},
		},
	})}
	s := NewLedgerStore(db)
	rec, err := s.GetBySession(context.Background(), "s-1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "l-1", rec.DocID)
}

func TestLedgerStore_GetBySession_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__SessionLedger": []any{},
	})}
	s := NewLedgerStore(db)
	rec, err := s.GetBySession(context.Background(), "s-missing")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestLedgerStore_GetBySession_Error(t *testing.T) {
	db := &mockDB{result: errResult("db error")}
	s := NewLedgerStore(db)
	rec, err := s.GetBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestLedgerStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewLedgerStore(db)
	err := s.Update(context.Background(), "l-1", map[string]any{"blocksVerified": 10})
	require.NoError(t, err)
}

func TestLedgerStore_Update_Error(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewLedgerStore(db)
	err := s.Update(context.Background(), "l-1", map[string]any{"blocksVerified": 10})
	require.Error(t, err)
}

func TestLedgerStore_ListAll_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__SessionLedger": []any{
			map[string]any{"_docID": "l-1"},
			map[string]any{"_docID": "l-2"},
		},
	})}
	s := NewLedgerStore(db)
	recs, err := s.ListAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestLedgerStore_ListAll_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewLedgerStore(db)
	recs, err := s.ListAll(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestLedgerStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewLedgerStore(db)
	recs, err := s.ListAll(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestLedgerStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewLedgerStore(db)
	rec, err := s.Create(context.Background(), &SessionLedgerRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestLedgerStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewLedgerStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestLedgerStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewLedgerStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__SessionLedger", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestLedgerStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "l-1"}},
	})}
	s := NewLedgerStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__SessionLedger", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
