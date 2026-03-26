package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/auth"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

// RegisterIndexerRequest is the payload an indexer submits to register itself.
// The defra_pk and peer_id fields must match the signed identity from GET /registration.
type RegisterIndexerRequest struct {
	// Identity fields (from indexer's GET /registration response)
	PeerID  string `json:"peer_id"`
	DefraPK string `json:"defra_pk"` // secp256k1 public key, hex-encoded
	// Signed messages for verification: map of message_hex → signature_hex
	SignedMessages map[string]string `json:"signed_messages"`

	// Service details
	HTTPUrl   string `json:"http_url"`
	Multiaddr string `json:"multiaddr"`
	Chain     string `json:"chain"`
	Network   string `json:"network"`
	Pricing   string `json:"pricing"` // JSON: {tipPer1kBlocks, snapshotPerRange}
}

// RegisterIndexerResponse is returned on successful registration.
type RegisterIndexerResponse struct {
	PeerID                   string `json:"peer_id"`
	APIKey                   string `json:"api_key"` // presented once; store securely
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
}

// HeartbeatRequest carries the latest state an indexer wants to report.
type HeartbeatRequest struct {
	CurrentTip     int    `json:"current_tip"`
	SnapshotRanges string `json:"snapshot_ranges"` // JSON array of SnapshotRange
}

// indexerStorer is the store subset used by IndexerRegistry.
type indexerStorer interface {
	GetByPeerID(ctx context.Context, peerID string) (*store.IndexerRecord, error)
	Create(ctx context.Context, r *store.IndexerRecord) (*store.IndexerRecord, error)
	Update(ctx context.Context, docID string, fields map[string]any) error
}

// IndexerRegistry manages indexer lifecycle.
type IndexerRegistry struct {
	store                    indexerStorer
	verifier                 *auth.Verifier
	log                      *zap.SugaredLogger
	chain                    string
	network                  string
	heartbeatIntervalSeconds int
}

func NewIndexerRegistry(s indexerStorer, v *auth.Verifier, log *zap.SugaredLogger, chain, network string, heartbeatInterval int) *IndexerRegistry {
	return &IndexerRegistry{store: s, verifier: v, log: log, chain: chain, network: network, heartbeatIntervalSeconds: heartbeatInterval}
}

// Register validates the signed payload, creates or refreshes the indexer record,
// and returns a newly-issued API key.
func (r *IndexerRegistry) Register(ctx context.Context, req RegisterIndexerRequest) (*RegisterIndexerResponse, error) {
	if err := r.verifyRegistration(req); err != nil {
		return nil, fmt.Errorf("registration rejected: %w", err)
	}

	plainKey, keyHash, err := r.verifier.IssueAPIKey(req.PeerID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	existing, err := r.store.GetByPeerID(ctx, req.PeerID)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		// Re-registration: update mutable fields and rotate the API key.
		if err := r.store.Update(ctx, existing.DocID, map[string]any{
			"httpUrl":    req.HTTPUrl,
			"multiaddr":  req.Multiaddr,
			"pricing":    req.Pricing,
			"status":     store.StatusActive,
			"apiKeyHash": keyHash,
		}); err != nil {
			return nil, err
		}
		r.log.Infow("indexer re-registered", "peer_id", req.PeerID)
	} else {
		rec := &store.IndexerRecord{
			PeerID:           req.PeerID,
			DefraPK:          req.DefraPK,
			HTTPUrl:          req.HTTPUrl,
			Multiaddr:        req.Multiaddr,
			Chain:            req.Chain,
			Network:          req.Network,
			CurrentTip:       0,
			SnapshotRanges:   "[]",
			Pricing:          req.Pricing,
			ReliabilityScore: 1.0,
			LastHeartbeat:    now,
			RegisteredAt:     now,
			Status:           store.StatusActive,
			APIKeyHash:       keyHash,
		}
		if _, err := r.store.Create(ctx, rec); err != nil {
			return nil, err
		}
		r.log.Infow("indexer registered", "peer_id", req.PeerID, "chain", req.Chain, "network", req.Network)
	}

	return &RegisterIndexerResponse{PeerID: req.PeerID, APIKey: plainKey, HeartbeatIntervalSeconds: r.heartbeatIntervalSeconds}, nil
}

// Heartbeat updates the indexer's current tip and snapshot list.
func (r *IndexerRegistry) Heartbeat(ctx context.Context, peerID string, req HeartbeatRequest) error {
	rec, err := r.store.GetByPeerID(ctx, peerID)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("indexer not found: %s", peerID)
	}

	return r.store.Update(ctx, rec.DocID, map[string]any{
		"currentTip":     req.CurrentTip,
		"snapshotRanges": req.SnapshotRanges,
		"lastHeartbeat":  time.Now().UTC().Format(time.RFC3339),
		"status":         store.StatusActive,
	})
}

// Deregister marks the indexer as inactive.
func (r *IndexerRegistry) Deregister(ctx context.Context, peerID string) error {
	rec, err := r.store.GetByPeerID(ctx, peerID)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("indexer not found: %s", peerID)
	}
	if err := r.store.Update(ctx, rec.DocID, map[string]any{"status": store.StatusInactive}); err != nil {
		return err
	}
	r.log.Infow("audit: indexer deregistered", "peer_id", peerID)
	return nil
}

// VerifyAPIKey checks the provided key against the stored hash for the given peer ID.
func (r *IndexerRegistry) VerifyAPIKey(ctx context.Context, apiKey string) (*store.IndexerRecord, error) {
	peerID, err := auth.ExtractPeerID(apiKey)
	if err != nil {
		return nil, err
	}
	rec, err := r.store.GetByPeerID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("indexer not found")
	}
	if err := r.verifier.VerifyAPIKey(apiKey, rec.APIKeyHash); err != nil {
		return nil, err
	}
	return rec, nil
}

// UpdateReliability persists the new reliability score computed by the prober.
func (r *IndexerRegistry) UpdateReliability(ctx context.Context, docID string, score float64, tip int, status string) error {
	fields := map[string]any{
		"reliabilityScore": score,
		"currentTip":       tip,
		"lastHeartbeat":    time.Now().UTC().Format(time.RFC3339),
	}
	if status != "" {
		fields["status"] = status
	}
	return r.store.Update(ctx, docID, fields)
}

// verifyRegistration checks that the signed_messages prove ownership of defra_pk.
// The indexer signs its own peer ID hex with the defra private key.
func (r *IndexerRegistry) verifyRegistration(req RegisterIndexerRequest) error {
	if req.PeerID == "" || req.DefraPK == "" {
		return fmt.Errorf("peer_id and defra_pk are required")
	}
	if req.HTTPUrl == "" || req.Multiaddr == "" {
		return fmt.Errorf("http_url and multiaddr are required")
	}
	if req.Chain == "" || req.Network == "" {
		return fmt.Errorf("chain and network are required")
	}
	if req.Chain != r.chain || req.Network != r.network {
		return fmt.Errorf("chain/network mismatch: scheduler accepts %s/%s", r.chain, r.network)
	}

	// Validate pricing JSON if provided.
	if req.Pricing != "" {
		var p store.Pricing
		if err := json.Unmarshal([]byte(req.Pricing), &p); err != nil {
			return fmt.Errorf("invalid pricing JSON: %w", err)
		}
	}

	// Verify all signed messages in signed_messages map.
	// Convention: the indexer signs the hex-encoded peer ID bytes.
	if len(req.SignedMessages) == 0 {
		return fmt.Errorf("no signed messages provided")
	}
	for msgHex, sigHex := range req.SignedMessages {
		if err := r.verifier.VerifySignature(req.DefraPK, msgHex, sigHex); err != nil {
			return fmt.Errorf("signature invalid for message %s: %w", msgHex, err)
		}
	}
	return nil
}
