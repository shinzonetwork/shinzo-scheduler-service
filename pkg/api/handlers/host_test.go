package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mocks ---

type mockHostRegistry struct {
	record *store.HostRecord
	resp   *registry.RegisterHostResponse
	err    error
}

func (m *mockHostRegistry) Register(_ context.Context, _ registry.RegisterHostRequest) (*registry.RegisterHostResponse, error) {
	return m.resp, m.err
}

func (m *mockHostRegistry) VerifyRequest(_ context.Context, _ string) (*store.HostRecord, error) {
	return m.record, m.err
}

func (m *mockHostRegistry) Heartbeat(_ context.Context, _ string) error {
	return m.err
}

func (m *mockHostRegistry) Deregister(_ context.Context, _ string) error {
	return m.err
}

type mockHostGetter struct {
	record *store.HostRecord
	err    error
}

func (m *mockHostGetter) GetByPeerID(_ context.Context, _ string) (*store.HostRecord, error) {
	return m.record, m.err
}

// --- tests ---

func TestHostHandler_Register_BadBody(t *testing.T) {
	h := &HostHandler{reg: &mockHostRegistry{}, hostSt: &mockHostGetter{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/hosts/register", bytes.NewBufferString("not-json"))
	h.Register(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHostHandler_Register_Success(t *testing.T) {
	h := &HostHandler{
		reg: &mockHostRegistry{
			resp: &registry.RegisterHostResponse{PeerID: "host1"},
		},
		hostSt: &mockHostGetter{},
	}
	body, _ := json.Marshal(registry.RegisterHostRequest{PeerID: "host1"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/hosts/register", bytes.NewBuffer(body))
	h.Register(w, r)
	assert.Equal(t, http.StatusCreated, w.Code)
	var resp registry.RegisterHostResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "host1", resp.PeerID)
}

func TestHostHandler_Register_Failure(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{err: errors.New("bad sig")},
		hostSt: &mockHostGetter{},
	}
	body, _ := json.Marshal(registry.RegisterHostRequest{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/hosts/register", bytes.NewBuffer(body))
	h.Register(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHostHandler_Heartbeat_Unauthorized(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{err: errors.New("bad key")},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/hosts/host1/heartbeat", nil)
	r = injectVars(injectAPIKey(r, "bad.key"), map[string]string{"id": "host1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHostHandler_Heartbeat_Success(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{record: &store.HostRecord{PeerID: "host1"}},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/hosts/host1/heartbeat", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "host1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHostHandler_Get_Unauthorized(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{err: errors.New("bad key")},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/hosts/host1", nil)
	r = injectVars(injectAPIKey(r, "bad.key"), map[string]string{"id": "host1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHostHandler_Get_NotFound(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{record: &store.HostRecord{PeerID: "host1"}},
		hostSt: &mockHostGetter{record: nil},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/hosts/host1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "host1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHostHandler_Get_Success(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{record: &store.HostRecord{PeerID: "host1"}},
		hostSt: &mockHostGetter{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/hosts/host1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "host1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var rec store.HostRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rec))
	assert.Equal(t, "host1", rec.PeerID)
}

func TestHostHandler_Deregister_Success(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{record: &store.HostRecord{PeerID: "host1"}},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/hosts/host1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "host1"})
	h.Deregister(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHostHandler_Heartbeat_WrongPeer(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{record: &store.HostRecord{PeerID: "other-host"}},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/hosts/host1/heartbeat", nil)
	r = injectVars(injectAPIKey(r, "other-host.key.val"), map[string]string{"id": "host1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHostHandler_Heartbeat_Error(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistryWithHeartbeatErr{record: &store.HostRecord{PeerID: "host1"}, heartbeatErr: errors.New("store error")},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/hosts/host1/heartbeat", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "host1"})
	h.Heartbeat(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHostHandler_Get_StoreError(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{record: &store.HostRecord{PeerID: "host1"}},
		hostSt: &mockHostGetter{err: errors.New("db error")},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/hosts/host1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "host1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHostHandler_Deregister_Unauthorized(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{err: errors.New("bad key")},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/hosts/host1", nil)
	r = injectVars(injectAPIKey(r, "bad.key"), map[string]string{"id": "host1"})
	h.Deregister(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHostHandler_Deregister_WrongPeer(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistry{record: &store.HostRecord{PeerID: "other-host"}},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/hosts/host1", nil)
	r = injectVars(injectAPIKey(r, "other-host.key.val"), map[string]string{"id": "host1"})
	h.Deregister(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHostHandler_Deregister_Error(t *testing.T) {
	h := &HostHandler{
		reg:    &mockHostRegistryWithDeregErr{record: &store.HostRecord{PeerID: "host1"}, deregErr: errors.New("db error")},
		hostSt: &mockHostGetter{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/hosts/host1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "host1"})
	h.Deregister(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

type mockHostRegistryWithHeartbeatErr struct {
	record       *store.HostRecord
	heartbeatErr error
}

func (m *mockHostRegistryWithHeartbeatErr) Register(_ context.Context, _ registry.RegisterHostRequest) (*registry.RegisterHostResponse, error) {
	return nil, nil
}
func (m *mockHostRegistryWithHeartbeatErr) VerifyRequest(_ context.Context, _ string) (*store.HostRecord, error) {
	return m.record, nil
}
func (m *mockHostRegistryWithHeartbeatErr) Heartbeat(_ context.Context, _ string) error {
	return m.heartbeatErr
}
func (m *mockHostRegistryWithHeartbeatErr) Deregister(_ context.Context, _ string) error {
	return nil
}

type mockHostRegistryWithDeregErr struct {
	record   *store.HostRecord
	deregErr error
}

func (m *mockHostRegistryWithDeregErr) Register(_ context.Context, _ registry.RegisterHostRequest) (*registry.RegisterHostResponse, error) {
	return nil, nil
}
func (m *mockHostRegistryWithDeregErr) VerifyRequest(_ context.Context, _ string) (*store.HostRecord, error) {
	return m.record, nil
}
func (m *mockHostRegistryWithDeregErr) Heartbeat(_ context.Context, _ string) error {
	return nil
}
func (m *mockHostRegistryWithDeregErr) Deregister(_ context.Context, _ string) error {
	return m.deregErr
}

func TestNewHostHandler(t *testing.T) {
	h := NewHostHandler(nil, nil)
	assert.NotNil(t, h)
}
