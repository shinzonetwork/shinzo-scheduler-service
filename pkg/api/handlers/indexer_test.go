package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/middleware"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mocks ---

type mockIndexerRegistry struct {
	record    *store.IndexerRecord
	resp      *registry.RegisterIndexerResponse
	err       error
	heartbeat error
}

func (m *mockIndexerRegistry) Register(_ context.Context, _ registry.RegisterIndexerRequest) (*registry.RegisterIndexerResponse, error) {
	return m.resp, m.err
}

func (m *mockIndexerRegistry) VerifyRequest(_ context.Context, _ string) (*store.IndexerRecord, error) {
	return m.record, m.err
}

func (m *mockIndexerRegistry) Heartbeat(_ context.Context, _ string, _ registry.HeartbeatRequest) error {
	return m.heartbeat
}

func (m *mockIndexerRegistry) Deregister(_ context.Context, _ string) error {
	return m.err
}

type mockIndexerGetter struct {
	record *store.IndexerRecord
	err    error
}

func (m *mockIndexerGetter) GetByPeerID(_ context.Context, _ string) (*store.IndexerRecord, error) {
	return m.record, m.err
}

// --- helpers ---

// injectAPIKey pipes the request through RequireAPIKey middleware so the API key
// is stored in the context with the correct unexported key type.
func injectAPIKey(r *http.Request, key string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+key)
	var out *http.Request
	middleware.RequireAPIKey(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		out = req
	})).ServeHTTP(httptest.NewRecorder(), r)
	if out == nil {
		return r
	}
	return out
}

// injectVars sets gorilla/mux route variables on the request context.
func injectVars(r *http.Request, vars map[string]string) *http.Request {
	return mux.SetURLVars(r, vars)
}

// --- tests ---

func TestIndexerHandler_Register_BadBody(t *testing.T) {
	h := &IndexerHandler{reg: &mockIndexerRegistry{}, indexerSt: &mockIndexerGetter{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/indexers/register", bytes.NewBufferString("not-json"))
	h.Register(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIndexerHandler_Register_Success(t *testing.T) {
	h := &IndexerHandler{
		reg: &mockIndexerRegistry{
			resp: &registry.RegisterIndexerResponse{PeerID: "peer1"},
		},
		indexerSt: &mockIndexerGetter{},
	}
	body, _ := json.Marshal(registry.RegisterIndexerRequest{PeerID: "peer1"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/indexers/register", bytes.NewBuffer(body))
	h.Register(w, r)
	assert.Equal(t, http.StatusCreated, w.Code)
	var resp registry.RegisterIndexerResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "peer1", resp.PeerID)
}

func TestIndexerHandler_Register_Failure(t *testing.T) {
	h := &IndexerHandler{
		reg:       &mockIndexerRegistry{err: errors.New("bad sig")},
		indexerSt: &mockIndexerGetter{},
	}
	body, _ := json.Marshal(registry.RegisterIndexerRequest{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/indexers/register", bytes.NewBuffer(body))
	h.Register(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIndexerHandler_Heartbeat_Unauthorized(t *testing.T) {
	h := &IndexerHandler{
		reg:       &mockIndexerRegistry{err: errors.New("bad key")},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/indexers/peer1/heartbeat", bytes.NewBufferString("{}"))
	r = injectVars(injectAPIKey(r, "bad.key"), map[string]string{"id": "peer1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIndexerHandler_Heartbeat_WrongPeer(t *testing.T) {
	h := &IndexerHandler{
		reg: &mockIndexerRegistry{
			record: &store.IndexerRecord{PeerID: "other-peer"},
		},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/indexers/peer1/heartbeat", bytes.NewBufferString(`{"current_tip":100}`))
	r = injectVars(injectAPIKey(r, "other-peer.key.val"), map[string]string{"id": "peer1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIndexerHandler_Heartbeat_Success(t *testing.T) {
	h := &IndexerHandler{
		reg: &mockIndexerRegistry{
			record: &store.IndexerRecord{PeerID: "peer1"},
		},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/indexers/peer1/heartbeat", bytes.NewBufferString(`{"current_tip":100}`))
	r = injectVars(injectAPIKey(r, "peer1.key.val"), map[string]string{"id": "peer1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIndexerHandler_Get_NotFound(t *testing.T) {
	h := &IndexerHandler{
		reg:       &mockIndexerRegistry{},
		indexerSt: &mockIndexerGetter{record: nil},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/indexers/peer1", nil)
	r = injectVars(r, map[string]string{"id": "peer1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestIndexerHandler_Get_Success(t *testing.T) {
	h := &IndexerHandler{
		reg:       &mockIndexerRegistry{},
		indexerSt: &mockIndexerGetter{record: &store.IndexerRecord{PeerID: "peer1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/indexers/peer1", nil)
	r = injectVars(r, map[string]string{"id": "peer1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var rec store.IndexerRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rec))
	assert.Equal(t, "peer1", rec.PeerID)
}

func TestIndexerHandler_Deregister_Unauthorized(t *testing.T) {
	h := &IndexerHandler{
		reg:       &mockIndexerRegistry{err: errors.New("bad key")},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/indexers/peer1", nil)
	r = injectVars(injectAPIKey(r, "bad.key"), map[string]string{"id": "peer1"})
	h.Deregister(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIndexerHandler_Deregister_Success(t *testing.T) {
	h := &IndexerHandler{
		reg: &mockIndexerRegistry{
			record: &store.IndexerRecord{PeerID: "peer1"},
		},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/indexers/peer1", nil)
	r = injectVars(injectAPIKey(r, "peer1.key.val"), map[string]string{"id": "peer1"})
	h.Deregister(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIndexerHandler_Heartbeat_BadBody(t *testing.T) {
	h := &IndexerHandler{
		reg: &mockIndexerRegistry{
			record: &store.IndexerRecord{PeerID: "peer1"},
		},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/indexers/peer1/heartbeat", bytes.NewBufferString("not-json"))
	r = injectVars(injectAPIKey(r, "peer1.key.val"), map[string]string{"id": "peer1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIndexerHandler_Heartbeat_Error(t *testing.T) {
	h := &IndexerHandler{
		reg: &mockIndexerRegistry{
			record:    &store.IndexerRecord{PeerID: "peer1"},
			heartbeat: errors.New("store error"),
		},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/indexers/peer1/heartbeat", bytes.NewBufferString(`{"current_tip":1}`))
	r = injectVars(injectAPIKey(r, "peer1.key.val"), map[string]string{"id": "peer1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIndexerHandler_Get_StoreError(t *testing.T) {
	h := &IndexerHandler{
		reg:       &mockIndexerRegistry{},
		indexerSt: &mockIndexerGetter{err: errors.New("db error")},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/indexers/peer1", nil)
	r = injectVars(r, map[string]string{"id": "peer1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIndexerHandler_Deregister_WrongPeer(t *testing.T) {
	h := &IndexerHandler{
		reg: &mockIndexerRegistry{
			record: &store.IndexerRecord{PeerID: "other-peer"},
		},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/indexers/peer1", nil)
	r = injectVars(injectAPIKey(r, "other-peer.key.val"), map[string]string{"id": "peer1"})
	h.Deregister(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIndexerHandler_Deregister_Error(t *testing.T) {
	h := &IndexerHandler{
		reg: &mockIndexerRegistryWithDeregErr{
			record:   &store.IndexerRecord{PeerID: "peer1"},
			deregErr: errors.New("db error"),
		},
		indexerSt: &mockIndexerGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/indexers/peer1", nil)
	r = injectVars(injectAPIKey(r, "peer1.key.val"), map[string]string{"id": "peer1"})
	h.Deregister(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

type mockIndexerRegistryWithDeregErr struct {
	record   *store.IndexerRecord
	deregErr error
}

func (m *mockIndexerRegistryWithDeregErr) Register(_ context.Context, _ registry.RegisterIndexerRequest) (*registry.RegisterIndexerResponse, error) {
	return nil, nil
}
func (m *mockIndexerRegistryWithDeregErr) VerifyRequest(_ context.Context, _ string) (*store.IndexerRecord, error) {
	return m.record, nil
}
func (m *mockIndexerRegistryWithDeregErr) Heartbeat(_ context.Context, _ string, _ registry.HeartbeatRequest) error {
	return nil
}
func (m *mockIndexerRegistryWithDeregErr) Deregister(_ context.Context, _ string) error {
	return m.deregErr
}

func TestNewIndexerHandler(t *testing.T) {
	h := NewIndexerHandler(nil, nil)
	assert.NotNil(t, h)
}
