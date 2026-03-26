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
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/accounting"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAccountingManager struct {
	claim       *store.DeliveryClaimRecord
	attest      *store.AttestationRecord
	ledger      *store.SessionLedgerRecord
	comparisons []store.ComparisonResultRecord
	err         error
}

func (m *mockAccountingManager) SubmitDeliveryClaim(_ context.Context, _ accounting.SubmitClaimRequest) (*store.DeliveryClaimRecord, error) {
	return m.claim, m.err
}

func (m *mockAccountingManager) SubmitAttestation(_ context.Context, _ accounting.SubmitAttestationRequest) (*store.AttestationRecord, error) {
	return m.attest, m.err
}

func (m *mockAccountingManager) GetSessionLedger(_ context.Context, _ string) (*store.SessionLedgerRecord, error) {
	return m.ledger, m.err
}

func (m *mockAccountingManager) GetComparisons(_ context.Context, _ string) ([]store.ComparisonResultRecord, error) {
	return m.comparisons, m.err
}

func TestAccountingHandler_SubmitClaim_Success(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{
		claim: &store.DeliveryClaimRecord{DocID: "c-1", ClaimID: "cid-1"},
	})
	body, _ := json.Marshal(accounting.SubmitClaimRequest{SessionID: "s-1", IndexerID: "i-1", BlockNumber: 100})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/claims", bytes.NewReader(body))
	h.SubmitClaim(w, r)

	assert.Equal(t, http.StatusCreated, w.Code)
	var rec store.DeliveryClaimRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rec))
	assert.Equal(t, "cid-1", rec.ClaimID)
}

func TestAccountingHandler_SubmitClaim_BadBody(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/claims", bytes.NewReader([]byte("invalid json")))
	h.SubmitClaim(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAccountingHandler_SubmitClaim_ManagerError(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{err: errors.New("duplicate claim")})
	body, _ := json.Marshal(accounting.SubmitClaimRequest{SessionID: "s-1"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/claims", bytes.NewReader(body))
	h.SubmitClaim(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAccountingHandler_SubmitAttestation_Success(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{
		attest: &store.AttestationRecord{DocID: "a-1", AttestationID: "aid-1"},
	})
	body, _ := json.Marshal(accounting.SubmitAttestationRequest{SessionID: "s-1", HostID: "h-1", BlockNumber: 100})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/attestations", bytes.NewReader(body))
	h.SubmitAttestation(w, r)

	assert.Equal(t, http.StatusCreated, w.Code)
	var rec store.AttestationRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rec))
	assert.Equal(t, "aid-1", rec.AttestationID)
}

func TestAccountingHandler_SubmitAttestation_BadBody(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/attestations", bytes.NewReader([]byte("{bad")))
	h.SubmitAttestation(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAccountingHandler_SubmitAttestation_ManagerError(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{err: errors.New("duplicate")})
	body, _ := json.Marshal(accounting.SubmitAttestationRequest{SessionID: "s-1"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/attestations", bytes.NewReader(body))
	h.SubmitAttestation(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAccountingHandler_SessionLedger_Success(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{
		ledger: &store.SessionLedgerRecord{DocID: "l-1", SessionID: "s-1", CreditRemaining: 50},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/s-1/ledger", nil)
	r = mux.SetURLVars(r, map[string]string{"id": "s-1"})
	h.SessionLedger(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var rec store.SessionLedgerRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rec))
	assert.Equal(t, 50.0, rec.CreditRemaining)
}

func TestAccountingHandler_SessionLedger_MissingID(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions//ledger", nil)
	r = mux.SetURLVars(r, map[string]string{"id": ""})
	h.SessionLedger(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAccountingHandler_SessionLedger_NotFound(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{ledger: nil})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/s-1/ledger", nil)
	r = mux.SetURLVars(r, map[string]string{"id": "s-1"})
	h.SessionLedger(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAccountingHandler_SessionLedger_Error(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{err: errors.New("db error")})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/s-1/ledger", nil)
	r = mux.SetURLVars(r, map[string]string{"id": "s-1"})
	h.SessionLedger(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAccountingHandler_Comparisons_Success(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{
		comparisons: []store.ComparisonResultRecord{
			{DocID: "cr-1", SessionID: "s-1", Outcome: "match"},
		},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/s-1/comparisons", nil)
	r = mux.SetURLVars(r, map[string]string{"id": "s-1"})
	h.Comparisons(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var recs []store.ComparisonResultRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recs))
	assert.Len(t, recs, 1)
}

func TestAccountingHandler_Comparisons_MissingID(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions//comparisons", nil)
	r = mux.SetURLVars(r, map[string]string{"id": ""})
	h.Comparisons(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAccountingHandler_Comparisons_Error(t *testing.T) {
	h := NewAccountingHandler(&mockAccountingManager{err: errors.New("db error")})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/s-1/comparisons", nil)
	r = mux.SetURLVars(r, map[string]string{"id": "s-1"})
	h.Comparisons(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNewAccountingHandler(t *testing.T) {
	h := NewAccountingHandler(nil)
	assert.NotNil(t, h)
}
