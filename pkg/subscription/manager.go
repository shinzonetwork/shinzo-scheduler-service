package subscription

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

// matchRecorder writes match history entries for diversity tracking.
type matchRecorder interface {
	Create(ctx context.Context, r *store.MatchHistoryRecord) (*store.MatchHistoryRecord, error)
}

// subStore is the store subset used by subscription management.
type subStore interface {
	Create(ctx context.Context, r *store.SubscriptionRecord) (*store.SubscriptionRecord, error)
	GetByID(ctx context.Context, subID string) (*store.SubscriptionRecord, error)
	ListByHost(ctx context.Context, hostID string) ([]store.SubscriptionRecord, error)
	ListByStatus(ctx context.Context, status string) ([]store.SubscriptionRecord, error)
	Update(ctx context.Context, docID string, fields map[string]any) error
}

// indexerQuerier is the store subset needed to look up an indexer.
type indexerQuerier interface {
	GetByPeerID(ctx context.Context, peerID string) (*store.IndexerRecord, error)
}

type Manager struct {
	subSt     subStore
	indexerSt indexerQuerier
	matchSt   matchRecorder // nil disables match history recording
	log       *zap.SugaredLogger
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func NewManager(subSt subStore, indexerSt indexerQuerier, log *zap.SugaredLogger) *Manager {
	return &Manager{
		subSt:     subSt,
		indexerSt: indexerSt,
		log:       log,
		stopCh:    make(chan struct{}),
	}
}

func (m *Manager) WithMatchRecorder(ms matchRecorder) {
	m.matchSt = ms
}

func (m *Manager) Create(ctx context.Context, req CreateRequest) (*store.SubscriptionRecord, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}

	// Verify the indexer exists and is active.
	indexer, err := m.indexerSt.GetByPeerID(ctx, req.IndexerID)
	if err != nil {
		return nil, err
	}
	if indexer == nil || indexer.Status != store.StatusActive {
		return nil, fmt.Errorf("indexer %s is not available", req.IndexerID)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	rec := &store.SubscriptionRecord{
		SubscriptionID: uuid.New().String(),
		HostID:         req.HostID,
		IndexerID:      req.IndexerID,
		SubType:        req.SubType,
		BlockFrom:      req.BlockFrom,
		BlockTo:        req.BlockTo,
		Status:         store.StatusPending,
		PaymentRef:     "",
		CreatedAt:      now,
		ActivatedAt:    "",
		ExpiresAt:      "",
		Metadata:       req.Metadata,
	}

	created, err := m.subSt.Create(ctx, rec)
	if err != nil {
		return nil, err
	}
	m.log.Infow("subscription created", "id", created.SubscriptionID, "host", req.HostID, "indexer", req.IndexerID)

	// Record match history for diversity weighting.
	if m.matchSt != nil {
		_, err := m.matchSt.Create(ctx, &store.MatchHistoryRecord{
			MatchID:   uuid.New().String(),
			HostID:    req.HostID,
			IndexerID: req.IndexerID,
			MatchType: req.SubType,
			MatchedAt: now,
		})
		if err != nil {
			m.log.Warnf("record match history: %v", err)
		}
	}

	return created, nil
}

func (m *Manager) ListByHost(ctx context.Context, hostID string) ([]store.SubscriptionRecord, error) {
	return m.subSt.ListByHost(ctx, hostID)
}

func (m *Manager) Activate(ctx context.Context, req ActivateRequest) error {
	sub, err := m.subSt.GetByID(ctx, req.SubscriptionID)
	if err != nil {
		return err
	}
	if sub == nil {
		return fmt.Errorf("subscription not found: %s", req.SubscriptionID)
	}
	if sub.Status != store.StatusPending {
		return fmt.Errorf("subscription %s is not pending (status=%s)", req.SubscriptionID, sub.Status)
	}

	fields := map[string]any{
		"status":      store.StatusActive,
		"paymentRef":  req.PaymentRef,
		"activatedAt": time.Now().UTC().Format(time.RFC3339),
	}
	if req.ExpiresAt != "" {
		fields["expiresAt"] = req.ExpiresAt
	}

	if err := m.subSt.Update(ctx, sub.DocID, fields); err != nil {
		return err
	}
	m.log.Infow("subscription activated", "id", req.SubscriptionID, "payment_ref", req.PaymentRef)
	return nil
}

func (m *Manager) Cancel(ctx context.Context, subID string) error {
	sub, err := m.subSt.GetByID(ctx, subID)
	if err != nil {
		return err
	}
	if sub == nil {
		return fmt.Errorf("subscription not found: %s", subID)
	}
	if sub.Status == store.StatusExpired || sub.Status == store.StatusCancelled {
		return fmt.Errorf("subscription %s is already %s", subID, sub.Status)
	}
	if err := m.subSt.Update(ctx, sub.DocID, map[string]any{"status": store.StatusCancelled}); err != nil {
		return err
	}
	m.log.Infow("audit: subscription cancelled", "id", subID, "host", sub.HostID, "indexer", sub.IndexerID)
	return nil
}

func (m *Manager) Get(ctx context.Context, subID string) (*store.SubscriptionRecord, *store.IndexerRecord, error) {
	sub, err := m.subSt.GetByID(ctx, subID)
	if err != nil {
		return nil, nil, err
	}
	if sub == nil {
		return nil, nil, fmt.Errorf("subscription not found: %s", subID)
	}

	var indexer *store.IndexerRecord
	if sub.Status == store.StatusActive {
		indexer, err = m.indexerSt.GetByPeerID(ctx, sub.IndexerID)
		if err != nil {
			return nil, nil, err
		}
	}
	return sub, indexer, nil
}

func (m *Manager) StartExpiryLoop(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.expireOverdue(ctx)
			}
		}
	}()
}

func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

func (m *Manager) expireOverdue(ctx context.Context) {
	actives, err := m.subSt.ListByStatus(ctx, store.StatusActive)
	if err != nil {
		m.log.Warnf("expiry loop: list active subs: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, sub := range actives {
		if sub.ExpiresAt == "" {
			continue
		}
		exp, err := time.Parse(time.RFC3339, sub.ExpiresAt)
		if err != nil {
			continue
		}
		if now.After(exp) {
			if err := m.subSt.Update(ctx, sub.DocID, map[string]any{"status": store.StatusExpired}); err != nil {
				m.log.Warnf("expiry loop: expire %s: %v", sub.SubscriptionID, err)
			} else {
				m.log.Infow("audit: subscription expired", "id", sub.SubscriptionID, "host", sub.HostID, "indexer", sub.IndexerID)
			}
		}
	}
}

func validateCreate(req CreateRequest) error {
	if req.HostID == "" {
		return fmt.Errorf("host_id is required")
	}
	if req.IndexerID == "" {
		return fmt.Errorf("indexer_id is required")
	}
	if req.SubType != store.SubTypeTip && req.SubType != store.SubTypeSnapshot {
		return fmt.Errorf("sub_type must be 'tip' or 'snapshot'")
	}
	if req.SubType == store.SubTypeSnapshot {
		if req.BlockFrom == 0 || req.BlockTo == 0 {
			return fmt.Errorf("block_from and block_to are required for snapshot subscriptions")
		}
		if req.BlockFrom >= req.BlockTo {
			return fmt.Errorf("block_from must be less than block_to")
		}
	}
	return nil
}
