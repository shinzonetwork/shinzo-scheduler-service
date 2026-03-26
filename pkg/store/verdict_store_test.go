package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerdictStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Verdict": []any{
			map[string]any{"_docID": "v-1", "verdictId": "vd-1", "sessionId": "s-1", "outcome": "clean_delivery", "evidenceCids": "[]", "schedulerSignatures": "[]", "createdAt": "2026-01-01T00:00:00Z", "submittedToHub": false},
		},
	})}
	s := NewVerdictStore(db)
	rec, err := s.Create(context.Background(), &VerdictRecord{
		VerdictID: "vd-1", SessionID: "s-1", Outcome: "clean_delivery",
		EvidenceCids: "[]", SchedulerSignatures: "[]",
		CreatedAt: "2026-01-01T00:00:00Z", SubmittedToHub: false,
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "v-1", rec.DocID)
	assert.Equal(t, "vd-1", rec.VerdictID)
}

func TestVerdictStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewVerdictStore(db)
	rec, err := s.Create(context.Background(), &VerdictRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestVerdictStore_Create_EmptyResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Verdict": []any{},
	})}
	s := NewVerdictStore(db)
	rec, err := s.Create(context.Background(), &VerdictRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestVerdictStore_GetBySession_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Verdict": []any{
			map[string]any{"_docID": "v-1", "sessionId": "s-1"},
		},
	})}
	s := NewVerdictStore(db)
	rec, err := s.GetBySession(context.Background(), "s-1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "v-1", rec.DocID)
}

func TestVerdictStore_GetBySession_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Verdict": []any{},
	})}
	s := NewVerdictStore(db)
	rec, err := s.GetBySession(context.Background(), "s-1")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestVerdictStore_GetBySession_Error(t *testing.T) {
	db := &mockDB{result: errResult("db error")}
	s := NewVerdictStore(db)
	rec, err := s.GetBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestVerdictStore_ListBySession_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Verdict": []any{
			map[string]any{"_docID": "v-1", "sessionId": "s-1"},
			map[string]any{"_docID": "v-2", "sessionId": "s-1"},
		},
	})}
	s := NewVerdictStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestVerdictStore_ListBySession_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewVerdictStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestVerdictStore_ListUnsubmitted_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Verdict": []any{
			map[string]any{"_docID": "v-3", "submittedToHub": false},
		},
	})}
	s := NewVerdictStore(db)
	recs, err := s.ListUnsubmitted(context.Background())
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestVerdictStore_ListUnsubmitted_Error(t *testing.T) {
	db := &mockDB{result: errResult("db down")}
	s := NewVerdictStore(db)
	recs, err := s.ListUnsubmitted(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestVerdictStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewVerdictStore(db)
	err := s.Update(context.Background(), "v-1", map[string]any{"submittedToHub": true})
	require.NoError(t, err)
}

func TestVerdictStore_Update_Error(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewVerdictStore(db)
	err := s.Update(context.Background(), "v-1", map[string]any{"submittedToHub": true})
	require.Error(t, err)
}

func TestVerdictStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewVerdictStore(db)
	recs, err := s.ListBySession(context.Background(), "s-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestVerdictStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewVerdictStore(db)
	rec, err := s.Create(context.Background(), &VerdictRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestVerdictStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewVerdictStore(db)
	recs, err := s.queryMany(context.Background(), `query {}`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestVerdictStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-a-map")}
	s := NewVerdictStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Verdict", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestVerdictStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "v-1"}},
	})}
	s := NewVerdictStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Verdict", `mutation {}`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
