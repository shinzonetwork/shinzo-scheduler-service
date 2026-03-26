package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttestationStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Attestation": []any{
			map[string]any{"_docID": "a-1", "attestationId": "att-1", "sessionId": "s-1", "hostId": "h-1", "blockNumber": float64(10), "docCidsReceived": "[]", "batchSignature": "sig", "submittedAt": "2026-01-01T00:00:00Z", "status": "pending"},
		},
	})}
	s := NewAttestationStore(db)
	rec, err := s.Create(context.Background(), &AttestationRecord{
		AttestationID: "att-1", SessionID: "s-1", HostID: "h-1",
		BlockNumber: 10, DocCidsReceived: "[]", BatchSignature: "sig",
		SubmittedAt: "2026-01-01T00:00:00Z", Status: "pending",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "a-1", rec.DocID)
	assert.Equal(t, "att-1", rec.AttestationID)
}

func TestAttestationStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewAttestationStore(db)
	rec, err := s.Create(context.Background(), &AttestationRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestAttestationStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Attestation": []any{},
	})}
	s := NewAttestationStore(db)
	rec, err := s.Create(context.Background(), &AttestationRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestAttestationStore_GetBySessionAndBlock_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Attestation": []any{
			map[string]any{"_docID": "a-1", "sessionId": "s-1", "blockNumber": float64(42)},
		},
	})}
	s := NewAttestationStore(db)
	rec, err := s.GetBySessionAndBlock(context.Background(), "s-1", 42)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "a-1", rec.DocID)
}

func TestAttestationStore_GetBySessionAndBlock_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Attestation": []any{},
	})}
	s := NewAttestationStore(db)
	rec, err := s.GetBySessionAndBlock(context.Background(), "s-1", 99)
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestAttestationStore_GetBySessionAndBlock_Error(t *testing.T) {
	db := &mockDB{result: errResult("db error")}
	s := NewAttestationStore(db)
	rec, err := s.GetBySessionAndBlock(context.Background(), "s-1", 99)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestAttestationStore_ListBySession_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Attestation": []any{
			map[string]any{"_docID": "a-1", "sessionId": "s-1"},
			map[string]any{"_docID": "a-2", "sessionId": "s-1"},
		},
	})}
	s := NewAttestationStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestAttestationStore_ListBySession_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewAttestationStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestAttestationStore_ListPending_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Attestation": []any{
			map[string]any{"_docID": "a-3", "status": "pending"},
		},
	})}
	s := NewAttestationStore(db)
	recs, err := s.ListPending(context.Background())
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestAttestationStore_ListPending_Error(t *testing.T) {
	db := &mockDB{result: errResult("db down")}
	s := NewAttestationStore(db)
	recs, err := s.ListPending(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestAttestationStore_UpdateStatus_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewAttestationStore(db)
	err := s.UpdateStatus(context.Background(), "a-1", "verified")
	require.NoError(t, err)
}

func TestAttestationStore_UpdateStatus_Error(t *testing.T) {
	db := &mockDB{result: errResult("update failed")}
	s := NewAttestationStore(db)
	err := s.UpdateStatus(context.Background(), "a-1", "verified")
	require.Error(t, err)
}

func TestAttestationStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewAttestationStore(db)
	err := s.Update(context.Background(), "a-1", map[string]any{"status": "verified"})
	require.NoError(t, err)
}

func TestAttestationStore_Update_Error(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewAttestationStore(db)
	err := s.Update(context.Background(), "a-1", map[string]any{"status": "verified"})
	require.Error(t, err)
}

func TestAttestationStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewAttestationStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestAttestationStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewAttestationStore(db)
	rec, err := s.Create(context.Background(), &AttestationRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestAttestationStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewAttestationStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestAttestationStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewAttestationStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Attestation", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestAttestationStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "a-1"}},
	})}
	s := NewAttestationStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Attestation", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
