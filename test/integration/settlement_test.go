//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/accounting"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/settlement"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestSettlementFlow_EscrowDrain creates an escrow, sets verified blocks on the
// ledger, drains, and verifies the escrow balance decreased accordingly.
func TestSettlementFlow_EscrowDrain(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	log := zap.NewNop().Sugar()
	hub := &recordingBroadcaster{}
	cfg := testSettlementCfg()

	escrowSt := store.NewEscrowStore(db)
	ledgerSt := store.NewLedgerStore(db)

	em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)

	sessionID := "sess-drain-1"
	initialBalance := 100.0
	pricePerBlock := 2.0

	require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-1", "idx-1", initialBalance, pricePerBlock))

	acctMgr := newAccountingManager(db, testAccountingCfg())
	require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-1", "idx-1", initialBalance, pricePerBlock))

	for i := 1; i <= 5; i++ {
		cids := cidJSON("cid-drain")
		_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-1", BlockNumber: i,
			DocCids: cids, BlockHash: "0xdrain",
		})
		require.NoError(t, err)
		_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
			SessionID: sessionID, HostID: "host-1", BlockNumber: i,
			DocCidsReceived: cids,
		})
		require.NoError(t, err)
		_, err = acctMgr.Compare(ctx, sessionID, i)
		require.NoError(t, err)
	}

	ledger, err := acctMgr.GetSessionLedger(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, 5, ledger.BlocksVerified)

	require.NoError(t, em.DrainSession(ctx, sessionID))

	escrow, err := escrowSt.GetBySession(ctx, sessionID)
	require.NoError(t, err)
	expectedBalance := initialBalance - (float64(5) * pricePerBlock)
	require.InDelta(t, expectedBalance, escrow.CurrentBalance, 0.001)
}

// TestSettlementFlow_LowCreditSignal drains an escrow below the low water
// threshold and verifies the hub received a low credit broadcast.
func TestSettlementFlow_LowCreditSignal(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	log := zap.NewNop().Sugar()
	hub := &recordingBroadcaster{}
	cfg := testSettlementCfg()
	cfg.LowCreditMultiplier = 5.0

	escrowSt := store.NewEscrowStore(db)
	ledgerSt := store.NewLedgerStore(db)
	em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)

	sessionID := "sess-lowcredit-1"
	pricePerBlock := 10.0
	initialBalance := 100.0

	require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-1", "idx-1", initialBalance, pricePerBlock))

	acctMgr := newAccountingManager(db, testAccountingCfg())
	require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-1", "idx-1", initialBalance, pricePerBlock))

	for i := 1; i <= 6; i++ {
		cids := cidJSON("cid-low")
		_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-1", BlockNumber: i,
			DocCids: cids, BlockHash: "0xlow",
		})
		require.NoError(t, err)
		_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
			SessionID: sessionID, HostID: "host-1", BlockNumber: i,
			DocCidsReceived: cids,
		})
		require.NoError(t, err)
		_, err = acctMgr.Compare(ctx, sessionID, i)
		require.NoError(t, err)
	}

	require.NoError(t, em.DrainSession(ctx, sessionID))

	hub.mu.Lock()
	defer hub.mu.Unlock()
	require.Len(t, hub.lowCreditCalls, 1, "expected exactly one low credit signal")
	require.Equal(t, sessionID, hub.lowCreditCalls[0].SessionID)
}

// TestSettlementFlow_BatchSettlement verifies that ProcessBatch creates settlement
// records and broadcasts a batch settlement message.
func TestSettlementFlow_BatchSettlement(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	log := zap.NewNop().Sugar()
	hub := &recordingBroadcaster{}

	escrowSt := store.NewEscrowStore(db)
	ledgerSt := store.NewLedgerStore(db)
	settleSt := store.NewSettlementStore(db)

	bp := settlement.NewBatchProcessor(settleSt, escrowSt, ledgerSt, hub, log)

	sessionID := "sess-batch-1"
	pricePerBlock := 5.0
	initialBalance := 200.0

	cfg := testSettlementCfg()
	em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)
	require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-1", "idx-1", initialBalance, pricePerBlock))

	acctMgr := newAccountingManager(db, testAccountingCfg())
	require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-1", "idx-1", initialBalance, pricePerBlock))

	for i := 1; i <= 3; i++ {
		cids := cidJSON("cid-batch")
		_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-1", BlockNumber: i,
			DocCids: cids, BlockHash: "0xbatch",
		})
		require.NoError(t, err)
		_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
			SessionID: sessionID, HostID: "host-1", BlockNumber: i,
			DocCidsReceived: cids,
		})
		require.NoError(t, err)
		_, err = acctMgr.Compare(ctx, sessionID, i)
		require.NoError(t, err)
	}

	require.NoError(t, bp.ProcessBatch(ctx, []string{sessionID}, store.CloseReasonHostInitiated))

	hub.mu.Lock()
	require.Len(t, hub.batchCalls, 1)
	require.Len(t, hub.batchCalls[0].Sessions, 1)
	require.Equal(t, sessionID, hub.batchCalls[0].Sessions[0].SessionID)
	require.Equal(t, 3, hub.batchCalls[0].Sessions[0].BlocksVerified)
	hub.mu.Unlock()

	recs, err := settleSt.ListBySession(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.Equal(t, "tx-batch", recs[0].TxHash)
	require.InDelta(t, 15.0, recs[0].IndexerAmount, 0.001)

	escrow, err := escrowSt.GetBySession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, "settled", escrow.Status)
}

// TestSettlementFlow_VerdictCreation creates a verdict, adds a co-signer signature,
// and checks quorum behaviour with different thresholds.
func TestSettlementFlow_VerdictCreation(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	log := zap.NewNop().Sugar()
	hub := &recordingBroadcaster{}
	cfg := testSettlementCfg()
	cfg.VerdictThresholdM = 3

	verdictSt := store.NewVerdictStore(db)
	vm := settlement.NewVerdictManager(verdictSt, hub, cfg, log)

	sessionID := "sess-verdict-1"

	verdict, err := vm.CreateVerdict(ctx, sessionID, store.OutcomeMismatch, []string{"evidence-cid-1"})
	require.NoError(t, err)
	require.NotEmpty(t, verdict.VerdictID)
	require.Equal(t, store.OutcomeMismatch, verdict.Outcome)

	hasQ, err := vm.HasQuorum(ctx, sessionID)
	require.NoError(t, err)
	require.False(t, hasQ)

	require.NoError(t, vm.AddSignature(ctx, sessionID, "cosigner-node-2"))
	hasQ, err = vm.HasQuorum(ctx, sessionID)
	require.NoError(t, err)
	require.False(t, hasQ)

	require.NoError(t, vm.AddSignature(ctx, sessionID, "cosigner-node-3"))
	hasQ, err = vm.HasQuorum(ctx, sessionID)
	require.NoError(t, err)
	require.True(t, hasQ)

	require.NoError(t, vm.SubmitToHub(ctx, sessionID))

	stored, err := verdictSt.GetBySession(ctx, sessionID)
	require.NoError(t, err)
	require.True(t, stored.SubmittedToHub)
}

// TestSettlementFlow_TopUpClearsLowCredit drains below threshold, tops up, and
// verifies the low credit signal is cleared.
func TestSettlementFlow_TopUpClearsLowCredit(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	log := zap.NewNop().Sugar()
	hub := &recordingBroadcaster{}
	cfg := testSettlementCfg()
	cfg.LowCreditMultiplier = 5.0

	escrowSt := store.NewEscrowStore(db)
	ledgerSt := store.NewLedgerStore(db)
	em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)

	sessionID := "sess-topup-1"
	pricePerBlock := 10.0
	initialBalance := 100.0

	require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-1", "idx-1", initialBalance, pricePerBlock))

	acctMgr := newAccountingManager(db, testAccountingCfg())
	require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-1", "idx-1", initialBalance, pricePerBlock))

	for i := 1; i <= 7; i++ {
		cids := cidJSON("cid-topup")
		_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-1", BlockNumber: i,
			DocCids: cids, BlockHash: "0xtopup",
		})
		require.NoError(t, err)
		_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
			SessionID: sessionID, HostID: "host-1", BlockNumber: i,
			DocCidsReceived: cids,
		})
		require.NoError(t, err)
		_, err = acctMgr.Compare(ctx, sessionID, i)
		require.NoError(t, err)
	}

	require.NoError(t, em.DrainSession(ctx, sessionID))

	escrow, err := escrowSt.GetBySession(ctx, sessionID)
	require.NoError(t, err)
	require.True(t, escrow.LowCreditSignaled)
	require.NotEmpty(t, escrow.GracePeriodEndsAt)

	require.NoError(t, em.TopUp(ctx, sessionID, 200.0))

	escrow, err = escrowSt.GetBySession(ctx, sessionID)
	require.NoError(t, err)
	require.False(t, escrow.LowCreditSignaled, "low credit flag should be cleared after top-up")
	require.Empty(t, escrow.GracePeriodEndsAt, "grace period should be cleared after top-up")
	require.Equal(t, store.StatusActive, escrow.Status)
}
