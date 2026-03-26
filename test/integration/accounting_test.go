//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/accounting"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/require"
)

// TestAccountingFlow_CleanDelivery exercises the full happy path:
// ledger creation, claim + attestation with matching CIDs, comparison, ledger update.
func TestAccountingFlow_CleanDelivery(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	cfg := testAccountingCfg()
	mgr := newAccountingManager(db, cfg)

	sessionID := "sess-clean-1"
	initialEscrow := 100.0
	pricePerBlock := 1.0

	require.NoError(t, mgr.CreateSessionLedger(ctx, sessionID, "host-1", "idx-1", initialEscrow, pricePerBlock))

	cids := cidJSON("cid-a", "cid-b")
	_, err := mgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
		SessionID:   sessionID,
		IndexerID:   "idx-1",
		BlockNumber: 1,
		DocCids:     cids,
		BlockHash:   "0xabc",
	})
	require.NoError(t, err)

	_, err = mgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
		SessionID:       sessionID,
		HostID:          "host-1",
		BlockNumber:     1,
		DocCidsReceived: cids,
	})
	require.NoError(t, err)

	result, err := mgr.Compare(ctx, sessionID, 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, store.OutcomeClean, result.Outcome)

	ledger, err := mgr.GetSessionLedger(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, 1, ledger.BlocksVerified)
	require.InDelta(t, initialEscrow-pricePerBlock, ledger.CreditRemaining, 0.001)
}

// TestAccountingFlow_UnderReport verifies that fewer attested CIDs produce an under_report outcome.
func TestAccountingFlow_UnderReport(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	mgr := newAccountingManager(db, testAccountingCfg())

	sessionID := "sess-under-1"
	require.NoError(t, mgr.CreateSessionLedger(ctx, sessionID, "host-1", "idx-1", 100, 1))

	claimCids := cidJSON("cid-1", "cid-2", "cid-3")
	attestCids := cidJSON("cid-1", "cid-2") // only 2 of 3

	_, err := mgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
		SessionID:   sessionID,
		IndexerID:   "idx-1",
		BlockNumber: 1,
		DocCids:     claimCids,
		BlockHash:   "0xdef",
	})
	require.NoError(t, err)

	_, err = mgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
		SessionID:       sessionID,
		HostID:          "host-1",
		BlockNumber:     1,
		DocCidsReceived: attestCids,
	})
	require.NoError(t, err)

	result, err := mgr.Compare(ctx, sessionID, 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, store.OutcomeUnderReport, result.Outcome)

	ledger, err := mgr.GetSessionLedger(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, 0, ledger.BlocksVerified)
}

// TestAccountingFlow_CIDMismatch verifies that divergent CIDs trigger a mismatch outcome.
func TestAccountingFlow_CIDMismatch(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	mgr := newAccountingManager(db, testAccountingCfg())

	sessionID := "sess-mismatch-1"
	require.NoError(t, mgr.CreateSessionLedger(ctx, sessionID, "host-1", "idx-1", 100, 1))

	_, err := mgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
		SessionID:   sessionID,
		IndexerID:   "idx-1",
		BlockNumber: 1,
		DocCids:     cidJSON("cid-x", "cid-y"),
		BlockHash:   "0x111",
	})
	require.NoError(t, err)

	_, err = mgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
		SessionID:       sessionID,
		HostID:          "host-1",
		BlockNumber:     1,
		DocCidsReceived: cidJSON("cid-a", "cid-b"),
	})
	require.NoError(t, err)

	result, err := mgr.Compare(ctx, sessionID, 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, store.OutcomeMismatch, result.Outcome)

	ledger, err := mgr.GetSessionLedger(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, 0, ledger.BlocksVerified)
}

// TestAccountingFlow_DuplicateClaimRejected submits the same (session, block) with
// different CIDs and expects a content-addressing fraud error.
func TestAccountingFlow_DuplicateClaimRejected(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	mgr := newAccountingManager(db, testAccountingCfg())

	sessionID := "sess-dup-1"

	_, err := mgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
		SessionID:   sessionID,
		IndexerID:   "idx-1",
		BlockNumber: 5,
		DocCids:     cidJSON("cid-original"),
		BlockHash:   "0xfff",
	})
	require.NoError(t, err)

	_, err = mgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
		SessionID:   sessionID,
		IndexerID:   "idx-1",
		BlockNumber: 5,
		DocCids:     cidJSON("cid-tampered"),
		BlockHash:   "0xfff",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "content-addressing fraud")
}

// TestAccountingFlow_AttestationAppendOnly submits the same (session, block) attestation
// twice and expects the second to be rejected.
func TestAccountingFlow_AttestationAppendOnly(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	mgr := newAccountingManager(db, testAccountingCfg())

	sessionID := "sess-append-1"

	_, err := mgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
		SessionID:       sessionID,
		HostID:          "host-1",
		BlockNumber:     3,
		DocCidsReceived: cidJSON("cid-z"),
	})
	require.NoError(t, err)

	_, err = mgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
		SessionID:       sessionID,
		HostID:          "host-1",
		BlockNumber:     3,
		DocCidsReceived: cidJSON("cid-z"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "append-only")
}

// TestAccountingFlow_ComparisonHeldUntilBothPresent verifies that a comparison
// returns nil when only the claim exists (attestation not yet submitted).
func TestAccountingFlow_ComparisonHeldUntilBothPresent(t *testing.T) {
	db, cleanup := setupDefra(t)
	defer cleanup()

	ctx := context.Background()
	cfg := testAccountingCfg()
	cfg.AttestationWindowSeconds = 3600
	mgr := newAccountingManager(db, cfg)

	sessionID := "sess-held-1"

	_, err := mgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
		SessionID:   sessionID,
		IndexerID:   "idx-1",
		BlockNumber: 10,
		DocCids:     cidJSON("cid-wait"),
		BlockHash:   "0x999",
	})
	require.NoError(t, err)

	result, err := mgr.Compare(ctx, sessionID, 10)
	require.NoError(t, err)
	require.Nil(t, result)
}
