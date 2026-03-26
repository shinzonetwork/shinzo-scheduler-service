package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/middleware"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/payment"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	subpkg "github.com/shinzonetwork/shinzo-scheduler-service/pkg/subscription"
)

type subscriptionActivator interface {
	Activate(ctx context.Context, req subpkg.ActivateRequest) error
}

type indexerLookup interface {
	GetByPeerID(ctx context.Context, peerID string) (*store.IndexerRecord, error)
}

// TxVerifier verifies that an on-chain transaction contains a matching payment event.
type TxVerifier interface {
	VerifySubscriptionPayment(ctx context.Context, txHash, subscriptionID string) error
}

// PaymentHandler serves /v1/quotes and /v1/payments/verify.
type PaymentHandler struct {
	mgr                   subscriptionActivator
	indexerSt             indexerLookup
	hostReg               hostVerifier
	txVerifier            TxVerifier // optional; nil disables on-chain verification
	floorTipPer1kBlocks   float64
	floorSnapshotPerRange float64
}

func NewPaymentHandler(mgr *subpkg.Manager, indexerSt *store.IndexerStore, hostReg *registry.HostRegistry) *PaymentHandler {
	return &PaymentHandler{mgr: mgr, indexerSt: indexerSt, hostReg: hostReg}
}

// WithFloorPricing sets the minimum acceptable pricing for indexers.
func (h *PaymentHandler) WithFloorPricing(tipPer1k, snapshotPerRange float64) {
	h.floorTipPer1kBlocks = tipPer1k
	h.floorSnapshotPerRange = snapshotPerRange
}

// WithTxVerifier attaches an on-chain verifier to the handler (Phase 3).
func (h *PaymentHandler) WithTxVerifier(v TxVerifier) {
	h.txVerifier = v
}

// Quote handles GET /v1/quotes?indexer_id=...&type=tip|snapshot&blocks=N
func (h *PaymentHandler) Quote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	indexerID := q.Get("indexer_id")
	subType := q.Get("type")
	if indexerID == "" || subType == "" {
		writeError(w, http.StatusBadRequest, "indexer_id and type are required")
		return
	}

	idx, err := h.indexerSt.GetByPeerID(r.Context(), indexerID)
	if err != nil || idx == nil {
		writeError(w, http.StatusNotFound, "indexer not found")
		return
	}

	var pricing store.Pricing
	if idx.Pricing != "" && idx.Pricing != "{}" {
		if err := json.Unmarshal([]byte(idx.Pricing), &pricing); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse indexer pricing")
			return
		}
	}
	if pricing.TipPer1kBlocks < h.floorTipPer1kBlocks || pricing.SnapshotPerRange < h.floorSnapshotPerRange {
		writeError(w, http.StatusBadRequest, "indexer pricing below floor")
		return
	}

	var price float64
	blockFrom, _ := strconv.Atoi(q.Get("block_from"))
	blockTo, _ := strconv.Atoi(q.Get("block_to"))

	switch subType {
	case store.SubTypeTip:
		blocks := float64(1000)
		if blocksStr := q.Get("blocks"); blocksStr != "" {
			var parseErr error
			blocks, parseErr = strconv.ParseFloat(blocksStr, 64)
			if parseErr != nil {
				writeError(w, http.StatusBadRequest, "blocks must be a number")
				return
			}
			if blocks <= 0 {
				blocks = 1000
			}
		}
		price = pricing.TipPer1kBlocks * (blocks / 1000)
	case store.SubTypeSnapshot:
		price = pricing.SnapshotPerRange
	default:
		writeError(w, http.StatusBadRequest, "type must be 'tip' or 'snapshot'")
		return
	}

	writeJSON(w, http.StatusOK, payment.Quote{
		IndexerID:   indexerID,
		SubType:     subType,
		BlockFrom:   blockFrom,
		BlockTo:     blockTo,
		PriceTokens: price,
		Currency:    "SHINZO",
		ValidUntil:  time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339),
	})
}

// VerifyPayment handles POST /v1/payments/verify.
// Phase 1: trust-based — the operator sends the subscription ID + payment ref.
// Phase 3: will verify the TxHash on ShinzoHub.
func (h *PaymentHandler) VerifyPayment(w http.ResponseWriter, r *http.Request) {
	// Only authenticated hosts or operators may call this.
	apiKey := middleware.APIKeyFromContext(r.Context())
	if _, err := h.hostReg.VerifyAPIKey(r.Context(), apiKey); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req payment.VerifyPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SubscriptionID == "" || req.PaymentRef == "" {
		writeError(w, http.StatusBadRequest, "subscription_id and payment_ref are required")
		return
	}

	// Phase 3: verify the on-chain transaction when a tx_hash is provided.
	if req.TxHash != "" && h.txVerifier != nil {
		if err := h.txVerifier.VerifySubscriptionPayment(r.Context(), req.TxHash, req.SubscriptionID); err != nil {
			writeError(w, http.StatusPaymentRequired, "tx verification failed: "+err.Error())
			return
		}
	}

	if err := h.mgr.Activate(r.Context(), subpkg.ActivateRequest{
		SubscriptionID: req.SubscriptionID,
		PaymentRef:     req.PaymentRef,
		ExpiresAt:      req.ExpiresAt,
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}
