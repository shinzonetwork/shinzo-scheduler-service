package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	subpkg "github.com/shinzonetwork/shinzo-scheduler-service/pkg/subscription"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mocks ---

type mockSubscriptionManager struct {
	sub     *store.SubscriptionRecord
	indexer *store.IndexerRecord
	subs    []store.SubscriptionRecord
	err     error
}

func (m *mockSubscriptionManager) Create(_ context.Context, _ subpkg.CreateRequest) (*store.SubscriptionRecord, error) {
	return m.sub, m.err
}

func (m *mockSubscriptionManager) ListByHost(_ context.Context, _ string) ([]store.SubscriptionRecord, error) {
	return m.subs, m.err
}

func (m *mockSubscriptionManager) Get(_ context.Context, _ string) (*store.SubscriptionRecord, *store.IndexerRecord, error) {
	return m.sub, m.indexer, m.err
}

func (m *mockSubscriptionManager) Cancel(_ context.Context, _ string) error {
	return m.err
}

type mockHostVerifier struct {
	record *store.HostRecord
	err    error
}

func (m *mockHostVerifier) VerifyAPIKey(_ context.Context, _ string) (*store.HostRecord, error) {
	return m.record, m.err
}

// --- tests ---

func TestSubscriptionHandler_Create_Unauthorized(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{},
		hostReg: &mockHostVerifier{err: errors.New("bad key")},
	}
	body, _ := json.Marshal(subpkg.CreateRequest{IndexerID: "idx1", SubType: store.SubTypeTip})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/subscriptions", bytes.NewBuffer(body))
	r = injectAPIKey(r, "bad.key")
	h.Create(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSubscriptionHandler_Create_BadBody(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/subscriptions", bytes.NewBufferString("bad"))
	r = injectAPIKey(r, "host1.key.val")
	h.Create(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSubscriptionHandler_Create_Success(t *testing.T) {
	sub := &store.SubscriptionRecord{
		SubscriptionID: "sub-1", HostID: "host1", Status: store.StatusPending,
	}
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{sub: sub},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	body, _ := json.Marshal(subpkg.CreateRequest{IndexerID: "idx1", SubType: store.SubTypeTip})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/subscriptions", bytes.NewBuffer(body))
	r = injectAPIKey(r, "host1.key.val")
	h.Create(w, r)
	assert.Equal(t, http.StatusCreated, w.Code)
	var resp store.SubscriptionRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "sub-1", resp.SubscriptionID)
}

func TestSubscriptionHandler_Get_NotFound(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{err: errors.New("not found")},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "sub-1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSubscriptionHandler_Get_Forbidden(t *testing.T) {
	h := &SubscriptionHandler{
		mgr: &mockSubscriptionManager{sub: &store.SubscriptionRecord{
			SubscriptionID: "sub-1", HostID: "other-host",
		}},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "sub-1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSubscriptionHandler_Get_Success(t *testing.T) {
	h := &SubscriptionHandler{
		mgr: &mockSubscriptionManager{sub: &store.SubscriptionRecord{
			SubscriptionID: "sub-1", HostID: "host1", Status: store.StatusActive,
		}},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "sub-1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSubscriptionHandler_List_Success(t *testing.T) {
	h := &SubscriptionHandler{
		mgr: &mockSubscriptionManager{subs: []store.SubscriptionRecord{
			{SubscriptionID: "s1", HostID: "host1"},
			{SubscriptionID: "s2", HostID: "host1"},
		}},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions", nil)
	r = injectAPIKey(r, "host1.key.val")
	h.List(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var subs []store.SubscriptionRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &subs))
	assert.Len(t, subs, 2)
}

func TestSubscriptionHandler_Cancel_Success(t *testing.T) {
	h := &SubscriptionHandler{
		mgr: &mockSubscriptionManager{sub: &store.SubscriptionRecord{
			SubscriptionID: "sub-1", HostID: "host1", Status: store.StatusActive,
		}},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "sub-1"})
	h.Cancel(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSubscriptionHandler_Create_ManagerError(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{err: errors.New("indexer unavailable")},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	body, _ := json.Marshal(subpkg.CreateRequest{IndexerID: "idx1", SubType: store.SubTypeTip})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/subscriptions", bytes.NewBuffer(body))
	r = injectAPIKey(r, "host1.key.val")
	h.Create(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSubscriptionHandler_Get_WithActiveIndexer(t *testing.T) {
	h := &SubscriptionHandler{
		mgr: &mockSubscriptionManager{
			sub:     &store.SubscriptionRecord{SubscriptionID: "sub-1", HostID: "host1", Status: store.StatusActive},
			indexer: &store.IndexerRecord{PeerID: "idx1", Multiaddr: "/ip4/1.2.3.4/tcp/4001"},
		},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "sub-1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "/ip4/1.2.3.4/tcp/4001", resp["indexer_multiaddr"])
}

func TestSubscriptionHandler_List_HostIDMismatch(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions?host_id=other-host", nil)
	r = injectAPIKey(r, "host1.key.val")
	h.List(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSubscriptionHandler_List_Error(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{err: errors.New("db error")},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions", nil)
	r = injectAPIKey(r, "host1.key.val")
	h.List(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSubscriptionHandler_Cancel_Forbidden(t *testing.T) {
	h := &SubscriptionHandler{
		mgr: &mockSubscriptionManager{sub: &store.SubscriptionRecord{
			SubscriptionID: "sub-1", HostID: "other-host", Status: store.StatusActive,
		}},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "sub-1"})
	h.Cancel(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSubscriptionHandler_Cancel_Error(t *testing.T) {
	// Get succeeds (returns the sub), but Cancel returns error.
	mgr := &mockSubscriptionManagerWithCancelErr{
		sub:       &store.SubscriptionRecord{SubscriptionID: "sub-1", HostID: "host1", Status: store.StatusActive},
		cancelErr: errors.New("already cancelled"),
	}
	h := &SubscriptionHandler{
		mgr:     mgr,
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "sub-1"})
	h.Cancel(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

type mockSubscriptionManagerWithCancelErr struct {
	sub       *store.SubscriptionRecord
	cancelErr error
}

func (m *mockSubscriptionManagerWithCancelErr) Create(_ context.Context, _ subpkg.CreateRequest) (*store.SubscriptionRecord, error) {
	return m.sub, nil
}
func (m *mockSubscriptionManagerWithCancelErr) ListByHost(_ context.Context, _ string) ([]store.SubscriptionRecord, error) {
	return nil, nil
}
func (m *mockSubscriptionManagerWithCancelErr) Get(_ context.Context, _ string) (*store.SubscriptionRecord, *store.IndexerRecord, error) {
	return m.sub, nil, nil
}
func (m *mockSubscriptionManagerWithCancelErr) Cancel(_ context.Context, _ string) error {
	return m.cancelErr
}

func TestSubscriptionHandler_Get_Unauthorized(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{},
		hostReg: &mockHostVerifier{err: errors.New("bad key")},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "bad.key"), map[string]string{"id": "sub-1"})
	h.Get(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSubscriptionHandler_List_Unauthorized(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{},
		hostReg: &mockHostVerifier{err: errors.New("bad key")},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscriptions", nil)
	r = injectAPIKey(r, "bad.key")
	h.List(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSubscriptionHandler_Cancel_Unauthorized(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{},
		hostReg: &mockHostVerifier{err: errors.New("bad key")},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "bad.key"), map[string]string{"id": "sub-1"})
	h.Cancel(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSubscriptionHandler_Cancel_GetError(t *testing.T) {
	h := &SubscriptionHandler{
		mgr:     &mockSubscriptionManager{err: errors.New("not found")},
		hostReg: &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/sub-1", nil)
	r = injectVars(injectAPIKey(r, "host1.key.val"), map[string]string{"id": "sub-1"})
	h.Cancel(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestNewSubscriptionHandler(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil)
	assert.NotNil(t, h)
}
