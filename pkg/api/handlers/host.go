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

type hostRegistry interface {
	Register(ctx context.Context, req registry.RegisterHostRequest) (*registry.RegisterHostResponse, error)
	VerifyAPIKey(ctx context.Context, apiKey string) (*store.HostRecord, error)
	Heartbeat(ctx context.Context, peerID string) error
	Deregister(ctx context.Context, peerID string) error
}

type hostGetter interface {
	GetByPeerID(ctx context.Context, peerID string) (*store.HostRecord, error)
}

type HostHandler struct {
	reg    hostRegistry
	hostSt hostGetter
}

func NewHostHandler(reg *registry.HostRegistry, hostSt *store.HostStore) *HostHandler {
	return &HostHandler{reg: reg, hostSt: hostSt}
}

func (h *HostHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registry.RegisterHostRequest
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

func (h *HostHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	peerID := mux.Vars(r)["id"]

	apiKey := middleware.APIKeyFromContext(r.Context())
	rec, err := h.reg.VerifyAPIKey(r.Context(), apiKey)
	if err != nil || rec.PeerID != peerID {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.reg.Heartbeat(r.Context(), peerID); err != nil {
		writeError(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HostHandler) Get(w http.ResponseWriter, r *http.Request) {
	peerID := mux.Vars(r)["id"]

	apiKey := middleware.APIKeyFromContext(r.Context())
	rec, err := h.reg.VerifyAPIKey(r.Context(), apiKey)
	if err != nil || rec.PeerID != peerID {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	hostRec, err := h.hostSt.GetByPeerID(r.Context(), peerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if hostRec == nil {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	hostRec.APIKeyHash = ""
	writeJSON(w, http.StatusOK, hostRec)
}

func (h *HostHandler) Deregister(w http.ResponseWriter, r *http.Request) {
	peerID := mux.Vars(r)["id"]

	apiKey := middleware.APIKeyFromContext(r.Context())
	rec, err := h.reg.VerifyAPIKey(r.Context(), apiKey)
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
