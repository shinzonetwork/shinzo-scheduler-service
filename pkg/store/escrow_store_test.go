package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscrowStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__EscrowAccount": []any{
			map[string]any{
				"_docID": "e-1", "escrowId": "eid-1", "sessionId": "s-1",
				"hostId": "h-1", "indexerId": "i-1",
				"initialBalance": 1000.0, "currentBalance": 1000.0,
				"pricePerBlock": 0.5, "lowWaterThreshold": 100.0,
				"lowCreditSignaled": false, "gracePeriodEndsAt": "",
				"status": "active", "createdAt": "2026-01-01T00:00:00Z",
				"updatedAt": "2026-01-01T00:00:00Z",
			},
		},
	})}
	s := NewEscrowStore(db)
	rec, err := s.Create(context.Background(), &EscrowAccountRecord{
		EscrowID: "eid-1", SessionID: "s-1", HostID: "h-1", IndexerID: "i-1",
		InitialBalance: 1000, CurrentBalance: 1000, PricePerBlock: 0.5,
		LowWaterThreshold: 100, Status: "active",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "e-1", rec.DocID)
	assert.Equal(t, 1000.0, rec.InitialBalance)
}

func TestEscrowStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewEscrowStore(db)
	rec, err := s.Create(context.Background(), &EscrowAccountRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestEscrowStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__EscrowAccount": []any{},
	})}
	s := NewEscrowStore(db)
	rec, err := s.Create(context.Background(), &EscrowAccountRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestEscrowStore_GetBySession_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__EscrowAccount": []any{
			map[string]any{"_docID": "e-1", "sessionId": "s-1", "currentBalance": 800.0},
		},
	})}
	s := NewEscrowStore(db)
	rec, err := s.GetBySession(context.Background(), "s-1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "e-1", rec.DocID)
}

func TestEscrowStore_GetBySession_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__EscrowAccount": []any{},
	})}
	s := NewEscrowStore(db)
	rec, err := s.GetBySession(context.Background(), "s-missing")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestEscrowStore_GetBySession_Error(t *testing.T) {
	db := &mockDB{result: errResult("db error")}
	s := NewEscrowStore(db)
	rec, err := s.GetBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestEscrowStore_ListActive_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__EscrowAccount": []any{
			map[string]any{"_docID": "e-1", "status": "active"},
			map[string]any{"_docID": "e-2", "status": "active"},
		},
	})}
	s := NewEscrowStore(db)
	recs, err := s.ListActive(context.Background())
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestEscrowStore_ListActive_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewEscrowStore(db)
	recs, err := s.ListActive(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestEscrowStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewEscrowStore(db)
	err := s.Update(context.Background(), "e-1", map[string]any{"currentBalance": 500.0})
	require.NoError(t, err)
}

func TestEscrowStore_Update_Error(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewEscrowStore(db)
	err := s.Update(context.Background(), "e-1", map[string]any{"currentBalance": 500.0})
	require.Error(t, err)
}

func TestEscrowStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewEscrowStore(db)
	recs, err := s.ListActive(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestEscrowStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewEscrowStore(db)
	rec, err := s.Create(context.Background(), &EscrowAccountRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestEscrowStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewEscrowStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestEscrowStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewEscrowStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__EscrowAccount", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestEscrowStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "e-1"}},
	})}
	s := NewEscrowStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__EscrowAccount", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
