package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/middleware"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	subpkg "github.com/shinzonetwork/shinzo-scheduler-service/pkg/subscription"
)

type subscriptionManager interface {
	Create(ctx context.Context, req subpkg.CreateRequest) (*store.SubscriptionRecord, error)
	ListByHost(ctx context.Context, hostID string) ([]store.SubscriptionRecord, error)
	Get(ctx context.Context, subID string) (*store.SubscriptionRecord, *store.IndexerRecord, error)
	Cancel(ctx context.Context, subID string) error
}

type hostVerifier interface {
	VerifyAPIKey(ctx context.Context, apiKey string) (*store.HostRecord, error)
}

// SubscriptionHandler serves /v1/subscriptions routes.
type SubscriptionHandler struct {
	mgr     subscriptionManager
	hostReg hostVerifier
}

func NewSubscriptionHandler(mgr *subpkg.Manager, hostReg *registry.HostRegistry) *SubscriptionHandler {
	return &SubscriptionHandler{mgr: mgr, hostReg: hostReg}
}

// Create handles POST /v1/subscriptions.
func (h *SubscriptionHandler) Create(w http.ResponseWriter, r *http.Request) {
	host, err := h.authHost(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req subpkg.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Enforce host_id matches the authenticated host.
	req.HostID = host.PeerID

	sub, err := h.mgr.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sub)
}

// Get handles GET /v1/subscriptions/{id}.
func (h *SubscriptionHandler) Get(w http.ResponseWriter, r *http.Request) {
	subID := mux.Vars(r)["id"]

	host, err := h.authHost(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	sub, indexer, err := h.mgr.Get(r.Context(), subID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if sub.HostID != host.PeerID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	resp := map[string]any{"subscription": sub}
	if indexer != nil {
		resp["indexer_multiaddr"] = indexer.Multiaddr
		resp["indexer_http_url"] = indexer.HTTPUrl
	}
	writeJSON(w, http.StatusOK, resp)
}

// List handles GET /v1/subscriptions?host_id=...
func (h *SubscriptionHandler) List(w http.ResponseWriter, r *http.Request) {
	host, err := h.authHost(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// hosts can only list their own subscriptions
	hostID := r.URL.Query().Get("host_id")
	if hostID != "" && hostID != host.PeerID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	subs, err := h.mgr.ListByHost(r.Context(), host.PeerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

// Cancel handles DELETE /v1/subscriptions/{id}.
func (h *SubscriptionHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	subID := mux.Vars(r)["id"]

	host, err := h.authHost(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	sub, _, err := h.mgr.Get(r.Context(), subID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if sub.HostID != host.PeerID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.mgr.Cancel(r.Context(), subID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *SubscriptionHandler) authHost(r *http.Request) (*store.HostRecord, error) {
	apiKey := middleware.APIKeyFromContext(r.Context())
	return h.hostReg.VerifyAPIKey(r.Context(), apiKey)
}
