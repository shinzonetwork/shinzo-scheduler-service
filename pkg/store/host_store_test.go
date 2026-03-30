package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostStore_GetByPeerID_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Host": []any{
			map[string]any{"_docID": "h-1", "peerId": "peer-h1", "status": "active"},
		},
	})}
	s := NewHostStore(db)
	rec, err := s.GetByPeerID(context.Background(), "peer-h1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "h-1", rec.DocID)
	assert.Equal(t, "peer-h1", rec.PeerID)
}

func TestHostStore_GetByPeerID_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("query failed")}
	s := NewHostStore(db)
	rec, err := s.GetByPeerID(context.Background(), "peer-h1")
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestHostStore_GetByPeerID_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Host": []any{},
	})}
	s := NewHostStore(db)
	rec, err := s.GetByPeerID(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestHostStore_GetByDefraPK_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Host": []any{
			map[string]any{"_docID": "h-2", "defraPk": "pk-h2"},
		},
	})}
	s := NewHostStore(db)
	rec, err := s.GetByDefraPK(context.Background(), "pk-h2")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "h-2", rec.DocID)
}

func TestHostStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Host": []any{
			map[string]any{"_docID": "h-3", "peerId": "peer-h3"},
		},
	})}
	s := NewHostStore(db)
	rec, err := s.Create(context.Background(), &HostRecord{PeerID: "peer-h3", Status: "active"})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "h-3", rec.DocID)
}

func TestHostStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("duplicate key")}
	s := NewHostStore(db)
	rec, err := s.Create(context.Background(), &HostRecord{PeerID: "peer-h3"})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestHostStore_Create_EmptyMutationResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Host": []any{},
	})}
	s := NewHostStore(db)
	rec, err := s.Create(context.Background(), &HostRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestHostStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewHostStore(db)
	err := s.Update(context.Background(), "h-1", map[string]any{"status": "inactive"})
	require.NoError(t, err)
}

func TestHostStore_Update_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewHostStore(db)
	err := s.Update(context.Background(), "h-1", map[string]any{"status": "inactive"})
	require.Error(t, err)
}

func TestHostStore_Delete_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewHostStore(db)
	err := s.Delete(context.Background(), "h-1")
	require.NoError(t, err)
}

func TestHostStore_Delete_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewHostStore(db)
	err := s.Delete(context.Background(), "h-1")
	require.Error(t, err)
}

func TestHostStore_Count_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Host": []any{
			map[string]any{"_docID": "h-1"},
			map[string]any{"_docID": "h-2"},
			map[string]any{"_docID": "h-3"},
		},
	})}
	s := NewHostStore(db)
	n, err := s.Count(context.Background(), "active")
	require.NoError(t, err)
	assert.Equal(t, 3, n)
}

func TestHostStore_Count_Error(t *testing.T) {
	db := &mockDB{result: errResult("db error")}
	s := NewHostStore(db)
	n, err := s.Count(context.Background(), "active")
	require.Error(t, err)
	assert.Equal(t, 0, n)
}

func TestHostStore_QuerySingle_ReturnsNilWhenEmpty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Host": []any{},
	})}
	s := NewHostStore(db)
	rec, err := s.querySingle(context.Background(), `query { Scheduler__Host { _docID } }`)
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestHostStore_QueryMany_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-an-object")}
	s := NewHostStore(db)
	recs, err := s.queryMany(context.Background(), `query { Scheduler__Host { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestHostStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-an-object")}
	s := NewHostStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Host", `mutation { create_Scheduler__Host(input: {}) { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestHostStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewHostStore(db)
	recs, err := s.queryMany(context.Background(), `query { Scheduler__Host { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestHostStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewHostStore(db)
	rec, err := s.Create(context.Background(), &HostRecord{PeerID: "p1"})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestHostStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "h-1"}},
	})}
	s := NewHostStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Host", `mutation { create_Scheduler__Host(input: {}) { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
