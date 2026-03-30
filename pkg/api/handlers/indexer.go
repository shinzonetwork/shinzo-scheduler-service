package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/middleware"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
)

type indexerRegistry interface {
	Register(ctx context.Context, req registry.RegisterIndexerRequest) (*registry.RegisterIndexerResponse, error)
	VerifyRequest(ctx context.Context, token string) (*store.IndexerRecord, error)
	Heartbeat(ctx context.Context, peerID string, req registry.HeartbeatRequest) error
	Deregister(ctx context.Context, peerID string) error
}

type indexerGetter interface {
	GetByPeerID(ctx context.Context, peerID string) (*store.IndexerRecord, error)
}

type IndexerHandler struct {
	reg       indexerRegistry
	indexerSt indexerGetter
}

func NewIndexerHandler(reg *registry.IndexerRegistry, indexerSt *store.IndexerStore) *IndexerHandler {
	return &IndexerHandler{reg: reg, indexerSt: indexerSt}
}

func (h *IndexerHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registry.RegisterIndexerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.reg.Register(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *IndexerHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	peerID := mux.Vars(r)["id"]

	token := middleware.APIKeyFromContext(r.Context())
	rec, err := h.reg.VerifyRequest(r.Context(), token)
	if err != nil || rec.PeerID != peerID {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req registry.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.reg.Heartbeat(r.Context(), peerID, req); err != nil {
		writeError(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *IndexerHandler) Get(w http.ResponseWriter, r *http.Request) {
	peerID := mux.Vars(r)["id"]
	rec, err := h.indexerSt.GetByPeerID(r.Context(), peerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rec == nil {
		writeError(w, http.StatusNotFound, "indexer not found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *IndexerHandler) Deregister(w http.ResponseWriter, r *http.Request) {
	peerID := mux.Vars(r)["id"]

	token := middleware.APIKeyFromContext(r.Context())
	rec, err := h.reg.VerifyRequest(r.Context(), token)
	if err != nil || rec.PeerID != peerID {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.reg.Deregister(r.Context(), peerID); err != nil {
		writeError(w, http.StatusInternalServerError, "deregister failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deregistered"})
}
