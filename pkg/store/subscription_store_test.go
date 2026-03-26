package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionStore_GetByID_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Subscription": []any{
			map[string]any{"_docID": "sub-1", "subscriptionId": "sid-1", "status": "active"},
		},
	})}
	s := NewSubscriptionStore(db)
	rec, err := s.GetByID(context.Background(), "sid-1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "sub-1", rec.DocID)
	assert.Equal(t, "sid-1", rec.SubscriptionID)
}

func TestSubscriptionStore_GetByID_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("query error")}
	s := NewSubscriptionStore(db)
	rec, err := s.GetByID(context.Background(), "sid-1")
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSubscriptionStore_GetByID_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Subscription": []any{},
	})}
	s := NewSubscriptionStore(db)
	rec, err := s.GetByID(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestSubscriptionStore_ListByHost_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Subscription": []any{
			map[string]any{"_docID": "sub-1", "hostId": "host-1"},
			map[string]any{"_docID": "sub-2", "hostId": "host-1"},
		},
	})}
	s := NewSubscriptionStore(db)
	recs, err := s.ListByHost(context.Background(), "host-1")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestSubscriptionStore_ListByHost_Error(t *testing.T) {
	db := &mockDB{result: errResult("db down")}
	s := NewSubscriptionStore(db)
	recs, err := s.ListByHost(context.Background(), "host-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestSubscriptionStore_ListByStatus_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Subscription": []any{
			map[string]any{"_docID": "sub-3", "status": "pending"},
		},
	})}
	s := NewSubscriptionStore(db)
	recs, err := s.ListByStatus(context.Background(), "pending")
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestSubscriptionStore_ListByStatus_Error(t *testing.T) {
	db := &mockDB{result: errResult("timeout")}
	s := NewSubscriptionStore(db)
	recs, err := s.ListByStatus(context.Background(), "pending")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestSubscriptionStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Subscription": []any{
			map[string]any{"_docID": "sub-4", "subscriptionId": "sid-4"},
		},
	})}
	s := NewSubscriptionStore(db)
	rec, err := s.Create(context.Background(), &SubscriptionRecord{SubscriptionID: "sid-4"})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "sub-4", rec.DocID)
}

func TestSubscriptionStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("write failed")}
	s := NewSubscriptionStore(db)
	rec, err := s.Create(context.Background(), &SubscriptionRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSubscriptionStore_Create_EmptyMutationResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Subscription": []any{},
	})}
	s := NewSubscriptionStore(db)
	rec, err := s.Create(context.Background(), &SubscriptionRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSubscriptionStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewSubscriptionStore(db)
	err := s.Update(context.Background(), "sub-1", map[string]any{"status": "expired"})
	require.NoError(t, err)
}

func TestSubscriptionStore_Update_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewSubscriptionStore(db)
	err := s.Update(context.Background(), "sub-1", map[string]any{"status": "expired"})
	require.Error(t, err)
}

func TestSubscriptionStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-an-object")}
	s := NewSubscriptionStore(db)
	recs, err := s.queryMany(context.Background(), `query { Scheduler__Subscription { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestSubscriptionStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-an-object")}
	s := NewSubscriptionStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Subscription", `mutation { create_Scheduler__Subscription(input: {}) { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSubscriptionStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewSubscriptionStore(db)
	recs, err := s.ListByHost(context.Background(), "host-1")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestSubscriptionStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewSubscriptionStore(db)
	rec, err := s.Create(context.Background(), &SubscriptionRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestSubscriptionStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "sub-1"}},
	})}
	s := NewSubscriptionStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Subscription", `mutation { create_Scheduler__Subscription(input: {}) { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
