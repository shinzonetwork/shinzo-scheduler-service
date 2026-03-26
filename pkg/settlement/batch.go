package settlement

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

type settlementStore interface {
	Create(ctx context.Context, r *store.SettlementRecord) (*store.SettlementRecord, error)
	ListByBatch(ctx context.Context, batchID string) ([]store.SettlementRecord, error)
	Update(ctx context.Context, docID string, fields map[string]any) error
}

// BatchProcessor handles batch settlement to ShinzoHub.
type BatchProcessor struct {
	settleSt settlementStore
	escrowSt escrowStore
	ledgerSt ledgerReader
	hub      HubBroadcaster
	log      *zap.SugaredLogger
}

func NewBatchProcessor(
	settleSt settlementStore,
	escrowSt escrowStore,
	ledgerSt ledgerReader,
	hub HubBroadcaster,
	log *zap.SugaredLogger,
) *BatchProcessor {
	return &BatchProcessor{
		settleSt: settleSt,
		escrowSt: escrowSt,
		ledgerSt: ledgerSt,
		hub:      hub,
		log:      log,
	}
}

// ProcessBatch settles a list of sessions atomically.
func (bp *BatchProcessor) ProcessBatch(ctx context.Context, sessionIDs []string, closeReason string) error {
	if len(sessionIDs) == 0 {
		return nil
	}

	batchID := uuid.New().String()
	var settlements []MsgCloseSession
	var records []*store.SettlementRecord

	// Validate all sessions before committing any.
	for _, sid := range sessionIDs {
		escrow, err := bp.escrowSt.GetBySession(ctx, sid)
		if err != nil {
			return fmt.Errorf("batch aborted: escrow lookup for %s: %w", sid, err)
		}
		if escrow == nil {
			return fmt.Errorf("batch aborted: no escrow for session %s", sid)
		}

		ledger, err := bp.ledgerSt.GetBySession(ctx, sid)
		if err != nil {
			return fmt.Errorf("batch aborted: ledger lookup for %s: %w", sid, err)
		}
		if ledger == nil {
			return fmt.Errorf("batch aborted: no ledger for session %s", sid)
		}

		// Final payment = blocks_verified * price_per_block.
		indexerAmount := float64(ledger.BlocksVerified) * ledger.PricePerBlock
		// Remaining escrow returned to host.
		hostRefund := escrow.InitialBalance - indexerAmount
		if hostRefund < 0 {
			hostRefund = 0
		}

		msg := MsgCloseSession{
			SessionID:      sid,
			CloseReason:    closeReason,
			BlocksVerified: ledger.BlocksVerified,
			IndexerAmount:  indexerAmount,
			HostRefund:     hostRefund,
		}
		settlements = append(settlements, msg)

		records = append(records, &store.SettlementRecord{
			SettlementID:   uuid.New().String(),
			BatchID:        batchID,
			SessionID:      sid,
			BlocksVerified: ledger.BlocksVerified,
			IndexerAmount:  indexerAmount,
			HostRefund:     hostRefund,
			CloseReason:    closeReason,
			Status:         store.StatusPending,
			SettledAt:      time.Now().UTC().Format(time.RFC3339),
		})
	}

	// Broadcast batch settlement to ShinzoHub.
	txHash, err := bp.hub.BroadcastBatchSettlement(ctx, MsgBatchSettlement{
		BatchID:  batchID,
		Sessions: settlements,
	})
	if err != nil {
		return fmt.Errorf("batch settlement broadcast failed (all reverted): %w", err)
	}

	// Record settlements and mark escrows as settled.
	for _, rec := range records {
		rec.TxHash = txHash
		rec.Status = store.StatusActive // settled
		if _, err := bp.settleSt.Create(ctx, rec); err != nil {
			bp.log.Warnf("record settlement for %s: %v", rec.SessionID, err)
		}
	}

	for _, sid := range sessionIDs {
		escrow, err := bp.escrowSt.GetBySession(ctx, sid)
		if err != nil {
			bp.log.Warnw("post-settlement escrow lookup failed", "session", sid, "batch", batchID, "error", err)
			continue
		}
		if escrow == nil {
			bp.log.Warnw("post-settlement escrow not found", "session", sid, "batch", batchID)
			continue
		}
		if err := bp.escrowSt.Update(ctx, escrow.DocID, map[string]any{
			"status":    "settled",
			"updatedAt": time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			bp.log.Warnw("post-settlement escrow update failed", "session", sid, "batch", batchID, "error", err)
		}
	}

	bp.log.Infow("batch settlement completed", "batch_id", batchID, "sessions", len(sessionIDs), "tx_hash", txHash)
	return nil
}
