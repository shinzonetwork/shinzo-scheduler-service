package handlers

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
)

type escrowQuerier interface {
	GetBySession(ctx context.Context, sessionID string) (*store.EscrowAccountRecord, error)
}

type settlementQuerier interface {
	ListBySession(ctx context.Context, sessionID string) ([]store.SettlementRecord, error)
}

type verdictQuerier interface {
	ListBySession(ctx context.Context, sessionID string) ([]store.VerdictRecord, error)
}

// SettlementHandler serves /v1/escrow/*, /v1/settlements/*, and /v1/verdicts/* routes.
type SettlementHandler struct {
	escrowSt     escrowQuerier
	settlementSt settlementQuerier
	verdictSt    verdictQuerier
}

func NewSettlementHandler(escrowSt escrowQuerier, settlementSt settlementQuerier, verdictSt verdictQuerier) *SettlementHandler {
	return &SettlementHandler{escrowSt: escrowSt, settlementSt: settlementSt, verdictSt: verdictSt}
}

// Escrow handles GET /v1/escrow/{session_id}.
func (h *SettlementHandler) Escrow(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	rec, err := h.escrowSt.GetBySession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rec == nil {
		writeError(w, http.StatusNotFound, "escrow account not found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// Settlements handles GET /v1/settlements/{session_id}.
func (h *SettlementHandler) Settlements(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	recs, err := h.settlementSt.ListBySession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

// Verdicts handles GET /v1/verdicts/{session_id}.
func (h *SettlementHandler) Verdicts(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	recs, err := h.verdictSt.ListBySession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, recs)
}
