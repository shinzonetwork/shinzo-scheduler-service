package settlement

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- mock stores ---

type mockEscrowStore struct {
	records   map[string]*store.EscrowAccountRecord // keyed by sessionID
	nextDoc   int
	err       error
	updateErr error
	lastUpd   map[string]any
}

func newMockEscrowStore() *mockEscrowStore {
	return &mockEscrowStore{records: make(map[string]*store.EscrowAccountRecord)}
}

func (m *mockEscrowStore) Create(_ context.Context, r *store.EscrowAccountRecord) (*store.EscrowAccountRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("edoc-%d", m.nextDoc)
	cp := *r
	m.records[r.SessionID] = &cp
	return &cp, nil
}

func (m *mockEscrowStore) GetBySession(_ context.Context, sessionID string) (*store.EscrowAccountRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	r, ok := m.records[sessionID]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *mockEscrowStore) ListActive(_ context.Context) ([]store.EscrowAccountRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []store.EscrowAccountRecord
	for _, r := range m.records {
		if r.Status == store.StatusActive {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockEscrowStore) Update(_ context.Context, docID string, fields map[string]any) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.lastUpd = fields
	// Apply fields to the in-memory record so subsequent reads reflect changes.
	for _, r := range m.records {
		if r.DocID == docID {
			if v, ok := fields["currentBalance"]; ok {
				r.CurrentBalance = v.(float64)
			}
			if v, ok := fields["initialBalance"]; ok {
				r.InitialBalance = v.(float64)
			}
			if v, ok := fields["lowCreditSignaled"]; ok {
				r.LowCreditSignaled = v.(bool)
			}
			if v, ok := fields["gracePeriodEndsAt"]; ok {
				r.GracePeriodEndsAt = v.(string)
			}
			if v, ok := fields["status"]; ok {
				r.Status = v.(string)
			}
		}
	}
	return nil
}

type mockLedgerReader struct {
	records map[string]*store.SessionLedgerRecord
	err     error
}

func newMockLedgerReader() *mockLedgerReader {
	return &mockLedgerReader{records: make(map[string]*store.SessionLedgerRecord)}
}

func (m *mockLedgerReader) GetBySession(_ context.Context, sessionID string) (*store.SessionLedgerRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	r, ok := m.records[sessionID]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

type mockHub struct {
	batchCalls    int
	lowCreditCnt  int
	batchErr      error
	lowCreditErr  error
	lastLowCredit MsgSignalLowCredit
}

func (m *mockHub) BroadcastCloseSession(_ context.Context, _ MsgCloseSession) (string, error) {
	return "tx-close", nil
}

func (m *mockHub) BroadcastBatchSettlement(_ context.Context, msg MsgBatchSettlement) (string, error) {
	m.batchCalls++
	if m.batchErr != nil {
		return "", m.batchErr
	}
	return "tx-batch-abc", nil
}

func (m *mockHub) BroadcastLowCredit(_ context.Context, msg MsgSignalLowCredit) (string, error) {
	m.lowCreditCnt++
	m.lastLowCredit = msg
	if m.lowCreditErr != nil {
		return "", m.lowCreditErr
	}
	return "tx-low", nil
}

func (m *mockHub) BroadcastSlash(_ context.Context, _ MsgSlash) (string, error) {
	return "tx-slash", nil
}

func testCfg() config.SettlementConfig {
	return config.SettlementConfig{
		Enabled:                true,
		DrainIntervalSeconds:   10,
		LowCreditMultiplier:    2.0,
		GracePeriodSeconds:     3600,
		SettlementWindowBlocks: 100,
		VerdictThresholdM:      2,
		VerdictThresholdN:      3,
	}
}

func testLogger() *zap.SugaredLogger {
	l, _ := zap.NewDevelopment()
	return l.Sugar()
}

// --- tests ---

func TestCreateEscrow_Success(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	hub := &mockHub{}
	cfg := testCfg()

	em := NewEscrowManager(es, lr, hub, cfg, testLogger())

	err := em.CreateEscrow(context.Background(), "sess-1", "host-1", "idx-1", 1000.0, 0.5)
	require.NoError(t, err)

	rec := es.records["sess-1"]
	require.NotNil(t, rec)
	assert.Equal(t, "sess-1", rec.SessionID)
	assert.Equal(t, "host-1", rec.HostID)
	assert.Equal(t, "idx-1", rec.IndexerID)
	assert.Equal(t, 1000.0, rec.InitialBalance)
	assert.Equal(t, 1000.0, rec.CurrentBalance)
	assert.Equal(t, 0.5, rec.PricePerBlock)
	assert.Equal(t, 0.5*cfg.LowCreditMultiplier, rec.LowWaterThreshold)
	assert.False(t, rec.LowCreditSignaled)
	assert.Equal(t, store.StatusActive, rec.Status)
}

func TestCreateEscrow_StoreError(t *testing.T) {
	es := newMockEscrowStore()
	es.err = fmt.Errorf("db down")
	em := NewEscrowManager(es, newMockLedgerReader(), &mockHub{}, testCfg(), testLogger())

	err := em.CreateEscrow(context.Background(), "s1", "h1", "i1", 100, 1)
	assert.Error(t, err)
}

func TestDrainSession_BalanceDecreases(t *testing.T) {
	// Continuous credit drainage based on verified blocks.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	hub := &mockHub{}
	cfg := testCfg()
	em := NewEscrowManager(es, lr, hub, cfg, testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 1000.0, 0.5))

	lr.records["sess-1"] = &store.SessionLedgerRecord{
		SessionID:      "sess-1",
		BlocksVerified: 100,
		PricePerBlock:  0.5,
	}

	require.NoError(t, em.DrainSession(ctx, "sess-1"))

	rec := es.records["sess-1"]
	// Expected: 1000 - (100 * 0.5) = 950
	assert.Equal(t, 950.0, rec.CurrentBalance)
}

func TestDrainSession_LowCreditSignal(t *testing.T) {
	// Low credit signal emitted when balance drops below threshold.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	hub := &mockHub{}
	cfg := testCfg()
	em := NewEscrowManager(es, lr, hub, cfg, testLogger())

	ctx := context.Background()
	// Price=1.0, LowWater = 1.0 * 2.0 = 2.0; initial balance = 3.0
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 3.0, 1.0))

	lr.records["sess-1"] = &store.SessionLedgerRecord{
		SessionID:      "sess-1",
		BlocksVerified: 2,
		PricePerBlock:  1.0,
	}

	require.NoError(t, em.DrainSession(ctx, "sess-1"))

	rec := es.records["sess-1"]
	// Balance should be 3.0 - 2*1.0 = 1.0, which is < lowWater 2.0
	assert.Equal(t, 1.0, rec.CurrentBalance)
	assert.True(t, rec.LowCreditSignaled)
	assert.NotEmpty(t, rec.GracePeriodEndsAt)
	assert.Equal(t, 1, hub.lowCreditCnt)
	assert.Equal(t, "sess-1", hub.lastLowCredit.SessionID)
	assert.InDelta(t, 1.0, hub.lastLowCredit.CreditRemaining, 0.001)
}

func TestDrainSession_GracePeriodExpiry(t *testing.T) {
	// Grace period expiry marks escrow as expired.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	hub := &mockHub{}
	cfg := testCfg()
	em := NewEscrowManager(es, lr, hub, cfg, testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 100.0, 1.0))

	// Simulate low credit already signaled with expired grace period.
	rec := es.records["sess-1"]
	rec.LowCreditSignaled = true
	rec.GracePeriodEndsAt = time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)

	lr.records["sess-1"] = &store.SessionLedgerRecord{
		SessionID:      "sess-1",
		BlocksVerified: 50,
		PricePerBlock:  1.0,
	}

	require.NoError(t, em.DrainSession(ctx, "sess-1"))

	assert.Equal(t, store.StatusExpired, es.records["sess-1"].Status)
}

func TestDrainSession_CreditExhaustion(t *testing.T) {
	// Balance reaches zero when all credits consumed.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	hub := &mockHub{}
	cfg := testCfg()
	em := NewEscrowManager(es, lr, hub, cfg, testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 10.0, 1.0))

	lr.records["sess-1"] = &store.SessionLedgerRecord{
		SessionID:      "sess-1",
		BlocksVerified: 15, // 15 * 1.0 > 10.0
		PricePerBlock:  1.0,
	}

	require.NoError(t, em.DrainSession(ctx, "sess-1"))

	rec := es.records["sess-1"]
	assert.Equal(t, 0.0, rec.CurrentBalance)
	assert.Equal(t, store.StatusExpired, rec.Status)
}

func TestDrainSession_InactiveEscrowSkipped(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	em := NewEscrowManager(es, lr, &mockHub{}, testCfg(), testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 100.0, 1.0))
	es.records["sess-1"].Status = store.StatusExpired

	lr.records["sess-1"] = &store.SessionLedgerRecord{
		SessionID:      "sess-1",
		BlocksVerified: 5,
		PricePerBlock:  1.0,
	}

	// Should return nil without updating (skips non-active escrow).
	require.NoError(t, em.DrainSession(ctx, "sess-1"))
	assert.Nil(t, es.lastUpd)
}

func TestDrainSession_NoLedger(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	em := NewEscrowManager(es, lr, &mockHub{}, testCfg(), testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 100.0, 1.0))

	// No ledger record — should return nil without error.
	require.NoError(t, em.DrainSession(ctx, "sess-1"))
	assert.Nil(t, es.lastUpd)
}

func TestTopUp_ClearsLowCreditSignal(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	em := NewEscrowManager(es, lr, &mockHub{}, testCfg(), testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 10.0, 1.0))

	// Simulate low credit state.
	rec := es.records["sess-1"]
	rec.LowCreditSignaled = true
	rec.GracePeriodEndsAt = time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	rec.CurrentBalance = 1.0

	require.NoError(t, em.TopUp(ctx, "sess-1", 50.0))

	updated := es.records["sess-1"]
	assert.False(t, updated.LowCreditSignaled)
	assert.Empty(t, updated.GracePeriodEndsAt)
	assert.Equal(t, store.StatusActive, updated.Status)
	// Balance: 1.0 (current) + 50.0 = 51.0
	assert.Equal(t, 51.0, updated.CurrentBalance)
}

func TestTopUp_SessionNotFound(t *testing.T) {
	es := newMockEscrowStore()
	em := NewEscrowManager(es, newMockLedgerReader(), &mockHub{}, testCfg(), testLogger())

	err := em.TopUp(context.Background(), "nonexistent", 100.0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escrow not found")
}

func TestTopUp_NegativeAmount(t *testing.T) {
	es := newMockEscrowStore()
	em := NewEscrowManager(es, newMockLedgerReader(), &mockHub{}, testCfg(), testLogger())

	err := em.TopUp(context.Background(), "sess-1", -50.0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "topup amount must be positive")
}

func TestTopUp_ZeroAmount(t *testing.T) {
	es := newMockEscrowStore()
	em := NewEscrowManager(es, newMockLedgerReader(), &mockHub{}, testCfg(), testLogger())

	err := em.TopUp(context.Background(), "sess-1", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "topup amount must be positive")
}

func TestDrainSession_LowCreditBroadcastError(t *testing.T) {
	// Broadcast failure for low credit is logged but does not fail the drain.
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	hub := &mockHub{lowCreditErr: fmt.Errorf("hub unreachable")}
	em := NewEscrowManager(es, lr, hub, testCfg(), testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 3.0, 1.0))
	lr.records["sess-1"] = &store.SessionLedgerRecord{
		SessionID:      "sess-1",
		BlocksVerified: 2,
		PricePerBlock:  1.0,
	}

	// Should succeed despite broadcast failure.
	require.NoError(t, em.DrainSession(ctx, "sess-1"))
	assert.True(t, es.records["sess-1"].LowCreditSignaled)
}

func TestDrainAll_ProcessesActiveEscrows(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	hub := &mockHub{}
	em := NewEscrowManager(es, lr, hub, testCfg(), testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "s1", "h1", "i1", 100.0, 1.0))
	require.NoError(t, em.CreateEscrow(ctx, "s2", "h1", "i2", 200.0, 2.0))

	lr.records["s1"] = &store.SessionLedgerRecord{SessionID: "s1", BlocksVerified: 10, PricePerBlock: 1.0}
	lr.records["s2"] = &store.SessionLedgerRecord{SessionID: "s2", BlocksVerified: 20, PricePerBlock: 2.0}

	em.drainAll(ctx)

	assert.Equal(t, 90.0, es.records["s1"].CurrentBalance)
	assert.Equal(t, 160.0, es.records["s2"].CurrentBalance)
}

func TestDrainAll_DrainSessionError(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	lr.err = fmt.Errorf("ledger read failure")
	hub := &mockHub{}
	em := NewEscrowManager(es, lr, hub, testCfg(), testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "s1", "h1", "i1", 100.0, 1.0))

	// drainAll should not panic even when DrainSession returns an error.
	em.drainAll(ctx)
}

func TestStartDrainLoop_TickerFires(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	cfg := testCfg()
	cfg.DrainIntervalSeconds = 1
	em := NewEscrowManager(es, lr, &mockHub{}, cfg, testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "s1", "h1", "i1", 100.0, 1.0))
	lr.records["s1"] = &store.SessionLedgerRecord{
		SessionID:      "s1",
		BlocksVerified: 10,
		PricePerBlock:  1.0,
	}

	em.StartDrainLoop(ctx)
	// Wait for at least one tick to fire.
	time.Sleep(1500 * time.Millisecond)
	em.Stop()

	// After drain, balance should have decreased.
	assert.Equal(t, 90.0, es.records["s1"].CurrentBalance)
}

func TestDrainAll_StoreListError(t *testing.T) {
	es := newMockEscrowStore()
	es.err = fmt.Errorf("list failure")
	em := NewEscrowManager(es, newMockLedgerReader(), &mockHub{}, testCfg(), testLogger())

	// Should not panic, just logs the error.
	em.drainAll(context.Background())
}

func TestStartDrainLoop_AndStop(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	cfg := testCfg()
	cfg.DrainIntervalSeconds = 1
	em := NewEscrowManager(es, lr, &mockHub{}, cfg, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	em.StartDrainLoop(ctx)
	// Let it run briefly, then stop gracefully.
	em.Stop()
}

func TestStartDrainLoop_DefaultInterval(t *testing.T) {
	es := newMockEscrowStore()
	cfg := testCfg()
	cfg.DrainIntervalSeconds = 0 // triggers default 60s
	em := NewEscrowManager(es, newMockLedgerReader(), &mockHub{}, cfg, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so loop exits
	em.StartDrainLoop(ctx)
	em.Stop()
}

func TestNoopBroadcaster(t *testing.T) {
	nb := NoopBroadcaster{}
	ctx := context.Background()

	tx, err := nb.BroadcastCloseSession(ctx, MsgCloseSession{})
	assert.NoError(t, err)
	assert.Empty(t, tx)

	tx, err = nb.BroadcastBatchSettlement(ctx, MsgBatchSettlement{})
	assert.NoError(t, err)
	assert.Empty(t, tx)

	tx, err = nb.BroadcastLowCredit(ctx, MsgSignalLowCredit{})
	assert.NoError(t, err)
	assert.Empty(t, tx)

	tx, err = nb.BroadcastSlash(ctx, MsgSlash{})
	assert.NoError(t, err)
	assert.Empty(t, tx)
}

func TestDrainSession_EscrowNotFound(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	em := NewEscrowManager(es, lr, &mockHub{}, testCfg(), testLogger())

	// No escrow seeded — GetBySession returns nil, nil.
	err := em.DrainSession(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, es.lastUpd)
}

func TestDrainSession_EscrowNonActiveStatus(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	em := NewEscrowManager(es, lr, &mockHub{}, testCfg(), testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 100.0, 1.0))
	es.records["sess-1"].Status = "settled"

	lr.records["sess-1"] = &store.SessionLedgerRecord{
		SessionID:      "sess-1",
		BlocksVerified: 5,
		PricePerBlock:  1.0,
	}

	// Non-active escrow should be skipped without error.
	err := em.DrainSession(ctx, "sess-1")
	require.NoError(t, err)
	assert.Nil(t, es.lastUpd)
}

func TestDrainSession_GracePeriodNotExpiredYet(t *testing.T) {
	es := newMockEscrowStore()
	lr := newMockLedgerReader()
	em := NewEscrowManager(es, lr, &mockHub{}, testCfg(), testLogger())

	ctx := context.Background()
	require.NoError(t, em.CreateEscrow(ctx, "sess-1", "h1", "i1", 100.0, 1.0))

	rec := es.records["sess-1"]
	rec.LowCreditSignaled = true
	rec.GracePeriodEndsAt = time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)

	lr.records["sess-1"] = &store.SessionLedgerRecord{
		SessionID:      "sess-1",
		BlocksVerified: 10,
		PricePerBlock:  1.0,
	}

	require.NoError(t, em.DrainSession(ctx, "sess-1"))

	// Grace period not expired yet, should remain active (balance 90 > 0).
	assert.Equal(t, store.StatusActive, es.records["sess-1"].Status)
}
