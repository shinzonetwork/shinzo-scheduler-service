package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/auth"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

// RegisterHostRequest is the payload a host submits at registration.
type RegisterHostRequest struct {
	PeerID         string            `json:"peer_id"`
	DefraPK        string            `json:"defra_pk"`
	SignedMessages map[string]string `json:"signed_messages"`
	HTTPUrl        string            `json:"http_url"`
	Multiaddr      string            `json:"multiaddr"`
	Chain          string            `json:"chain"`
	Network        string            `json:"network"`
}

// RegisterHostResponse is returned on successful registration.
type RegisterHostResponse struct {
	PeerID                   string `json:"peer_id"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
}

// hostStorer is the store subset used by HostRegistry.
type hostStorer interface {
	GetByPeerID(ctx context.Context, peerID string) (*store.HostRecord, error)
	Create(ctx context.Context, r *store.HostRecord) (*store.HostRecord, error)
	Update(ctx context.Context, docID string, fields map[string]any) error
}

type HostRegistry struct {
	store                    hostStorer
	verifier                 *auth.Verifier
	log                      *zap.SugaredLogger
	chain                    string
	network                  string
	heartbeatIntervalSeconds int
}

func NewHostRegistry(s hostStorer, v *auth.Verifier, log *zap.SugaredLogger, chain, network string, heartbeatInterval int) *HostRegistry {
	return &HostRegistry{store: s, verifier: v, log: log, chain: chain, network: network, heartbeatIntervalSeconds: heartbeatInterval}
}

func (r *HostRegistry) Register(ctx context.Context, req RegisterHostRequest) (*RegisterHostResponse, error) {
	if err := r.verifyRegistration(req); err != nil {
		return nil, fmt.Errorf("registration rejected: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	existing, err := r.store.GetByPeerID(ctx, req.PeerID)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		if err := r.store.Update(ctx, existing.DocID, map[string]any{
			"httpUrl":   req.HTTPUrl,
			"multiaddr": req.Multiaddr,
			"status":    store.StatusActive,
		}); err != nil {
			return nil, err
		}
		r.log.Infow("host re-registered", "peer_id", req.PeerID)
	} else {
		rec := &store.HostRecord{
			PeerID:        req.PeerID,
			DefraPK:       req.DefraPK,
			HTTPUrl:       req.HTTPUrl,
			Multiaddr:     req.Multiaddr,
			Chain:         req.Chain,
			Network:       req.Network,
			LastHeartbeat: now,
			RegisteredAt:  now,
			Status:        store.StatusActive,
		}
		if _, err := r.store.Create(ctx, rec); err != nil {
			return nil, err
		}
		r.log.Infow("host registered", "peer_id", req.PeerID)
	}

	return &RegisterHostResponse{PeerID: req.PeerID, HeartbeatIntervalSeconds: r.heartbeatIntervalSeconds}, nil
}

func (r *HostRegistry) Heartbeat(ctx context.Context, peerID string) error {
	rec, err := r.store.GetByPeerID(ctx, peerID)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("host not found: %s", peerID)
	}
	return r.store.Update(ctx, rec.DocID, map[string]any{
		"lastHeartbeat": time.Now().UTC().Format(time.RFC3339),
		"status":        store.StatusActive,
	})
}

func (r *HostRegistry) Deregister(ctx context.Context, peerID string) error {
	rec, err := r.store.GetByPeerID(ctx, peerID)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("host not found: %s", peerID)
	}
	if err := r.store.Update(ctx, rec.DocID, map[string]any{"status": store.StatusInactive}); err != nil {
		return err
	}
	r.log.Infow("audit: host deregistered", "peer_id", peerID)
	return nil
}

// VerifyRequest authenticates a per-request Bearer token by looking up the
// host's stored secp256k1 public key and verifying the token signature.
func (r *HostRegistry) VerifyRequest(ctx context.Context, token string) (*store.HostRecord, error) {
	peerID, err := auth.ExtractPeerID(token)
	if err != nil {
		return nil, err
	}
	rec, err := r.store.GetByPeerID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("host not found")
	}
	if _, err := r.verifier.VerifyRequestToken(rec.DefraPK, token); err != nil {
		return nil, err
	}
	return rec, nil
}

func (r *HostRegistry) verifyRegistration(req RegisterHostRequest) error {
	if req.PeerID == "" || req.DefraPK == "" {
		return fmt.Errorf("peer_id and defra_pk are required")
	}
	if req.Chain == "" || req.Network == "" {
		return fmt.Errorf("chain and network are required")
	}
	if req.Chain != r.chain || req.Network != r.network {
		return fmt.Errorf("chain/network mismatch: scheduler accepts %s/%s", r.chain, r.network)
	}
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
