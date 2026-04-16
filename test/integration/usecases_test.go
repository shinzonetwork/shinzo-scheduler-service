//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/accounting"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/discovery"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/settlement"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Indexer Lifecycle

func TestIndexerLifecycle(t *testing.T) {

	t.Run("register_serve_earn_settle", func(t *testing.T) {
		h := newFullHarness(t)
		ctx := context.Background()

		pricingJSON, _ := json.Marshal(store.Pricing{TipPer1kBlocks: 10.0})
		idxID := "idx-happy-1"
		_ = h.registerIndexerWithOpts(t, &store.IndexerRecord{
			PeerID:           idxID,
			HTTPUrl:          "http://" + idxID + ":8080",
			Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/" + idxID,
			CurrentTip:       500,
			Pricing:          string(pricingJSON),
			ReliabilityScore: 1.0,
		})

		hostID := "host-happy-1"
		hostKey := h.registerHost(t, hostID)

		// Host discovers the indexer.
		url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s", testChain, testNetwork)
		resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var matches []discovery.IndexerMatch
		decodeJSON(t, resp, &matches)

		found := false
		for _, m := range matches {
			if m.PeerID == idxID {
				found = true
			}
		}
		require.True(t, found, "indexer should appear in discovery results")

		// Set up session: subscription + ledger + escrow.
		// Create subscription via HTTP, activate it, then set up accounting.
		pricePerBlock := 1.0
		resp = h.doRequest(t, http.MethodPost, "/v1/subscriptions", map[string]any{
			"host_id": hostID, "indexer_id": idxID, "sub_type": "tip",
		}, hostKey)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		var subRec store.SubscriptionRecord
		decodeJSON(t, resp, &subRec)
		sessionID := subRec.SubscriptionID
		require.NotEmpty(t, sessionID)

		resp = h.doRequest(t, http.MethodPost, "/v1/payments/verify", map[string]any{
			"subscription_id": sessionID,
			"payment_ref":     "test-pay-happy",
			"expires_at":      time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		}, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		require.NoError(t, h.acctMgr.CreateSessionLedger(ctx, sessionID, hostID, idxID, 100.0, pricePerBlock))
		require.NoError(t, h.escrowMgr.CreateEscrow(ctx, sessionID, hostID, idxID, 100.0, pricePerBlock))

		// Deliver 10 clean blocks.
		for i := 1; i <= 10; i++ {
			h.submitCleanBlock(t, sessionID, idxID, hostID, i)
		}

		// Verify ledger state.
		ledger, err := h.acctMgr.GetSessionLedger(ctx, sessionID)
		require.NoError(t, err)
		require.Equal(t, 10, ledger.BlocksVerified)
		require.InDelta(t, 90.0, ledger.CreditRemaining, 0.001)

		// Drain escrow and batch settle.
		require.NoError(t, h.escrowMgr.DrainSession(ctx, sessionID))
		require.NoError(t, h.batchProc.ProcessBatch(ctx, []string{sessionID}, store.CloseReasonHostInitiated))

		// Verify settlement message.
		h.hub.mu.Lock()
		require.Len(t, h.hub.batchCalls, 1)
		sess := h.hub.batchCalls[0].Sessions[0]
		assert.InDelta(t, 10.0, sess.IndexerAmount, 0.001)
		assert.InDelta(t, 90.0, sess.HostRefund, 0.001)
		h.hub.mu.Unlock()

		// Verify escrow marked settled.
		escrow, err := h.escrowSt.GetBySession(ctx, sessionID)
		require.NoError(t, err)
		assert.Equal(t, "settled", escrow.Status)
	})

	t.Run("stale_mid_session_replaced", func(t *testing.T) {
		h := newFullHarness(t)
		ctx := context.Background()

		idxA := "idx-stale-a"
		idxB := "idx-stale-b"
		h.registerIndexerWithOpts(t, &store.IndexerRecord{
			PeerID:           idxA,
			HTTPUrl:          "http://" + idxA + ":8080",
			Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/" + idxA,
			CurrentTip:       100,
			ReliabilityScore: 1.0,
		})
		h.registerIndexerWithOpts(t, &store.IndexerRecord{
			PeerID:           idxB,
			HTTPUrl:          "http://" + idxB + ":8080",
			Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/" + idxB,
			CurrentTip:       100,
			ReliabilityScore: 1.0,
		})

		hostID := "host-stale-repl"
		hostKey := h.registerHost(t, hostID)

		// Both should be discoverable initially.
		url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s", testChain, testNetwork)
		resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var matches []discovery.IndexerMatch
		decodeJSON(t, resp, &matches)
		require.GreaterOrEqual(t, len(matches), 2)

		// Set up session with A, deliver 5 blocks.
		idxAKey := h.registerIndexer(t, idxA+"-key") // need the key; re-register is fine for a unique peer
		_ = idxAKey
		// Use the already-registered A. We need setupSession which requires keys.
		// Actually we already registered A without getting its key. Let's just
		// create the session manually using accounting manager directly.
		sessionA := "sess-stale-a"
		require.NoError(t, h.acctMgr.CreateSessionLedger(ctx, sessionA, hostID, idxA, 100.0, 1.0))
		require.NoError(t, h.escrowMgr.CreateEscrow(ctx, sessionA, hostID, idxA, 100.0, 1.0))
		for i := 1; i <= 5; i++ {
			cids := cidJSON(fmt.Sprintf("cid-stale-%d", i))
			_, err := h.acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
				SessionID: sessionA, IndexerID: idxA, BlockNumber: i,
				DocCids: cids, BlockHash: fmt.Sprintf("0xstale-%d", i),
			})
			require.NoError(t, err)
			_, err = h.acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
				SessionID: sessionA, HostID: hostID, BlockNumber: i,
				DocCidsReceived: cids,
			})
			require.NoError(t, err)
			_, err = h.acctMgr.Compare(ctx, sessionA, i)
			require.NoError(t, err)
		}

		// Age A's heartbeat to 5 minutes ago (past staleness window of 120s).
		recA, err := h.indexerSt.GetByPeerID(ctx, idxA)
		require.NoError(t, err)
		oldTime := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
		require.NoError(t, h.indexerSt.Update(ctx, recA.DocID, map[string]any{
			"lastHeartbeat": oldTime,
		}))

		// Re-query: A should be gone, B should remain.
		resp = h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var matches2 []discovery.IndexerMatch
		decodeJSON(t, resp, &matches2)

		foundA, foundB := false, false
		for _, m := range matches2 {
			if m.PeerID == idxA {
				foundA = true
			}
			if m.PeerID == idxB {
				foundB = true
			}
		}
		assert.False(t, foundA, "stale indexer A should be excluded")
		assert.True(t, foundB, "fresh indexer B should remain")
	})

	t.Run("snapshot_session_with_fallback", func(t *testing.T) {
		h := newHarness(t)

		snapRanges, _ := json.Marshal([]store.SnapshotRange{
			{Start: 100, End: 200, File: "snap-100-200.tar.gz", SizeBytes: 1024},
		})
		h.registerIndexerWithOpts(t, &store.IndexerRecord{
			PeerID:           "idx-snap-a",
			HTTPUrl:          "http://idx-snap-a:8080",
			Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-snap-a",
			CurrentTip:       500,
			SnapshotRanges:   string(snapRanges),
			ReliabilityScore: 1.0,
		})
		h.registerIndexerWithOpts(t, &store.IndexerRecord{
			PeerID:           "idx-snap-b",
			HTTPUrl:          "http://idx-snap-b:8080",
			Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/idx-snap-b",
			CurrentTip:       500,
			ReliabilityScore: 1.0,
		})

		hostKey := h.registerHost(t, "host-snap-uc")

		// Query for range 100-200: A has snapshot, B does not.
		url := fmt.Sprintf("/v1/discover/snapshots?chain=%s&network=%s&block_from=100&block_to=200", testChain, testNetwork)
		resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var matches []discovery.IndexerMatch
		decodeJSON(t, resp, &matches)
		require.GreaterOrEqual(t, len(matches), 2)

		// Snapshot holder should be first.
		assert.Equal(t, "idx-snap-a", matches[0].PeerID)
		assert.False(t, matches[0].SnapshotCreationRequired)

		// Fallback should require creation.
		var bMatch *discovery.IndexerMatch
		for i := range matches {
			if matches[i].PeerID == "idx-snap-b" {
				bMatch = &matches[i]
			}
		}
		require.NotNil(t, bMatch)
		assert.True(t, bMatch.SnapshotCreationRequired)

		// Query for range 300-400: neither has a snapshot.
		url2 := fmt.Sprintf("/v1/discover/snapshots?chain=%s&network=%s&block_from=300&block_to=400", testChain, testNetwork)
		resp2 := h.doRequest(t, http.MethodGet, url2, nil, hostKey)
		require.Equal(t, http.StatusOK, resp2.StatusCode)
		var matches2 []discovery.IndexerMatch
		decodeJSON(t, resp2, &matches2)
		for _, m := range matches2 {
			assert.True(t, m.SnapshotCreationRequired, "all should require snapshot creation for range 300-400")
		}
	})
}

// Host Lifecycle

func TestHostLifecycle(t *testing.T) {

	t.Run("credit_drain_low_signal_topup_settle", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		log := zap.NewNop().Sugar()
		hub := &recordingBroadcaster{}
		cfg := testSettlementCfg()
		cfg.LowCreditMultiplier = 5.0 // threshold = 5 * 10 = 50

		escrowSt := store.NewEscrowStore(db)
		ledgerSt := store.NewLedgerStore(db)
		settleSt := store.NewSettlementStore(db)

		em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)
		bp := settlement.NewBatchProcessor(settleSt, escrowSt, ledgerSt, hub, log)
		acctMgr := newAccountingManager(db, testAccountingCfg())

		sessionID := "sess-credit-drain"
		pricePerBlock := 10.0
		initialBalance := 100.0

		require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-cd", "idx-cd", initialBalance, pricePerBlock))
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-cd", "idx-cd", initialBalance, pricePerBlock))

		// 6 clean blocks drain to 100 - 60 = 40, which is below threshold 50.
		for i := 1; i <= 6; i++ {
			cids := cidJSON(fmt.Sprintf("cid-cd-%d", i))
			_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
				SessionID: sessionID, IndexerID: "idx-cd", BlockNumber: i,
				DocCids: cids, BlockHash: "0xcd",
			})
			require.NoError(t, err)
			_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
				SessionID: sessionID, HostID: "host-cd", BlockNumber: i,
				DocCidsReceived: cids,
			})
			require.NoError(t, err)
			_, err = acctMgr.Compare(ctx, sessionID, i)
			require.NoError(t, err)
		}

		require.NoError(t, em.DrainSession(ctx, sessionID))

		// Low credit signal should have fired.
		hub.mu.Lock()
		require.Len(t, hub.lowCreditCalls, 1)
		hub.mu.Unlock()

		escrow, err := escrowSt.GetBySession(ctx, sessionID)
		require.NoError(t, err)
		require.True(t, escrow.LowCreditSignaled)

		// Top up 200 — signal should clear.
		require.NoError(t, em.TopUp(ctx, sessionID, 200.0))

		escrow, err = escrowSt.GetBySession(ctx, sessionID)
		require.NoError(t, err)
		assert.False(t, escrow.LowCreditSignaled)
		assert.Empty(t, escrow.GracePeriodEndsAt)

		// 10 more clean blocks (7-16).
		for i := 7; i <= 16; i++ {
			cids := cidJSON(fmt.Sprintf("cid-cd-%d", i))
			_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
				SessionID: sessionID, IndexerID: "idx-cd", BlockNumber: i,
				DocCids: cids, BlockHash: "0xcd2",
			})
			require.NoError(t, err)
			_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
				SessionID: sessionID, HostID: "host-cd", BlockNumber: i,
				DocCidsReceived: cids,
			})
			require.NoError(t, err)
			_, err = acctMgr.Compare(ctx, sessionID, i)
			require.NoError(t, err)
		}

		// Batch settle: payment = 16 * 10 = 160, refund = (100+200) - 160 = 140.
		require.NoError(t, bp.ProcessBatch(ctx, []string{sessionID}, store.CloseReasonHostInitiated))

		hub.mu.Lock()
		require.Len(t, hub.batchCalls, 1)
		sess := hub.batchCalls[0].Sessions[0]
		assert.InDelta(t, 160.0, sess.IndexerAmount, 0.001)
		assert.InDelta(t, 140.0, sess.HostRefund, 0.001)
		hub.mu.Unlock()
	})

	t.Run("refund_after_indexer_silent", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		log := zap.NewNop().Sugar()
		hub := &recordingBroadcaster{}

		escrowSt := store.NewEscrowStore(db)
		ledgerSt := store.NewLedgerStore(db)
		settleSt := store.NewSettlementStore(db)

		cfg := testSettlementCfg()
		em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)
		bp := settlement.NewBatchProcessor(settleSt, escrowSt, ledgerSt, hub, log)

		acctCfg := testAccountingCfg()
		acctCfg.AttestationWindowSeconds = 0 // immediate comparison
		acctMgr := newAccountingManager(db, acctCfg)

		sessionID := "sess-idx-silent"
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-is", "idx-is", 100.0, 1.0))
		require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-is", "idx-is", 100.0, 1.0))

		// Only attestations, no claims → indexer_silent.
		for i := 1; i <= 3; i++ {
			cids := cidJSON(fmt.Sprintf("cid-is-%d", i))
			_, err := acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
				SessionID: sessionID, HostID: "host-is", BlockNumber: i,
				DocCidsReceived: cids,
			})
			require.NoError(t, err)
			result, err := acctMgr.Compare(ctx, sessionID, i)
			require.NoError(t, err)
			require.Equal(t, store.OutcomeIndexerSilent, result.Outcome)
		}

		// Ledger should show 0 verified.
		ledger, err := acctMgr.GetSessionLedger(ctx, sessionID)
		require.NoError(t, err)
		assert.Equal(t, 0, ledger.BlocksVerified)

		// Batch settle: host gets full refund.
		require.NoError(t, bp.ProcessBatch(ctx, []string{sessionID}, store.CloseReasonHostInitiated))

		hub.mu.Lock()
		require.Len(t, hub.batchCalls, 1)
		sess := hub.batchCalls[0].Sessions[0]
		assert.InDelta(t, 0.0, sess.IndexerAmount, 0.001)
		assert.InDelta(t, 100.0, sess.HostRefund, 0.001)
		hub.mu.Unlock()
	})

	t.Run("price_filter_and_diversity", func(t *testing.T) {
		h := newHarness(t)

		// Register 5 indexers with varying prices.
		prices := []float64{1, 5, 10, 50, 100}
		for i, p := range prices {
			pricingJSON, _ := json.Marshal(store.Pricing{TipPer1kBlocks: p})
			h.registerIndexerWithOpts(t, &store.IndexerRecord{
				PeerID:           fmt.Sprintf("idx-price-%d", i),
				HTTPUrl:          fmt.Sprintf("http://idx-price-%d:8080", i),
				Multiaddr:        fmt.Sprintf("/ip4/127.0.0.1/tcp/9171/p2p/idx-price-%d", i),
				CurrentTip:       500,
				Pricing:          string(pricingJSON),
				ReliabilityScore: 1.0,
			})
		}

		hostID := "host-price-div"
		hostKey := h.registerHost(t, hostID)

		// Query with max_tip_per_1k=15: only prices 1, 5, 10 pass.
		url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s&max_tip_per_1k=15", testChain, testNetwork)
		resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var matches []discovery.IndexerMatch
		decodeJSON(t, resp, &matches)
		assert.Equal(t, 3, len(matches), "only 3 indexers within budget")

		for _, m := range matches {
			assert.NotEqual(t, "idx-price-3", m.PeerID, "price=50 should be excluded")
			assert.NotEqual(t, "idx-price-4", m.PeerID, "price=100 should be excluded")
		}
	})
}

// Adversarial

func TestAdversarial(t *testing.T) {

	t.Run("content_addressing_fraud", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		acctMgr := newAccountingManager(db, testAccountingCfg())

		sessionID := "sess-caf"
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-caf", "idx-caf", 100.0, 1.0))

		// Submit claim with CIDs ["a","b"].
		_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-caf", BlockNumber: 5,
			DocCids: cidJSON("a", "b"), BlockHash: "0xcaf",
		})
		require.NoError(t, err)

		// Submit again with different CIDs — should be rejected as fraud.
		_, err = acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-caf", BlockNumber: 5,
			DocCids: cidJSON("x", "y"), BlockHash: "0xcaf",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "content-addressing fraud")

		// Submit matching attestation with original CIDs.
		_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
			SessionID: sessionID, HostID: "host-caf", BlockNumber: 5,
			DocCidsReceived: cidJSON("a", "b"),
		})
		require.NoError(t, err)

		result, err := acctMgr.Compare(ctx, sessionID, 5)
		require.NoError(t, err)
		assert.Equal(t, store.OutcomeClean, result.Outcome)
	})

	t.Run("mismatch_to_verdict_to_settlement", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		log := zap.NewNop().Sugar()
		hub := &recordingBroadcaster{}
		cfg := testSettlementCfg()
		cfg.VerdictThresholdM = 3

		escrowSt := store.NewEscrowStore(db)
		ledgerSt := store.NewLedgerStore(db)
		settleSt := store.NewSettlementStore(db)
		verdictSt := store.NewVerdictStore(db)

		em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)
		bp := settlement.NewBatchProcessor(settleSt, escrowSt, ledgerSt, hub, log)
		vm := settlement.NewVerdictManager(verdictSt, hub, cfg, log)
		acctMgr := newAccountingManager(db, testAccountingCfg())

		sessionID := "sess-mv"
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-mv", "idx-mv", 100.0, 1.0))
		require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-mv", "idx-mv", 100.0, 1.0))

		// Submit claim ["a"], attestation ["z"] → mismatch.
		_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-mv", BlockNumber: 1,
			DocCids: cidJSON("a"), BlockHash: "0xmv",
		})
		require.NoError(t, err)
		_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
			SessionID: sessionID, HostID: "host-mv", BlockNumber: 1,
			DocCidsReceived: cidJSON("z"),
		})
		require.NoError(t, err)
		result, err := acctMgr.Compare(ctx, sessionID, 1)
		require.NoError(t, err)
		assert.Equal(t, store.OutcomeMismatch, result.Outcome)

		// Create verdict, add 2 more signatures (total = 3 = M).
		_, err = vm.CreateVerdict(ctx, sessionID, store.OutcomeMismatch, []string{"evidence-cid-1"})
		require.NoError(t, err)
		require.NoError(t, vm.AddSignature(ctx, sessionID, "cosigner-2"))
		require.NoError(t, vm.AddSignature(ctx, sessionID, "cosigner-3"))

		hasQ, err := vm.HasQuorum(ctx, sessionID)
		require.NoError(t, err)
		require.True(t, hasQ)

		require.NoError(t, vm.SubmitToHub(ctx, sessionID))

		// Batch settle with 0 verified blocks → host gets full refund.
		require.NoError(t, bp.ProcessBatch(ctx, []string{sessionID}, store.CloseReasonDispute))

		hub.mu.Lock()
		require.Len(t, hub.batchCalls, 1)
		sess := hub.batchCalls[0].Sessions[0]
		assert.InDelta(t, 0.0, sess.IndexerAmount, 0.001)
		assert.InDelta(t, 100.0, sess.HostRefund, 0.001)
		hub.mu.Unlock()
	})

	t.Run("systematic_under_report_flagged", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		acctMgr := newAccountingManager(db, testAccountingCfg())

		sessionID := "sess-underrpt"
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-ur", "idx-ur", 100.0, 1.0))

		// 10 blocks: indexer claims 5 CIDs, host attests 3 → all under_report.
		for i := 1; i <= 10; i++ {
			_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
				SessionID: sessionID, IndexerID: "idx-ur", BlockNumber: i,
				DocCids:   cidJSON("c1", "c2", "c3", "c4", "c5"),
				BlockHash: fmt.Sprintf("0xur-%d", i),
			})
			require.NoError(t, err)
			_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
				SessionID:       sessionID,
				HostID:          "host-ur",
				BlockNumber:     i,
				DocCidsReceived: cidJSON("c1", "c2", "c3"),
			})
			require.NoError(t, err)
			result, err := acctMgr.Compare(ctx, sessionID, i)
			require.NoError(t, err)
			assert.Equal(t, store.OutcomeUnderReport, result.Outcome)
		}

		// Cross-check: 10 under-reports out of 10 = 100% > 30% threshold.
		isOutlier, err := acctMgr.CrossCheckHost(ctx, sessionID, "host-ur")
		require.NoError(t, err)
		assert.True(t, isOutlier, "100% under-report rate should flag outlier")
	})

	t.Run("under_report_escalation_on_mismatch", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		acctMgr := newAccountingManager(db, testAccountingCfg())

		sessionID := "sess-esc"
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-esc", "idx-esc", 100.0, 1.0))

		// Wire a mock escalation handler.
		mock := &mockEscalation{}
		acctMgr.WithEscalation(mock)

		// Submit a mismatch: claim ["a"], attest ["z"].
		_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-esc", BlockNumber: 1,
			DocCids: cidJSON("a"), BlockHash: "0xesc",
		})
		require.NoError(t, err)
		_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
			SessionID: sessionID, HostID: "host-esc", BlockNumber: 1,
			DocCidsReceived: cidJSON("z"),
		})
		require.NoError(t, err)

		result, err := acctMgr.Compare(ctx, sessionID, 1)
		require.NoError(t, err)
		assert.Equal(t, store.OutcomeMismatch, result.Outcome)

		// Verify OnMismatch was called synchronously.
		mock.mu.Lock()
		assert.Equal(t, 1, mock.mismatchCount, "OnMismatch should be called once")
		assert.Equal(t, sessionID, mock.lastSession)
		mock.mu.Unlock()
	})

	t.Run("host_silent_indexer_held_harmless", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		acctCfg := testAccountingCfg()
		acctCfg.AttestationWindowSeconds = 0
		acctMgr := newAccountingManager(db, acctCfg)

		sessionID := "sess-hs"
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-hs", "idx-hs", 100.0, 1.0))

		// Claims only, no attestations → host_silent.
		for i := 1; i <= 3; i++ {
			_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
				SessionID: sessionID, IndexerID: "idx-hs", BlockNumber: i,
				DocCids: cidJSON(fmt.Sprintf("cid-hs-%d", i)), BlockHash: "0xhs",
			})
			require.NoError(t, err)
			result, err := acctMgr.Compare(ctx, sessionID, i)
			require.NoError(t, err)
			assert.Equal(t, store.OutcomeHostSilent, result.Outcome)
		}

		ledger, err := acctMgr.GetSessionLedger(ctx, sessionID)
		require.NoError(t, err)
		assert.Equal(t, 0, ledger.BlocksVerified)
	})
}

// Market Dynamics

func TestMarketDynamics(t *testing.T) {

	t.Run("clearing_price_reliability_tiers", func(t *testing.T) {
		h := newHarness(t)

		reliabilities := []float64{1.0, 0.5, 0.1}
		for i, r := range reliabilities {
			pricingJSON, _ := json.Marshal(store.Pricing{TipPer1kBlocks: 10.0})
			h.registerIndexerWithOpts(t, &store.IndexerRecord{
				PeerID:           fmt.Sprintf("idx-rel-%d", i),
				HTTPUrl:          fmt.Sprintf("http://idx-rel-%d:8080", i),
				Multiaddr:        fmt.Sprintf("/ip4/127.0.0.1/tcp/9171/p2p/idx-rel-%d", i),
				CurrentTip:       500,
				Pricing:          string(pricingJSON),
				ReliabilityScore: r,
			})
		}

		hostKey := h.registerHost(t, "host-rel")

		url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s&max_tip_per_1k=20", testChain, testNetwork)
		resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var matches []discovery.IndexerMatch
		decodeJSON(t, resp, &matches)
		require.Len(t, matches, 3)

		// Should be sorted by reliability DESC.
		assert.Equal(t, "idx-rel-0", matches[0].PeerID) // reliability 1.0
		assert.Equal(t, "idx-rel-1", matches[1].PeerID) // reliability 0.5
		assert.Equal(t, "idx-rel-2", matches[2].PeerID) // reliability 0.1

		// Verify clearing prices follow formula: (ask*reliability + bid) / (1+reliability).
		// ask=10, bid=20 for each.
		for i, r := range reliabilities {
			expected := (10.0*r + 20.0) / (1.0 + r)
			assert.InDelta(t, expected, matches[i].ClearingPrice, 0.01,
				"clearing price mismatch for reliability=%.1f", r)
		}
	})

	t.Run("diversity_with_liquidity_preservation", func(t *testing.T) {
		h := newHarness(t)
		ctx := context.Background()

		// Register 3 indexers + host.
		for i := 0; i < 3; i++ {
			h.registerIndexerWithOpts(t, &store.IndexerRecord{
				PeerID:           fmt.Sprintf("idx-div-%d", i),
				HTTPUrl:          fmt.Sprintf("http://idx-div-%d:8080", i),
				Multiaddr:        fmt.Sprintf("/ip4/127.0.0.1/tcp/9171/p2p/idx-div-%d", i),
				CurrentTip:       500,
				ReliabilityScore: 1.0,
			})
		}
		hostID := "host-div"
		hostKey := h.registerHost(t, hostID)

		// Create match history between host and indexer A.
		_, err := h.matchSt.Create(ctx, &store.MatchHistoryRecord{
			MatchID:       uuid.New().String(),
			HostID:        hostID,
			IndexerID:     "idx-div-0",
			MatchType:     "tip",
			MatchedAt:     time.Now().UTC().Format(time.RFC3339),
			ClearingPrice: 5.0,
		})
		require.NoError(t, err)

		// Query with host_id — pool=3 ≤ 3 so liquidity preservation kicks in.
		url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s&host_id=%s", testChain, testNetwork, hostID)
		resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var matches3 []discovery.IndexerMatch
		decodeJSON(t, resp, &matches3)

		// With pool ≤ 3, idx-div-0 should NOT be penalized.
		for _, m := range matches3 {
			if m.PeerID == "idx-div-0" {
				assert.Equal(t, 1.0, m.ReliabilityScore,
					"diversity should not penalize when pool ≤ 3")
			}
		}

		// Register 2 more indexers (pool = 5).
		for i := 3; i < 5; i++ {
			h.registerIndexerWithOpts(t, &store.IndexerRecord{
				PeerID:           fmt.Sprintf("idx-div-%d", i),
				HTTPUrl:          fmt.Sprintf("http://idx-div-%d:8080", i),
				Multiaddr:        fmt.Sprintf("/ip4/127.0.0.1/tcp/9171/p2p/idx-div-%d", i),
				CurrentTip:       500,
				ReliabilityScore: 1.0,
			})
		}

		// Query again — pool=5 > 3, so idx-div-0 should be deprioritized.
		resp2 := h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp2.StatusCode)
		var matches5 []discovery.IndexerMatch
		decodeJSON(t, resp2, &matches5)

		for _, m := range matches5 {
			if m.PeerID == "idx-div-0" {
				assert.Less(t, m.ReliabilityScore, 1.0,
					"recently matched indexer should be deprioritized when pool > 3")
			}
		}
	})

	t.Run("tip_lag_boundary_precision", func(t *testing.T) {
		h := newHarness(t)

		// Register indexers at tips: 1000, 990, 960, 950, 940.
		// Reference tip = 1000, exclusion threshold = 50.
		// 1000-940=60 > 50 → excluded. 1000-950=50, NOT > 50 → included.
		tips := []int{1000, 990, 960, 950, 940}
		for i, tip := range tips {
			h.registerIndexerWithOpts(t, &store.IndexerRecord{
				PeerID:           fmt.Sprintf("idx-lag-%d", i),
				HTTPUrl:          fmt.Sprintf("http://idx-lag-%d:8080", i),
				Multiaddr:        fmt.Sprintf("/ip4/127.0.0.1/tcp/9171/p2p/idx-lag-%d", i),
				CurrentTip:       tip,
				ReliabilityScore: 1.0,
			})
		}

		hostKey := h.registerHost(t, "host-lag")

		url := fmt.Sprintf("/v1/discover/indexers?chain=%s&network=%s", testChain, testNetwork)
		resp := h.doRequest(t, http.MethodGet, url, nil, hostKey)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var matches []discovery.IndexerMatch
		decodeJSON(t, resp, &matches)

		// idx-lag-4 (tip=940, lag=60) should be excluded.
		// idx-lag-3 (tip=950, lag=50) should be included (> threshold, not >=).
		assert.Equal(t, 4, len(matches), "exactly 4 indexers should pass tip lag filter")

		for _, m := range matches {
			assert.NotEqual(t, "idx-lag-4", m.PeerID, "indexer at tip=940 (lag=60) should be excluded")
		}

		// Verify idx-lag-3 (tip=950) is present.
		found950 := false
		for _, m := range matches {
			if m.PeerID == "idx-lag-3" {
				found950 = true
			}
		}
		assert.True(t, found950, "indexer at tip=950 (lag=50) should be included")
	})
}

// Operational

func TestOperational(t *testing.T) {

	t.Run("multi_session_batch_atomicity", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		log := zap.NewNop().Sugar()
		hub := &recordingBroadcaster{}

		escrowSt := store.NewEscrowStore(db)
		ledgerSt := store.NewLedgerStore(db)
		settleSt := store.NewSettlementStore(db)
		cfg := testSettlementCfg()

		em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)
		bp := settlement.NewBatchProcessor(settleSt, escrowSt, ledgerSt, hub, log)
		acctMgr := newAccountingManager(db, testAccountingCfg())

		// Create 3 sessions with 5, 10, 15 verified blocks.
		blockCounts := []int{5, 10, 15}
		sessionIDs := make([]string, 3)
		pricePerBlock := 2.0

		for s, bc := range blockCounts {
			sid := fmt.Sprintf("sess-batch-%d", s)
			sessionIDs[s] = sid
			require.NoError(t, acctMgr.CreateSessionLedger(ctx, sid, "host-mb", "idx-mb", 200.0, pricePerBlock))
			require.NoError(t, em.CreateEscrow(ctx, sid, "host-mb", "idx-mb", 200.0, pricePerBlock))

			for i := 1; i <= bc; i++ {
				cids := cidJSON(fmt.Sprintf("cid-%s-%d", sid, i))
				_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
					SessionID: sid, IndexerID: "idx-mb", BlockNumber: i,
					DocCids: cids, BlockHash: fmt.Sprintf("0x%s-%d", sid, i),
				})
				require.NoError(t, err)
				_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
					SessionID: sid, HostID: "host-mb", BlockNumber: i,
					DocCidsReceived: cids,
				})
				require.NoError(t, err)
				_, err = acctMgr.Compare(ctx, sid, i)
				require.NoError(t, err)
			}
		}

		require.NoError(t, bp.ProcessBatch(ctx, sessionIDs, store.CloseReasonHostInitiated))

		// Single batch broadcast with 3 items.
		hub.mu.Lock()
		require.Len(t, hub.batchCalls, 1)
		require.Len(t, hub.batchCalls[0].Sessions, 3)
		hub.mu.Unlock()

		// All 3 escrows settled with correct amounts.
		for s, bc := range blockCounts {
			escrow, err := escrowSt.GetBySession(ctx, sessionIDs[s])
			require.NoError(t, err)
			assert.Equal(t, "settled", escrow.Status)

			recs, err := settleSt.ListBySession(ctx, sessionIDs[s])
			require.NoError(t, err)
			require.Len(t, recs, 1)
			expectedAmount := float64(bc) * pricePerBlock
			assert.InDelta(t, expectedAmount, recs[0].IndexerAmount, 0.001)
			assert.InDelta(t, 200.0-expectedAmount, recs[0].HostRefund, 0.001)
		}
	})

	t.Run("grace_period_expiry", func(t *testing.T) {
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
		acctMgr := newAccountingManager(db, testAccountingCfg())

		sessionID := "sess-grace"
		pricePerBlock := 10.0
		require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-gr", "idx-gr", 100.0, pricePerBlock))
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-gr", "idx-gr", 100.0, pricePerBlock))

		// 6 blocks → balance = 40, triggers low credit (threshold=50).
		for i := 1; i <= 6; i++ {
			cids := cidJSON(fmt.Sprintf("cid-gr-%d", i))
			_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
				SessionID: sessionID, IndexerID: "idx-gr", BlockNumber: i,
				DocCids: cids, BlockHash: "0xgr",
			})
			require.NoError(t, err)
			_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
				SessionID: sessionID, HostID: "host-gr", BlockNumber: i,
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

		// Move grace period end to the past.
		pastTime := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		require.NoError(t, escrowSt.Update(ctx, escrow.DocID, map[string]any{
			"gracePeriodEndsAt": pastTime,
		}))

		// Drain again — should mark as expired.
		require.NoError(t, em.DrainSession(ctx, sessionID))

		escrow, err = escrowSt.GetBySession(ctx, sessionID)
		require.NoError(t, err)
		assert.Equal(t, store.StatusExpired, escrow.Status)
	})

	t.Run("credit_exhaustion", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		log := zap.NewNop().Sugar()
		hub := &recordingBroadcaster{}
		cfg := testSettlementCfg()

		escrowSt := store.NewEscrowStore(db)
		ledgerSt := store.NewLedgerStore(db)
		settleSt := store.NewSettlementStore(db)

		em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)
		bp := settlement.NewBatchProcessor(settleSt, escrowSt, ledgerSt, hub, log)
		acctMgr := newAccountingManager(db, testAccountingCfg())

		sessionID := "sess-exhaust"
		pricePerBlock := 10.0
		initialBalance := 20.0

		require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-ex", "idx-ex", initialBalance, pricePerBlock))
		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-ex", "idx-ex", initialBalance, pricePerBlock))

		// 2 clean blocks → balance = 0.
		for i := 1; i <= 2; i++ {
			cids := cidJSON(fmt.Sprintf("cid-ex-%d", i))
			_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
				SessionID: sessionID, IndexerID: "idx-ex", BlockNumber: i,
				DocCids: cids, BlockHash: "0xex",
			})
			require.NoError(t, err)
			_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
				SessionID: sessionID, HostID: "host-ex", BlockNumber: i,
				DocCidsReceived: cids,
			})
			require.NoError(t, err)
			_, err = acctMgr.Compare(ctx, sessionID, i)
			require.NoError(t, err)
		}

		// Drain → balance = 0 → expired.
		require.NoError(t, em.DrainSession(ctx, sessionID))

		escrow, err := escrowSt.GetBySession(ctx, sessionID)
		require.NoError(t, err)
		assert.Equal(t, store.StatusExpired, escrow.Status)
		assert.InDelta(t, 0.0, escrow.CurrentBalance, 0.001)

		// Batch settle: indexer=20, host refund=0.
		require.NoError(t, bp.ProcessBatch(ctx, []string{sessionID}, store.CloseReasonCreditExhaustion))

		hub.mu.Lock()
		require.Len(t, hub.batchCalls, 1)
		sess := hub.batchCalls[0].Sessions[0]
		assert.InDelta(t, 20.0, sess.IndexerAmount, 0.001)
		assert.InDelta(t, 0.0, sess.HostRefund, 0.001)
		hub.mu.Unlock()
	})

	t.Run("mixed_outcomes_end_to_end", func(t *testing.T) {
		db, cleanup := setupDefra(t)
		defer cleanup()

		ctx := context.Background()
		log := zap.NewNop().Sugar()
		hub := &recordingBroadcaster{}
		cfg := testSettlementCfg()

		escrowSt := store.NewEscrowStore(db)
		ledgerSt := store.NewLedgerStore(db)
		settleSt := store.NewSettlementStore(db)

		em := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, cfg, log)
		bp := settlement.NewBatchProcessor(settleSt, escrowSt, ledgerSt, hub, log)

		acctCfg := testAccountingCfg()
		acctCfg.AttestationWindowSeconds = 0 // immediate for silent party detection
		acctMgr := newAccountingManager(db, acctCfg)

		sessionID := "sess-mixed"
		pricePerBlock := 1.0
		initialEscrow := 100.0

		require.NoError(t, acctMgr.CreateSessionLedger(ctx, sessionID, "host-mx", "idx-mx", initialEscrow, pricePerBlock))
		require.NoError(t, em.CreateEscrow(ctx, sessionID, "host-mx", "idx-mx", initialEscrow, pricePerBlock))

		// Blocks 1-3: clean delivery.
		for i := 1; i <= 3; i++ {
			cids := cidJSON(fmt.Sprintf("clean-%d", i))
			result := submitBlockDirect(t, ctx, acctMgr, sessionID, "idx-mx", "host-mx", i, cids, cids)
			assert.Equal(t, store.OutcomeClean, result.Outcome)
		}

		// Block 4: under-report (claim 3 CIDs, attest 2).
		result := submitBlockDirect(t, ctx, acctMgr, sessionID, "idx-mx", "host-mx", 4,
			cidJSON("u1", "u2", "u3"), cidJSON("u1", "u2"))
		assert.Equal(t, store.OutcomeUnderReport, result.Outcome)

		// Block 5: mismatch (completely different CIDs).
		result = submitBlockDirect(t, ctx, acctMgr, sessionID, "idx-mx", "host-mx", 5,
			cidJSON("claim-x"), cidJSON("attest-y"))
		assert.Equal(t, store.OutcomeMismatch, result.Outcome)

		// Block 6: host_silent (claim only).
		_, err := acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
			SessionID: sessionID, IndexerID: "idx-mx", BlockNumber: 6,
			DocCids: cidJSON("hs-cid"), BlockHash: "0xmx-6",
		})
		require.NoError(t, err)
		result, err = acctMgr.Compare(ctx, sessionID, 6)
		require.NoError(t, err)
		assert.Equal(t, store.OutcomeHostSilent, result.Outcome)

		// Block 7: indexer_silent (attest only).
		_, err = acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
			SessionID: sessionID, HostID: "host-mx", BlockNumber: 7,
			DocCidsReceived: cidJSON("is-cid"),
		})
		require.NoError(t, err)
		result, err = acctMgr.Compare(ctx, sessionID, 7)
		require.NoError(t, err)
		assert.Equal(t, store.OutcomeIndexerSilent, result.Outcome)

		// Blocks 8-10: clean again.
		for i := 8; i <= 10; i++ {
			cids := cidJSON(fmt.Sprintf("clean2-%d", i))
			result = submitBlockDirect(t, ctx, acctMgr, sessionID, "idx-mx", "host-mx", i, cids, cids)
			assert.Equal(t, store.OutcomeClean, result.Outcome)
		}

		// Verify ledger: 6 clean deliveries (blocks 1-3 + 8-10).
		ledger, err := acctMgr.GetSessionLedger(ctx, sessionID)
		require.NoError(t, err)
		assert.Equal(t, 6, ledger.BlocksVerified)

		// Cross-check host: 1 under-report out of 10 = 10% < 30%.
		isOutlier, err := acctMgr.CrossCheckHost(ctx, sessionID, "host-mx")
		require.NoError(t, err)
		assert.False(t, isOutlier, "10% under-report rate should not flag outlier")

		// Drain escrow: balance = 100 - 6*1 = 94.
		require.NoError(t, em.DrainSession(ctx, sessionID))

		escrow, err := escrowSt.GetBySession(ctx, sessionID)
		require.NoError(t, err)
		assert.InDelta(t, 94.0, escrow.CurrentBalance, 0.001)

		// Batch settle: indexer=6, host refund=94.
		require.NoError(t, bp.ProcessBatch(ctx, []string{sessionID}, store.CloseReasonHostInitiated))

		hub.mu.Lock()
		require.Len(t, hub.batchCalls, 1)
		sess := hub.batchCalls[0].Sessions[0]
		assert.InDelta(t, 6.0, sess.IndexerAmount, 0.001)
		assert.InDelta(t, 94.0, sess.HostRefund, 0.001)
		hub.mu.Unlock()
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mockEscalation records escalation calls for assertions.
type mockEscalation struct {
	mu            sync.Mutex
	mismatchCount int
	underRptCount int
	lastSession   string
}

func (m *mockEscalation) OnMismatch(_ context.Context, sessionID string, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mismatchCount++
	m.lastSession = sessionID
	return nil
}

func (m *mockEscalation) OnUnderReportExpired(_ context.Context, sessionID string, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.underRptCount++
	m.lastSession = sessionID
	return nil
}

// submitBlockDirect submits a claim + attestation with specified CIDs and runs comparison.
func submitBlockDirect(t *testing.T, ctx context.Context, mgr *accounting.Manager, sessionID, indexerID, hostID string, blockN int, claimCids, attestCids string) *accounting.ComparisonResult {
	t.Helper()
	_, err := mgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
		SessionID: sessionID, IndexerID: indexerID, BlockNumber: blockN,
		DocCids: claimCids, BlockHash: fmt.Sprintf("0x%s-%d", sessionID, blockN),
	})
	require.NoError(t, err)
	_, err = mgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
		SessionID: sessionID, HostID: hostID, BlockNumber: blockN,
		DocCidsReceived: attestCids,
	})
	require.NoError(t, err)
	result, err := mgr.Compare(ctx, sessionID, blockN)
	require.NoError(t, err)
	return result
}
