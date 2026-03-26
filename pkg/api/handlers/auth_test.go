package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/auth"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- mocks ---

type mockIndexerKeyStore struct {
	record    *store.IndexerRecord
	getErr    error
	updateErr error
	updated   string // captures the new hash
}

func (m *mockIndexerKeyStore) GetByPeerID(_ context.Context, _ string) (*store.IndexerRecord, error) {
	return m.record, m.getErr
}

func (m *mockIndexerKeyStore) UpdateAPIKeyHash(_ context.Context, _ string, hash string) error {
	m.updated = hash
	return m.updateErr
}

type mockHostKeyStore struct {
	record    *store.HostRecord
	getErr    error
	updateErr error
	updated   string
}

func (m *mockHostKeyStore) GetByPeerID(_ context.Context, _ string) (*store.HostRecord, error) {
	return m.record, m.getErr
}

func (m *mockHostKeyStore) UpdateAPIKeyHash(_ context.Context, _ string, hash string) error {
	m.updated = hash
	return m.updateErr
}

// buildAuthHandler creates an AuthHandler with a real Verifier and the provided mocks.
func buildAuthHandler(idxSt indexerKeyStore, hostSt hostKeyStore) *AuthHandler {
	log, _ := zap.NewDevelopment()
	v := auth.NewVerifier("test-secret")
	return &AuthHandler{verifier: v, indexerSt: idxSt, hostSt: hostSt, log: log.Sugar()}
}

func TestRotateKey_Unauthenticated(t *testing.T) {
	h := buildAuthHandler(
		&mockIndexerKeyStore{},
		&mockHostKeyStore{},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/rotate-key", nil)
	// No Bearer token — no API key in context.
	h.RotateKey(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRotateKey_MalformedKey(t *testing.T) {
	h := buildAuthHandler(
		&mockIndexerKeyStore{},
		&mockHostKeyStore{},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/rotate-key", nil)
	r = injectAPIKey(r, "badkey-no-dots")
	h.RotateKey(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRotateKey_PeerNotFound(t *testing.T) {
	h := buildAuthHandler(
		&mockIndexerKeyStore{record: nil, getErr: nil},
		&mockHostKeyStore{record: nil, getErr: errors.New("not found")},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/rotate-key", nil)
	r = injectAPIKey(r, "peer1.20260101T000000Z.abcdef")
	h.RotateKey(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRotateKey_IndexerSuccess(t *testing.T) {
	idxSt := &mockIndexerKeyStore{
		record: &store.IndexerRecord{DocID: "doc-123", PeerID: "peer1"},
	}
	h := buildAuthHandler(idxSt, &mockHostKeyStore{})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/rotate-key", nil)
	r = injectAPIKey(r, "peer1.20260101T000000Z.abcdef")
	h.RotateKey(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, idxSt.updated)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp["api_key"])
}

func TestRotateKey_HostFallback(t *testing.T) {
	// Indexer not found → falls back to host.
	hostSt := &mockHostKeyStore{
		record: &store.HostRecord{DocID: "hst-456", PeerID: "peer2"},
	}
	h := buildAuthHandler(
		&mockIndexerKeyStore{record: nil},
		hostSt,
	)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/rotate-key", nil)
	r = injectAPIKey(r, "peer2.20260101T000000Z.abcdef")
	h.RotateKey(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, hostSt.updated)
}

func TestRotateKey_UpdateIndexerError(t *testing.T) {
	h := buildAuthHandler(
		&mockIndexerKeyStore{
			record:    &store.IndexerRecord{DocID: "doc-1", PeerID: "peer1"},
			updateErr: errors.New("db write failed"),
		},
		&mockHostKeyStore{},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/rotate-key", nil)
	r = injectAPIKey(r, "peer1.20260101T000000Z.abcdef")
	h.RotateKey(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

type mockKeyRotator struct {
	key  string
	hash string
	err  error
}

func (m *mockKeyRotator) IssueAPIKey(_ string) (string, string, error) {
	return m.key, m.hash, m.err
}

func TestRotateKey_IssueKeyError(t *testing.T) {
	log, _ := zap.NewDevelopment()
	h := &AuthHandler{
		verifier:  &mockKeyRotator{err: errors.New("issue failed")},
		indexerSt: &mockIndexerKeyStore{},
		hostSt:    &mockHostKeyStore{},
		log:       log.Sugar(),
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/rotate-key", nil)
	r = injectAPIKey(r, "peer1.20260101T000000Z.abcdef")
	h.RotateKey(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRotateKey_UpdateHostError(t *testing.T) {
	h := buildAuthHandler(
		&mockIndexerKeyStore{record: nil},
		&mockHostKeyStore{
			record:    &store.HostRecord{DocID: "hst-1", PeerID: "peer2"},
			updateErr: errors.New("db write failed"),
		},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/rotate-key", nil)
	r = injectAPIKey(r, "peer2.20260101T000000Z.abcdef")
	h.RotateKey(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestNewAuthHandler(t *testing.T) {
	h := NewAuthHandler(nil, nil, nil, nil)
	assert.NotNil(t, h)
}
