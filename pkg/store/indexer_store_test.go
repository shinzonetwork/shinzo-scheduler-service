package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexerStore_GetByPeerID_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Indexer": []any{
			map[string]any{"_docID": "doc-1", "peerId": "peer-1", "status": "active"},
		},
	})}
	s := NewIndexerStore(db)
	rec, err := s.GetByPeerID(context.Background(), "peer-1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "doc-1", rec.DocID)
	assert.Equal(t, "peer-1", rec.PeerID)
	assert.Equal(t, "active", rec.Status)
}

func TestIndexerStore_GetByPeerID_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("gql error")}
	s := NewIndexerStore(db)
	rec, err := s.GetByPeerID(context.Background(), "peer-1")
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestIndexerStore_GetByPeerID_Empty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Indexer": []any{},
	})}
	s := NewIndexerStore(db)
	rec, err := s.GetByPeerID(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestIndexerStore_GetByDefraPK_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Indexer": []any{
			map[string]any{"_docID": "doc-2", "peerId": "peer-2", "defraPk": "pk-abc"},
		},
	})}
	s := NewIndexerStore(db)
	rec, err := s.GetByDefraPK(context.Background(), "pk-abc")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "doc-2", rec.DocID)
}

func TestIndexerStore_ListActive_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Indexer": []any{
			map[string]any{"_docID": "doc-1", "peerId": "p1", "status": "active"},
			map[string]any{"_docID": "doc-2", "peerId": "p2", "status": "active"},
		},
	})}
	s := NewIndexerStore(db)
	recs, err := s.ListActive(context.Background(), "eth", "mainnet")
	require.NoError(t, err)
	assert.Len(t, recs, 2)
}

func TestIndexerStore_ListActive_Error(t *testing.T) {
	db := &mockDB{result: errResult("db down")}
	s := NewIndexerStore(db)
	recs, err := s.ListActive(context.Background(), "eth", "mainnet")
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestIndexerStore_ListAllActive_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Indexer": []any{
			map[string]any{"_docID": "doc-3", "peerId": "p3", "status": "active"},
		},
	})}
	s := NewIndexerStore(db)
	recs, err := s.ListAllActive(context.Background())
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestIndexerStore_Create_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Indexer": []any{
			map[string]any{"_docID": "doc-1", "peerId": "peer-1"},
		},
	})}
	s := NewIndexerStore(db)
	rec, err := s.Create(context.Background(), &IndexerRecord{PeerID: "peer-1", Status: "active"})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "doc-1", rec.DocID)
}

func TestIndexerStore_Create_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("constraint violation")}
	s := NewIndexerStore(db)
	rec, err := s.Create(context.Background(), &IndexerRecord{PeerID: "peer-1"})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestIndexerStore_Create_EmptyMutationResult(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"create_Scheduler__Indexer": []any{},
	})}
	s := NewIndexerStore(db)
	rec, err := s.Create(context.Background(), &IndexerRecord{})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestIndexerStore_Update_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewIndexerStore(db)
	err := s.Update(context.Background(), "doc-1", map[string]any{"status": "inactive"})
	require.NoError(t, err)
}

func TestIndexerStore_Update_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewIndexerStore(db)
	err := s.Update(context.Background(), "doc-1", map[string]any{"status": "inactive"})
	require.Error(t, err)
}

func TestIndexerStore_Delete_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewIndexerStore(db)
	err := s.Delete(context.Background(), "doc-1")
	require.NoError(t, err)
}

func TestIndexerStore_Delete_GQLError(t *testing.T) {
	db := &mockDB{result: errResult("not found")}
	s := NewIndexerStore(db)
	err := s.Delete(context.Background(), "doc-1")
	require.Error(t, err)
}

func TestIndexerStore_UpdateAPIKeyHash_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{})}
	s := NewIndexerStore(db)
	err := s.UpdateAPIKeyHash(context.Background(), "doc-1", "newhash")
	require.NoError(t, err)
}

func TestIndexerStore_Count_Success(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Indexer": []any{
			map[string]any{"_docID": "doc-1"},
			map[string]any{"_docID": "doc-2"},
		},
	})}
	s := NewIndexerStore(db)
	n, err := s.Count(context.Background(), "active")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestIndexerStore_Count_Error(t *testing.T) {
	db := &mockDB{result: errResult("db error")}
	s := NewIndexerStore(db)
	n, err := s.Count(context.Background(), "active")
	require.Error(t, err)
	assert.Equal(t, 0, n)
}

func TestIndexerStore_QuerySingle_ReturnsNilWhenEmpty(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"Scheduler__Indexer": []any{},
	})}
	s := NewIndexerStore(db)
	rec, err := s.querySingle(context.Background(), `query { Scheduler__Indexer { _docID } }`)
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestIndexerStore_QueryMany_UnmarshalError(t *testing.T) {
	// A string value marshals to a JSON string, which can't unmarshal into the wrapper struct.
	db := &mockDB{result: okResult("not-an-object")}
	s := NewIndexerStore(db)
	recs, err := s.queryMany(context.Background(), `query { Scheduler__Indexer { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestIndexerStore_MutateOne_UnmarshalError(t *testing.T) {
	db := &mockDB{result: okResult("not-an-object")}
	s := NewIndexerStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Indexer", `mutation { create_Scheduler__Indexer(input: {}) { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestIndexerStore_QueryMany_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewIndexerStore(db)
	recs, err := s.ListAllActive(context.Background())
	require.Error(t, err)
	assert.Nil(t, recs)
}

func TestIndexerStore_MutateOne_MarshalError(t *testing.T) {
	db := &mockDB{result: unmarshalableResult()}
	s := NewIndexerStore(db)
	rec, err := s.Create(context.Background(), &IndexerRecord{PeerID: "p1"})
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestIndexerStore_MutateOne_MissingKey(t *testing.T) {
	db := &mockDB{result: okResult(map[string]any{
		"other_key": []any{map[string]any{"_docID": "doc-1"}},
	})}
	s := NewIndexerStore(db)
	rec, err := s.mutateOne(context.Background(), "create_Scheduler__Indexer", `mutation { create_Scheduler__Indexer(input: {}) { _docID } }`)
	require.Error(t, err)
	assert.Nil(t, rec)
}
