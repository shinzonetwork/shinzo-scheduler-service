package settlement

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

type escrowStore interface {
	Create(ctx context.Context, r *store.EscrowAccountRecord) (*store.EscrowAccountRecord, error)
	GetBySession(ctx context.Context, sessionID string) (*store.EscrowAccountRecord, error)
	ListActive(ctx context.Context) ([]store.EscrowAccountRecord, error)
	Update(ctx context.Context, docID string, fields map[string]any) error
}

type ledgerReader interface {
	GetBySession(ctx context.Context, sessionID string) (*store.SessionLedgerRecord, error)
}

type EscrowManager struct {
	escrowSt escrowStore
	ledgerSt ledgerReader
	hub      HubBroadcaster
	cfg      config.SettlementConfig
	log      *zap.SugaredLogger
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewEscrowManager(
	escrowSt escrowStore,
	ledgerSt ledgerReader,
	hub HubBroadcaster,
	cfg config.SettlementConfig,
	log *zap.SugaredLogger,
) *EscrowManager {
	return &EscrowManager{
		escrowSt: escrowSt,
		ledgerSt: ledgerSt,
		hub:      hub,
		cfg:      cfg,
		log:      log,
		stopCh:   make(chan struct{}),
	}
}

func (em *EscrowManager) CreateEscrow(ctx context.Context, sessionID, hostID, indexerID string, initialBalance, pricePerBlock float64) error {
	lowWater := pricePerBlock * em.cfg.LowCreditMultiplier
	rec := &store.EscrowAccountRecord{
		EscrowID:          uuid.New().String(),
		SessionID:         sessionID,
		HostID:            hostID,
		IndexerID:         indexerID,
		InitialBalance:    initialBalance,
		CurrentBalance:    initialBalance,
		PricePerBlock:     pricePerBlock,
		LowWaterThreshold: lowWater,
		LowCreditSignaled: false,
		GracePeriodEndsAt: "",
		Status:            store.StatusActive,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	_, err := em.escrowSt.Create(ctx, rec)
	return err
}

func (em *EscrowManager) StartDrainLoop(ctx context.Context) {
	em.wg.Add(1)
	go func() {
		defer em.wg.Done()
		interval := time.Duration(em.cfg.DrainIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-em.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				em.drainAll(ctx)
			}
		}
	}()
	em.log.Infof("escrow drain loop started (interval=%ds)", em.cfg.DrainIntervalSeconds)
}

func (em *EscrowManager) Stop() {
	close(em.stopCh)
	em.wg.Wait()
}

func (em *EscrowManager) DrainSession(ctx context.Context, sessionID string) error {
	escrow, err := em.escrowSt.GetBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("escrow lookup for session %s: %w", sessionID, err)
	}
	if escrow == nil || escrow.Status != store.StatusActive {
		return nil
	}

	ledger, err := em.ledgerSt.GetBySession(ctx, sessionID)
	if err != nil || ledger == nil {
		return err
	}

	// Continuous credit drainage based on verified blocks.
	expectedDrain := float64(ledger.BlocksVerified) * escrow.PricePerBlock
	newBalance := escrow.InitialBalance - expectedDrain
	if newBalance < 0 {
		newBalance = 0
	}

	fields := map[string]any{
		"currentBalance": newBalance,
		"updatedAt":      time.Now().UTC().Format(time.RFC3339),
	}

	// Signal low credit when balance drops below threshold.
	if newBalance < escrow.LowWaterThreshold && !escrow.LowCreditSignaled {
		fields["lowCreditSignaled"] = true
		gracePeriodEnd := time.Now().Add(time.Duration(em.cfg.GracePeriodSeconds) * time.Second).UTC().Format(time.RFC3339)
		fields["gracePeriodEndsAt"] = gracePeriodEnd
		em.log.Warnw("low credit signal", "session", sessionID, "balance", newBalance, "threshold", escrow.LowWaterThreshold)

		if _, err := em.hub.BroadcastLowCredit(ctx, MsgSignalLowCredit{
			SessionID:       sessionID,
			CreditRemaining: newBalance,
			PricePerBlock:   escrow.PricePerBlock,
		}); err != nil {
			em.log.Warnf("broadcast low credit: %v", err)
		}
	}

	// Check grace period expiry.
	if escrow.LowCreditSignaled && escrow.GracePeriodEndsAt != "" {
		graceEnd, err := time.Parse(time.RFC3339, escrow.GracePeriodEndsAt)
		if err == nil && time.Now().After(graceEnd) {
			fields["status"] = store.StatusExpired
			em.log.Infow("escrow grace period expired, marking for disconnect", "session", sessionID)
		}
	}

	// Credit exhaustion.
	if newBalance <= 0 {
		fields["status"] = store.StatusExpired
	}

	return em.escrowSt.Update(ctx, escrow.DocID, fields)
}

func (em *EscrowManager) TopUp(ctx context.Context, sessionID string, amount float64) error {
	if amount <= 0 {
		return fmt.Errorf("topup amount must be positive, got %g", amount)
	}
	escrow, err := em.escrowSt.GetBySession(ctx, sessionID)
	if err != nil || escrow == nil {
		return fmt.Errorf("escrow not found for session %s", sessionID)
	}
	return em.escrowSt.Update(ctx, escrow.DocID, map[string]any{
		"currentBalance":    escrow.CurrentBalance + amount,
		"initialBalance":    escrow.InitialBalance + amount,
		"lowCreditSignaled": false,
		"gracePeriodEndsAt": "",
		"status":            store.StatusActive,
		"updatedAt":         time.Now().UTC().Format(time.RFC3339),
	})
}

func (em *EscrowManager) drainAll(ctx context.Context) {
	active, err := em.escrowSt.ListActive(ctx)
	if err != nil {
		em.log.Warnf("escrow drain: list active: %v", err)
		return
	}
	for _, e := range active {
		if err := em.DrainSession(ctx, e.SessionID); err != nil {
			em.log.Debugw("escrow drain", "session", e.SessionID, "error", err)
		}
	}
}
