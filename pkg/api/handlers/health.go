package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/dto"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
)

type nodeCounter interface {
	Count(ctx context.Context, status string) (int, error)
}

type subStatusLister interface {
	ListByStatus(ctx context.Context, status string) ([]store.SubscriptionRecord, error)
}

type HealthHandler struct {
	indexerSt nodeCounter
	hostSt    nodeCounter
	subSt     subStatusLister
}

func NewHealthHandler(indexerSt *store.IndexerStore, hostSt *store.HostStore, subSt *store.SubscriptionStore) *HealthHandler {
	return &HealthHandler{indexerSt: indexerSt, hostSt: hostSt, subSt: subSt}
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HealthHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ai, err := h.indexerSt.Count(ctx, store.StatusActive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ah, err := h.hostSt.Count(ctx, store.StatusActive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	subs, err := h.countSubs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, dto.StatsResponse{
		ActiveIndexers: ai,
		ActiveHosts:    ah,
		Subscriptions:  subs,
	})
}

func (h *HealthHandler) countSubs(ctx context.Context) (int, error) {
	active, err := h.subSt.ListByStatus(ctx, store.StatusActive)
	if err != nil {
		return 0, err
	}
	pending, err := h.subSt.ListByStatus(ctx, store.StatusPending)
	if err != nil {
		return 0, err
	}
	return len(active) + len(pending), nil
}

// --- shared helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "writeJSON encode error: %v\n", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, dto.ErrorResponse{Error: msg})
}
