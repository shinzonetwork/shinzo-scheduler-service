package settlement

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloseSession_Success(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())
	sc := NewSessionCloser(bp, hub, testLogger())

	seedEscrowAndLedger(es, lr, "s1", 100.0, 20, 1.0)

	err := sc.CloseSession(context.Background(), "s1", "host_initiated")
	require.NoError(t, err)

	assert.Equal(t, 1, hub.batchCalls)
	assert.Len(t, ss.records, 1)
}

func TestCloseSession_EmptySessionID(t *testing.T) {
	bp := NewBatchProcessor(newMockSettlementStore(), newMockEscrowStore(), newMockLedgerReader(), &mockHub{}, testLogger())
	sc := NewSessionCloser(bp, &mockHub{}, testLogger())

	err := sc.CloseSession(context.Background(), "", "host_initiated")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session_id is required")
}

func TestCloseSession_BatchError(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	ss := newMockSettlementStore()
	hub := &mockHub{}

	bp := NewBatchProcessor(ss, es, lr, hub, testLogger())
	sc := NewSessionCloser(bp, hub, testLogger())

	// No escrow seeded, so ProcessBatch will fail.
	err := sc.CloseSession(context.Background(), "nonexistent", "expired")
	assert.Error(t, err)
}
