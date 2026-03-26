package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/accounting"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
)

type accountingManager interface {
	SubmitDeliveryClaim(ctx context.Context, req accounting.SubmitClaimRequest) (*store.DeliveryClaimRecord, error)
	SubmitAttestation(ctx context.Context, req accounting.SubmitAttestationRequest) (*store.AttestationRecord, error)
	GetSessionLedger(ctx context.Context, sessionID string) (*store.SessionLedgerRecord, error)
	GetComparisons(ctx context.Context, sessionID string) ([]store.ComparisonResultRecord, error)
}

type AccountingHandler struct {
	mgr accountingManager
}

func NewAccountingHandler(mgr accountingManager) *AccountingHandler {
	return &AccountingHandler{mgr: mgr}
}

func (h *AccountingHandler) SubmitClaim(w http.ResponseWriter, r *http.Request) {
	var req accounting.SubmitClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	claim, err := h.mgr.SubmitDeliveryClaim(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, claim)
}

func (h *AccountingHandler) SubmitAttestation(w http.ResponseWriter, r *http.Request) {
	var req accounting.SubmitAttestationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	attest, err := h.mgr.SubmitAttestation(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, attest)
}

func (h *AccountingHandler) SessionLedger(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	ledger, err := h.mgr.GetSessionLedger(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if ledger == nil {
		writeError(w, http.StatusNotFound, "session ledger not found")
		return
	}
	writeJSON(w, http.StatusOK, ledger)
}

func (h *AccountingHandler) Comparisons(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	results, err := h.mgr.GetComparisons(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, results)
}
