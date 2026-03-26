package settlement

import (
	"context"
	"fmt"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock settlement store ---

type mockSettlementStore struct {
	records   map[string]*store.SettlementRecord // keyed by settlementID
	nextDoc   int
	err       error
	updateErr error
}

func newMockSettlementStore() *mockSettlementStore {
	return &mockSettlementStore{records: make(map[string]*store.SettlementRecord)}
}

func (m *mockSettlementStore) Create(_ context.Context, r *store.SettlementRecord) (*store.SettlementRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("sdoc-%d", m.nextDoc)
	cp := *r
	m.records[r.SettlementID] = &cp
	return &cp, nil
}

func (m *mockSettlementStore) ListByBatch(_ context.Context, batchID string) ([]store.SettlementRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []store.SettlementRecord
	for _, r := range m.records {
		if r.BatchID == batchID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockSettlementStore) Update(_ context.Context, docID string, fields map[string]any) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for _, r := range m.records {
		if r.DocID == docID {
			if v, ok := fields["status"]; ok {
				r.Status = v.(string)
			}
			if v, ok := fields["txHash"]; ok {
				r.TxHash = v.(string)
			}
		}
	}
	return nil
}

func seedEscrowAndLedger(es *mockEscrowStore, lr *mockLedgerReader, sid string, initialBalance float64, blocksVerified int, pricePerBlock float64) {
	es.records[sid] = &store.EscrowAccountRecord{
		DocID:          "edoc-" + sid,
		EscrowID:       "esc-" + sid,
		SessionID:      sid,
		HostID:         "host-1",
		IndexerID:      "idx-1",
		InitialBalance: initialBalance,
		CurrentBalance: initialBalance - float64(blocksVerified)*pricePerBlock,
		PricePerBlock:  pricePerBlock,
		Status:         store.StatusActive,
	}
	lr.records[sid] = &store.SessionLedgerRecord{
		SessionID:      sid,
		BlocksVerified: blocksVerified,
		PricePerBlock:  pricePerBlock,
		InitialEscrow:  initialBalance,
	}
}

func TestProcessBatch_Success(t *testing.T) {
	// Records created and hub broadcast called.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	seedEscrowAndLedger(es, lr, "s1", 100.0, 50, 1.0)
	seedEscrowAndLedger(es, lr, "s2", 200.0, 80, 2.0)

	err := bp.ProcessBatch(context.Background(), []string{"s1", "s2"}, "host_initiated")
	require.NoError(t, err)

	assert.Equal(t, 1, hub.batchCalls)
	assert.Len(t, ss.records, 2)

	// Verify each settlement record has txHash and settled status.
	for _, rec := range ss.records {
		assert.Equal(t, "tx-batch-abc", rec.TxHash)
		assert.Equal(t, store.StatusActive, rec.Status) // "active" means settled
	}
}

func TestProcessBatch_FinalPayment(t *testing.T) {
	// Final payment = blocks_verified * price_per_block.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	seedEscrowAndLedger(es, lr, "s1", 500.0, 200, 1.5)

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	require.NoError(t, err)

	// indexerAmount = 200 * 1.5 = 300.0
	var found bool
	for _, rec := range ss.records {
		if rec.SessionID == "s1" {
			assert.Equal(t, 300.0, rec.IndexerAmount)
			assert.Equal(t, 200, rec.BlocksVerified)
			found = true
		}
	}
	assert.True(t, found)
}

func TestProcessBatch_HostRefund(t *testing.T) {
	// Host refund = initial_escrow - indexer_amount.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	// initial=500, verified=100 blocks at 1.0 => indexer=100, refund=400
	seedEscrowAndLedger(es, lr, "s1", 500.0, 100, 1.0)

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	require.NoError(t, err)

	for _, rec := range ss.records {
		if rec.SessionID == "s1" {
			assert.Equal(t, 100.0, rec.IndexerAmount)
			assert.Equal(t, 400.0, rec.HostRefund)
		}
	}
}

func TestProcessBatch_HostRefundClampedToZero(t *testing.T) {
	// When indexer consumed more than initial balance, refund is 0.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	// initial=50, verified=100 blocks at 1.0 => indexer=100 > 50, refund clamped to 0
	seedEscrowAndLedger(es, lr, "s1", 50.0, 100, 1.0)

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "credit_exhaustion")
	require.NoError(t, err)

	for _, rec := range ss.records {
		if rec.SessionID == "s1" {
			assert.Equal(t, 0.0, rec.HostRefund)
		}
	}
}

func TestProcessBatch_BroadcastFailure(t *testing.T) {
	// Broadcast failure reverts all (no records created).
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{batchErr: fmt.Errorf("chain unreachable")}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	seedEscrowAndLedger(es, lr, "s1", 100.0, 50, 1.0)

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all reverted")

	// No settlement records should exist.
	assert.Empty(t, ss.records)
}

func TestProcessBatch_EmptySessionIDs(t *testing.T) {
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, newMockEscrowStore(), newMockLedgerReader(), hub, testLogger())

	err := bp.ProcessBatch(context.Background(), []string{}, "host_initiated")
	require.NoError(t, err)

	// No broadcast, no records.
	assert.Equal(t, 0, hub.batchCalls)
	assert.Empty(t, ss.records)
}

func TestProcessBatch_NilSessionIDs(t *testing.T) {
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, newMockEscrowStore(), newMockLedgerReader(), hub, testLogger())

	err := bp.ProcessBatch(context.Background(), nil, "host_initiated")
	require.NoError(t, err)
	assert.Equal(t, 0, hub.batchCalls)
}

func TestProcessBatch_EscrowNotFound(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	// No escrow seeded for "s1".
	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "batch aborted")
}

func TestProcessBatch_LedgerNotFound(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	es.records["s1"] = &store.EscrowAccountRecord{
		DocID:     "edoc-s1",
		SessionID: "s1",
		Status:    store.StatusActive,
	}
	// No ledger record for "s1".

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no ledger")
}

func TestProcessBatch_LedgerStoreError(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	lr.err = fmt.Errorf("ledger db failure")
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	es.records["s1"] = &store.EscrowAccountRecord{
		DocID:     "edoc-s1",
		SessionID: "s1",
		Status:    store.StatusActive,
	}

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ledger lookup")
}

func TestProcessBatch_EscrowStoreError(t *testing.T) {
	es := newMockEscrowStore()
	es.err = fmt.Errorf("store failure")
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, newMockLedgerReader(), hub, testLogger())

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escrow lookup")
}

func TestProcessBatch_StoreCreateErrorAfterBroadcast(t *testing.T) {
	// Broadcast succeeds but settlement store Create fails. The batch still
	// completes (errors are logged, not propagated) since the on-chain tx
	// already committed.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	seedEscrowAndLedger(es, lr, "s1", 100.0, 50, 1.0)

	// Make settlement store fail on Create after broadcast.
	ss.err = fmt.Errorf("settlement store write failed")

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	require.NoError(t, err)

	// Broadcast was called, but no records persisted due to store error.
	assert.Equal(t, 1, hub.batchCalls)
	assert.Empty(t, ss.records)
}

// escrowDeletingHub wraps mockHub and deletes escrow records after broadcast.
type escrowDeletingHub struct {
	inner *mockHub
	es    *mockEscrowStore
}

func (h *escrowDeletingHub) BroadcastCloseSession(ctx context.Context, msg MsgCloseSession) (string, error) {
	return h.inner.BroadcastCloseSession(ctx, msg)
}

func (h *escrowDeletingHub) BroadcastBatchSettlement(ctx context.Context, msg MsgBatchSettlement) (string, error) {
	tx, err := h.inner.BroadcastBatchSettlement(ctx, msg)
	if err == nil {
		// Simulate escrow disappearing after broadcast.
		for k := range h.es.records {
			delete(h.es.records, k)
		}
	}
	return tx, err
}

func (h *escrowDeletingHub) BroadcastLowCredit(ctx context.Context, msg MsgSignalLowCredit) (string, error) {
	return h.inner.BroadcastLowCredit(ctx, msg)
}

func (h *escrowDeletingHub) BroadcastSlash(ctx context.Context, msg MsgSlash) (string, error) {
	return h.inner.BroadcastSlash(ctx, msg)
}

func TestProcessBatch_EscrowGoneAfterBroadcast(t *testing.T) {
	// Escrow exists during validation but is gone by the post-broadcast update loop.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	// Custom hub that deletes escrow records after broadcast.
	hub := &mockHub{}
	hub.batchErr = nil

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	seedEscrowAndLedger(es, lr, "s1", 100.0, 50, 1.0)

	// Override hub to delete escrow after broadcast succeeds.
	deletingHub := &escrowDeletingHub{inner: hub, es: es}
	bp.hub = deletingHub

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	require.NoError(t, err)
	assert.Equal(t, 1, hub.batchCalls)
}

func TestProcessBatch_EscrowUpdateErrorAfterBroadcast(t *testing.T) {
	// Broadcast and settlement store succeed, but escrow update fails.
	// Should still complete without error (update failure is silently ignored).
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	seedEscrowAndLedger(es, lr, "s1", 100.0, 50, 1.0)

	// Escrow update will fail, but Create still works (err only affects GetBySession).
	es.updateErr = fmt.Errorf("escrow update failed")

	err := bp.ProcessBatch(context.Background(), []string{"s1"}, "host_initiated")
	require.NoError(t, err)
	assert.Equal(t, 1, hub.batchCalls)
	assert.Len(t, ss.records, 1)
}

func TestProcessBatch_MultipleSessions(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())

	seedEscrowAndLedger(es, lr, "s1", 100.0, 10, 2.0)
	seedEscrowAndLedger(es, lr, "s2", 200.0, 50, 1.0)
	seedEscrowAndLedger(es, lr, "s3", 300.0, 30, 3.0)

	err := bp.ProcessBatch(context.Background(), []string{"s1", "s2", "s3"}, "expired")
	require.NoError(t, err)

	assert.Len(t, ss.records, 3)
	assert.Equal(t, 1, hub.batchCalls)

	// Verify correct amounts for each session.
	for _, rec := range ss.records {
		switch rec.SessionID {
		case "s1":
			assert.Equal(t, 20.0, rec.IndexerAmount) // 10 * 2.0
			assert.Equal(t, 80.0, rec.HostRefund)    // 100 - 20
		case "s2":
			assert.Equal(t, 50.0, rec.IndexerAmount) // 50 * 1.0
			assert.Equal(t, 150.0, rec.HostRefund)   // 200 - 50
		case "s3":
			assert.Equal(t, 90.0, rec.IndexerAmount) // 30 * 3.0
			assert.Equal(t, 210.0, rec.HostRefund)   // 300 - 90
		}
	}
}
