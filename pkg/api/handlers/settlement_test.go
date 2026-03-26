package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockEscrowQuerier struct {
	rec *store.EscrowAccountRecord
	err error
}

func (m *mockEscrowQuerier) GetBySession(_ context.Context, _ string) (*store.EscrowAccountRecord, error) {
	return m.rec, m.err
}

type mockSettlementQuerier struct {
	recs []store.SettlementRecord
	err  error
}

func (m *mockSettlementQuerier) ListBySession(_ context.Context, _ string) ([]store.SettlementRecord, error) {
	return m.recs, m.err
}

type mockVerdictQuerier struct {
	recs []store.VerdictRecord
	err  error
}

func (m *mockVerdictQuerier) ListBySession(_ context.Context, _ string) ([]store.VerdictRecord, error) {
	return m.recs, m.err
}

func TestSettlementHandler_Escrow_Success(t *testing.T) {
	h := NewSettlementHandler(
		&mockEscrowQuerier{rec: &store.EscrowAccountRecord{DocID: "e-1", EscrowID: "eid-1", CurrentBalance: 800}},
		&mockSettlementQuerier{},
		&mockVerdictQuerier{},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/escrow/s-1", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": "s-1"})
	h.Escrow(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var rec store.EscrowAccountRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rec))
	assert.Equal(t, 800.0, rec.CurrentBalance)
}

func TestSettlementHandler_Escrow_MissingSessionID(t *testing.T) {
	h := NewSettlementHandler(&mockEscrowQuerier{}, &mockSettlementQuerier{}, &mockVerdictQuerier{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/escrow/", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": ""})
	h.Escrow(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSettlementHandler_Escrow_NotFound(t *testing.T) {
	h := NewSettlementHandler(&mockEscrowQuerier{rec: nil}, &mockSettlementQuerier{}, &mockVerdictQuerier{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/escrow/s-1", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": "s-1"})
	h.Escrow(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSettlementHandler_Escrow_Error(t *testing.T) {
	h := NewSettlementHandler(&mockEscrowQuerier{err: errors.New("db error")}, &mockSettlementQuerier{}, &mockVerdictQuerier{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/escrow/s-1", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": "s-1"})
	h.Escrow(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSettlementHandler_Settlements_Success(t *testing.T) {
	h := NewSettlementHandler(
		&mockEscrowQuerier{},
		&mockSettlementQuerier{recs: []store.SettlementRecord{
			{DocID: "st-1", SettlementID: "sid-1", SessionID: "s-1"},
		}},
		&mockVerdictQuerier{},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/settlements/s-1", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": "s-1"})
	h.Settlements(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var recs []store.SettlementRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recs))
	assert.Len(t, recs, 1)
}

func TestSettlementHandler_Settlements_MissingSessionID(t *testing.T) {
	h := NewSettlementHandler(&mockEscrowQuerier{}, &mockSettlementQuerier{}, &mockVerdictQuerier{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/settlements/", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": ""})
	h.Settlements(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSettlementHandler_Settlements_Error(t *testing.T) {
	h := NewSettlementHandler(&mockEscrowQuerier{}, &mockSettlementQuerier{err: errors.New("db error")}, &mockVerdictQuerier{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/settlements/s-1", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": "s-1"})
	h.Settlements(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSettlementHandler_Verdicts_Success(t *testing.T) {
	h := NewSettlementHandler(
		&mockEscrowQuerier{},
		&mockSettlementQuerier{},
		&mockVerdictQuerier{recs: []store.VerdictRecord{
			{DocID: "v-1", VerdictID: "vid-1", SessionID: "s-1", Outcome: "honest"},
		}},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/verdicts/s-1", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": "s-1"})
	h.Verdicts(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var recs []store.VerdictRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recs))
	assert.Len(t, recs, 1)
	assert.Equal(t, "honest", recs[0].Outcome)
}

func TestSettlementHandler_Verdicts_MissingSessionID(t *testing.T) {
	h := NewSettlementHandler(&mockEscrowQuerier{}, &mockSettlementQuerier{}, &mockVerdictQuerier{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/verdicts/", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": ""})
	h.Verdicts(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSettlementHandler_Verdicts_Error(t *testing.T) {
	h := NewSettlementHandler(&mockEscrowQuerier{}, &mockSettlementQuerier{}, &mockVerdictQuerier{err: errors.New("db error")})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/verdicts/s-1", nil)
	r = mux.SetURLVars(r, map[string]string{"session_id": "s-1"})
	h.Verdicts(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNewSettlementHandler(t *testing.T) {
	h := NewSettlementHandler(nil, nil, nil)
	assert.NotNil(t, h)
}
