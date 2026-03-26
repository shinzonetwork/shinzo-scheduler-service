package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/middleware"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/auth"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

type keyRotator interface {
	IssueAPIKey(peerID string) (plaintext, hash string, err error)
}

type indexerKeyStore interface {
	GetByPeerID(ctx context.Context, peerID string) (*store.IndexerRecord, error)
	UpdateAPIKeyHash(ctx context.Context, docID, hash string) error
}

type hostKeyStore interface {
	GetByPeerID(ctx context.Context, peerID string) (*store.HostRecord, error)
	UpdateAPIKeyHash(ctx context.Context, docID, hash string) error
}

type AuthHandler struct {
	verifier  keyRotator
	indexerSt indexerKeyStore
	hostSt    hostKeyStore
	log       *zap.SugaredLogger
}

func NewAuthHandler(v *auth.Verifier, indexerSt *store.IndexerStore, hostSt *store.HostStore, log *zap.SugaredLogger) *AuthHandler {
	return &AuthHandler{verifier: v, indexerSt: indexerSt, hostSt: hostSt, log: log}
}

func (h *AuthHandler) RotateKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	apiKey := middleware.APIKeyFromContext(ctx)

	peerID, err := auth.ExtractPeerID(apiKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "malformed API key")
		return
	}

	plainKey, keyHash, err := h.verifier.IssueAPIKey(peerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue API key")
		return
	}

	// Try indexer first, then host.
	role, updateErr := h.resolveAndUpdate(ctx, peerID, keyHash)
	if updateErr != nil {
		writeError(w, http.StatusNotFound, "peer not found")
		return
	}

	h.log.Infow("audit: API key rotated", "peer_id", peerID, "role", role)
	writeJSON(w, http.StatusOK, map[string]string{"api_key": plainKey})
}

func (h *AuthHandler) resolveAndUpdate(ctx context.Context, peerID, keyHash string) (role string, err error) {
	idx, idxErr := h.indexerSt.GetByPeerID(ctx, peerID)
	if idxErr == nil && idx != nil {
		return "indexer", h.indexerSt.UpdateAPIKeyHash(ctx, idx.DocID, keyHash)
	}
	host, hostErr := h.hostSt.GetByPeerID(ctx, peerID)
	if hostErr == nil && host != nil {
		return "host", h.hostSt.UpdateAPIKeyHash(ctx, host.DocID, keyHash)
	}
	if idxErr != nil {
		return "", idxErr
	}
	if hostErr != nil {
		return "", hostErr
	}
	return "", fmt.Errorf("peer %s not found as indexer or host", peerID)
}
