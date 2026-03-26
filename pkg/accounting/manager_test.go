package accounting

import (
	"context"
	"encoding/json"
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

type mockClaimStore struct {
	records   map[string]*store.DeliveryClaimRecord // keyed by "sessionID:blockN"
	pending   []store.DeliveryClaimRecord
	nextDoc   int
	err       error
	createErr error
}

func newMockClaimStore() *mockClaimStore {
	return &mockClaimStore{records: make(map[string]*store.DeliveryClaimRecord)}
}

func claimKey(sessionID string, blockN int) string {
	return fmt.Sprintf("%s:%d", sessionID, blockN)
}

func (m *mockClaimStore) Create(_ context.Context, r *store.DeliveryClaimRecord) (*store.DeliveryClaimRecord, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("claim-doc-%d", m.nextDoc)
	cp := *r
	m.records[claimKey(r.SessionID, r.BlockNumber)] = &cp
	if r.Status == store.StatusPending {
		m.pending = append(m.pending, cp)
	}
	return &cp, nil
}

func (m *mockClaimStore) GetBySessionAndBlock(_ context.Context, sessionID string, blockN int) (*store.DeliveryClaimRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	r, ok := m.records[claimKey(sessionID, blockN)]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *mockClaimStore) ListBySession(_ context.Context, sessionID string) ([]store.DeliveryClaimRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []store.DeliveryClaimRecord
	for _, r := range m.records {
		if r.SessionID == sessionID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockClaimStore) ListPending(_ context.Context) ([]store.DeliveryClaimRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := make([]store.DeliveryClaimRecord, len(m.pending))
	copy(out, m.pending)
	return out, nil
}

func (m *mockClaimStore) UpdateStatus(_ context.Context, docID, status string) error {
	if m.err != nil {
		return m.err
	}
	for k, r := range m.records {
		if r.DocID == docID {
			r.Status = status
			m.records[k] = r
			break
		}
	}
	return nil
}

type mockAttestStore struct {
	records   map[string]*store.AttestationRecord // keyed by "sessionID:blockN"
	nextDoc   int
	err       error
	createErr error
}

func newMockAttestStore() *mockAttestStore {
	return &mockAttestStore{records: make(map[string]*store.AttestationRecord)}
}

func (m *mockAttestStore) Create(_ context.Context, r *store.AttestationRecord) (*store.AttestationRecord, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("attest-doc-%d", m.nextDoc)
	cp := *r
	m.records[claimKey(r.SessionID, r.BlockNumber)] = &cp
	return &cp, nil
}

func (m *mockAttestStore) GetBySessionAndBlock(_ context.Context, sessionID string, blockN int) (*store.AttestationRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	r, ok := m.records[claimKey(sessionID, blockN)]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *mockAttestStore) ListBySession(_ context.Context, sessionID string) ([]store.AttestationRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []store.AttestationRecord
	for _, r := range m.records {
		if r.SessionID == sessionID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockAttestStore) UpdateStatus(_ context.Context, docID, status string) error {
	if m.err != nil {
		return m.err
	}
	for k, r := range m.records {
		if r.DocID == docID {
			r.Status = status
			m.records[k] = r
			break
		}
	}
	return nil
}

type mockLedgerStore struct {
	records    map[string]*store.SessionLedgerRecord // keyed by sessionID
	nextDoc    int
	err        error
	updateErr  error
	lastUpdate map[string]any
}

func newMockLedgerStore() *mockLedgerStore {
	return &mockLedgerStore{records: make(map[string]*store.SessionLedgerRecord)}
}

func (m *mockLedgerStore) Create(_ context.Context, r *store.SessionLedgerRecord) (*store.SessionLedgerRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("ledger-doc-%d", m.nextDoc)
	cp := *r
	m.records[r.SessionID] = &cp
	return &cp, nil
}

func (m *mockLedgerStore) GetBySession(_ context.Context, sessionID string) (*store.SessionLedgerRecord, error) {
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

func (m *mockLedgerStore) Update(_ context.Context, docID string, fields map[string]any) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.lastUpdate = fields
	// Apply fields to the stored record so subsequent reads reflect the change.
	for _, r := range m.records {
		if r.DocID == docID {
			if v, ok := fields["blocksVerified"]; ok {
				r.BlocksVerified = v.(int)
			}
			if v, ok := fields["creditRemaining"]; ok {
				r.CreditRemaining = v.(float64)
			}
			if v, ok := fields["lastComparedBlock"]; ok {
				r.LastComparedBlock = v.(int)
			}
			break
		}
	}
	return nil
}

type mockCompStore struct {
	records []store.ComparisonResultRecord
	nextDoc int
	err     error
}

func newMockCompStore() *mockCompStore {
	return &mockCompStore{}
}

func (m *mockCompStore) Create(_ context.Context, r *store.ComparisonResultRecord) (*store.ComparisonResultRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("comp-doc-%d", m.nextDoc)
	cp := *r
	m.records = append(m.records, cp)
	return &cp, nil
}

func (m *mockCompStore) ListBySession(_ context.Context, sessionID string) ([]store.ComparisonResultRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []store.ComparisonResultRecord
	for _, r := range m.records {
		if r.SessionID == sessionID {
			out = append(out, r)
		}
	}
	return out, nil
}

// --- helpers ---

func testLogger() *zap.SugaredLogger {
	l, _ := zap.NewDevelopment()
	return l.Sugar()
}

func defaultCfg() config.AccountingConfig {
	return config.AccountingConfig{
		Enabled:                   true,
		ComparisonIntervalSeconds: 30,
		AttestationWindowSeconds:  300,
	}
}

func cidJSON(cids ...string) string {
	b, _ := json.Marshal(cids)
	return string(b)
}

func newTestManager(
	cs *mockClaimStore,
	as *mockAttestStore,
	ls *mockLedgerStore,
	cmp *mockCompStore,
	cfg config.AccountingConfig,
) *Manager {
	return NewManager(cs, as, ls, cmp, cfg, testLogger())
}

// --- SubmitDeliveryClaim tests ---

func TestSubmitDeliveryClaim_Success(t *testing.T) {
	cs := newMockClaimStore()
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	req := SubmitClaimRequest{
		SessionID:      "sess-1",
		IndexerID:      "idx-1",
		BlockNumber:    100,
		DocCids:        cidJSON("cid1", "cid2"),
		BlockHash:      "0xabc",
		BatchSignature: "sig1",
	}

	rec, err := m.SubmitDeliveryClaim(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", rec.SessionID)
	assert.Equal(t, "idx-1", rec.IndexerID)
	assert.Equal(t, 100, rec.BlockNumber)
	assert.Equal(t, store.StatusPending, rec.Status)
	assert.NotEmpty(t, rec.ClaimID)
	assert.NotEmpty(t, rec.DocID)
}

func TestSubmitDeliveryClaim_MissingFields(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())
	ctx := context.Background()

	tests := []struct {
		name string
		req  SubmitClaimRequest
	}{
		{"missing session_id", SubmitClaimRequest{IndexerID: "idx", BlockNumber: 1, DocCids: "[]", BlockHash: "0x1"}},
		{"missing indexer_id", SubmitClaimRequest{SessionID: "s", BlockNumber: 1, DocCids: "[]", BlockHash: "0x1"}},
		{"zero block_number", SubmitClaimRequest{SessionID: "s", IndexerID: "idx", BlockNumber: 0, DocCids: "[]", BlockHash: "0x1"}},
		{"negative block_number", SubmitClaimRequest{SessionID: "s", IndexerID: "idx", BlockNumber: -1, DocCids: "[]", BlockHash: "0x1"}},
		{"missing doc_cids", SubmitClaimRequest{SessionID: "s", IndexerID: "idx", BlockNumber: 1, BlockHash: "0x1"}},
		{"missing block_hash", SubmitClaimRequest{SessionID: "s", IndexerID: "idx", BlockNumber: 1, DocCids: "[]"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.SubmitDeliveryClaim(ctx, tc.req)
			assert.Error(t, err)
		})
	}
}

func TestSubmitDeliveryClaim_IdempotentDuplicate(t *testing.T) {
	cs := newMockClaimStore()
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())
	ctx := context.Background()

	cids := cidJSON("cid1", "cid2")
	req := SubmitClaimRequest{
		SessionID: "sess-1", IndexerID: "idx-1", BlockNumber: 10,
		DocCids: cids, BlockHash: "0xabc",
	}

	first, err := m.SubmitDeliveryClaim(ctx, req)
	require.NoError(t, err)

	second, err := m.SubmitDeliveryClaim(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, first.ClaimID, second.ClaimID, "duplicate with same CIDs should return existing record")
}

func TestSubmitDeliveryClaim_DuplicateDifferentCIDs(t *testing.T) {
	cs := newMockClaimStore()
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())
	ctx := context.Background()

	req1 := SubmitClaimRequest{
		SessionID: "sess-1", IndexerID: "idx-1", BlockNumber: 10,
		DocCids: cidJSON("cid1"), BlockHash: "0xabc",
	}
	_, err := m.SubmitDeliveryClaim(ctx, req1)
	require.NoError(t, err)

	req2 := SubmitClaimRequest{
		SessionID: "sess-1", IndexerID: "idx-1", BlockNumber: 10,
		DocCids: cidJSON("cid-DIFFERENT"), BlockHash: "0xabc",
	}
	_, err = m.SubmitDeliveryClaim(ctx, req2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content-addressing fraud")
}

func TestSubmitDeliveryClaim_StoreError(t *testing.T) {
	cs := newMockClaimStore()
	cs.err = fmt.Errorf("db unavailable")
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.SubmitDeliveryClaim(context.Background(), SubmitClaimRequest{
		SessionID: "s", IndexerID: "i", BlockNumber: 1,
		DocCids: "[]", BlockHash: "0x1",
	})
	assert.Error(t, err)
}

// --- SubmitAttestation tests ---

func TestSubmitAttestation_Success(t *testing.T) {
	as := newMockAttestStore()
	m := newTestManager(newMockClaimStore(), as, newMockLedgerStore(), newMockCompStore(), defaultCfg())

	req := SubmitAttestationRequest{
		SessionID:       "sess-1",
		HostID:          "host-1",
		BlockNumber:     100,
		DocCidsReceived: cidJSON("cid1", "cid2"),
		BatchSignature:  "sig1",
	}

	rec, err := m.SubmitAttestation(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", rec.SessionID)
	assert.Equal(t, "host-1", rec.HostID)
	assert.Equal(t, 100, rec.BlockNumber)
	assert.Equal(t, store.StatusPending, rec.Status)
	assert.NotEmpty(t, rec.AttestationID)
}

func TestSubmitAttestation_MissingFields(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())
	ctx := context.Background()

	tests := []struct {
		name string
		req  SubmitAttestationRequest
	}{
		{"missing session_id", SubmitAttestationRequest{HostID: "h", BlockNumber: 1, DocCidsReceived: "[]"}},
		{"missing host_id", SubmitAttestationRequest{SessionID: "s", BlockNumber: 1, DocCidsReceived: "[]"}},
		{"zero block_number", SubmitAttestationRequest{SessionID: "s", HostID: "h", BlockNumber: 0, DocCidsReceived: "[]"}},
		{"missing doc_cids_received", SubmitAttestationRequest{SessionID: "s", HostID: "h", BlockNumber: 1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.SubmitAttestation(ctx, tc.req)
			assert.Error(t, err)
		})
	}
}

func TestSubmitAttestation_DuplicateRejection(t *testing.T) {
	as := newMockAttestStore()
	m := newTestManager(newMockClaimStore(), as, newMockLedgerStore(), newMockCompStore(), defaultCfg())
	ctx := context.Background()

	req := SubmitAttestationRequest{
		SessionID: "sess-1", HostID: "host-1", BlockNumber: 10,
		DocCidsReceived: cidJSON("cid1"),
	}

	_, err := m.SubmitAttestation(ctx, req)
	require.NoError(t, err)

	_, err = m.SubmitAttestation(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")
}

func TestSubmitAttestation_StoreError(t *testing.T) {
	as := newMockAttestStore()
	as.err = fmt.Errorf("db unavailable")
	m := newTestManager(newMockClaimStore(), as, newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.SubmitAttestation(context.Background(), SubmitAttestationRequest{
		SessionID: "s", HostID: "h", BlockNumber: 1, DocCidsReceived: "[]",
	})
	assert.Error(t, err)
}

// --- Compare tests ---

func TestCompare_CleanDelivery(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	cmp := newMockCompStore()
	m := newTestManager(cs, as, newMockLedgerStore(), cmp, defaultCfg())
	ctx := context.Background()

	cids := cidJSON("cid1", "cid2", "cid3")

	// Seed claim and attestation with identical CIDs.
	cs.records[claimKey("sess-1", 5)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "claim-1", SessionID: "sess-1", BlockNumber: 5,
		DocCids: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 5)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "att-1", SessionID: "sess-1", BlockNumber: 5,
		DocCidsReceived: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "sess-1", 5)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, store.OutcomeClean, result.Outcome)
	assert.Equal(t, "claim-1", result.ClaimID)
	assert.Equal(t, "att-1", result.AttestID)
}

func TestCompare_UnderReport(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	m := newTestManager(cs, as, newMockLedgerStore(), newMockCompStore(), defaultCfg())
	ctx := context.Background()

	// Indexer claimed 3 CIDs; host received only 2 (subset).
	cs.records[claimKey("sess-1", 5)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "claim-1", SessionID: "sess-1", BlockNumber: 5,
		DocCids: cidJSON("cid1", "cid2", "cid3"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 5)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "att-1", SessionID: "sess-1", BlockNumber: 5,
		DocCidsReceived: cidJSON("cid1", "cid2"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "sess-1", 5)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, store.OutcomeUnderReport, result.Outcome)
}

func TestCompare_Mismatch(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	m := newTestManager(cs, as, newMockLedgerStore(), newMockCompStore(), defaultCfg())
	ctx := context.Background()

	// Completely divergent CIDs.
	cs.records[claimKey("sess-1", 5)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "claim-1", SessionID: "sess-1", BlockNumber: 5,
		DocCids: cidJSON("cid1", "cid2"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 5)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "att-1", SessionID: "sess-1", BlockNumber: 5,
		DocCidsReceived: cidJSON("cidX", "cidY"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "sess-1", 5)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, store.OutcomeMismatch, result.Outcome)
}

func TestCompare_IndexerSilent(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	cfg := defaultCfg()
	cfg.AttestationWindowSeconds = 1 // short window so it expires immediately
	m := newTestManager(cs, as, newMockLedgerStore(), newMockCompStore(), cfg)
	ctx := context.Background()

	// Only attestation, no claim. Submitted in the past beyond the window.
	as.records[claimKey("sess-1", 5)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "att-1", SessionID: "sess-1", BlockNumber: 5,
		DocCidsReceived: cidJSON("cid1"), Status: store.StatusPending,
		SubmittedAt: time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "sess-1", 5)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, store.OutcomeIndexerSilent, result.Outcome)
	assert.Empty(t, result.ClaimID)
	assert.Equal(t, "att-1", result.AttestID)
}

func TestCompare_HostSilent(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	cfg := defaultCfg()
	cfg.AttestationWindowSeconds = 1
	m := newTestManager(cs, as, newMockLedgerStore(), newMockCompStore(), cfg)
	ctx := context.Background()

	// Only claim, no attestation. Submitted in the past beyond the window.
	cs.records[claimKey("sess-1", 5)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "claim-1", SessionID: "sess-1", BlockNumber: 5,
		DocCids: cidJSON("cid1"), Status: store.StatusPending,
		SubmittedAt: time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "sess-1", 5)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, store.OutcomeHostSilent, result.Outcome)
	assert.Equal(t, "claim-1", result.ClaimID)
	assert.Empty(t, result.AttestID)
}

func TestCompare_HeldUntilBothPresent(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	cfg := defaultCfg()
	cfg.AttestationWindowSeconds = 3600 // large window so nothing expires
	m := newTestManager(cs, as, newMockLedgerStore(), newMockCompStore(), cfg)
	ctx := context.Background()

	// Neither side present.
	result, err := m.Compare(ctx, "sess-1", 5)
	require.NoError(t, err)
	assert.Nil(t, result, "no data at all should return nil")

	// Only claim present, within window — should wait.
	cs.records[claimKey("sess-1", 5)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "claim-1", SessionID: "sess-1", BlockNumber: 5,
		DocCids: cidJSON("cid1"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err = m.Compare(ctx, "sess-1", 5)
	require.NoError(t, err)
	assert.Nil(t, result, "claim only + within window should hold")

	// Only attestation present, within window — should wait.
	delete(cs.records, claimKey("sess-1", 5))
	as.records[claimKey("sess-1", 5)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "att-1", SessionID: "sess-1", BlockNumber: 5,
		DocCidsReceived: cidJSON("cid1"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err = m.Compare(ctx, "sess-1", 5)
	require.NoError(t, err)
	assert.Nil(t, result, "attestation only + within window should hold")
}

func TestCompare_NeitherPresent(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())
	result, err := m.Compare(context.Background(), "sess-1", 5)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// --- Ledger update on clean delivery ---

func TestCompare_CleanDeliveryUpdatesLedger(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	cmp := newMockCompStore()
	m := newTestManager(cs, as, ls, cmp, defaultCfg())
	ctx := context.Background()

	// Pre-create a session ledger.
	err := m.CreateSessionLedger(ctx, "sess-1", "host-1", "idx-1", 100.0, 0.5)
	require.NoError(t, err)

	ledgerBefore, _ := ls.GetBySession(ctx, "sess-1")
	require.NotNil(t, ledgerBefore)
	assert.Equal(t, 0, ledgerBefore.BlocksVerified)
	assert.Equal(t, 100.0, ledgerBefore.CreditRemaining)

	// Seed matching claim and attestation.
	cids := cidJSON("cid1")
	cs.records[claimKey("sess-1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "claim-1", SessionID: "sess-1", BlockNumber: 1,
		DocCids: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 1)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "att-1", SessionID: "sess-1", BlockNumber: 1,
		DocCidsReceived: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "sess-1", 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, store.OutcomeClean, result.Outcome)

	// Verify ledger was updated.
	ledgerAfter, _ := ls.GetBySession(ctx, "sess-1")
	require.NotNil(t, ledgerAfter)
	assert.Equal(t, 1, ledgerAfter.BlocksVerified, "blocksVerified should increment")
	assert.Equal(t, 99.5, ledgerAfter.CreditRemaining, "creditRemaining should decrease by pricePerBlock")
	assert.Equal(t, 1, ledgerAfter.LastComparedBlock)
}

func TestCompare_NonCleanOutcomeDoesNotUpdateLedger(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	m := newTestManager(cs, as, ls, newMockCompStore(), defaultCfg())
	ctx := context.Background()

	err := m.CreateSessionLedger(ctx, "sess-1", "host-1", "idx-1", 100.0, 0.5)
	require.NoError(t, err)

	// Mismatch scenario.
	cs.records[claimKey("sess-1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "claim-1", SessionID: "sess-1", BlockNumber: 1,
		DocCids: cidJSON("cid1"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 1)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "att-1", SessionID: "sess-1", BlockNumber: 1,
		DocCidsReceived: cidJSON("cidX"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "sess-1", 1)
	require.NoError(t, err)
	assert.Equal(t, store.OutcomeMismatch, result.Outcome)

	ledger, _ := ls.GetBySession(ctx, "sess-1")
	assert.Equal(t, 0, ledger.BlocksVerified, "mismatch should not increment blocksVerified")
	assert.Equal(t, 100.0, ledger.CreditRemaining, "mismatch should not touch credit")
}

func TestCompare_CreditFloorAtZero(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	m := newTestManager(cs, as, ls, newMockCompStore(), defaultCfg())
	ctx := context.Background()

	// Ledger with almost no credit remaining.
	err := m.CreateSessionLedger(ctx, "sess-1", "host-1", "idx-1", 0.1, 0.5)
	require.NoError(t, err)

	cids := cidJSON("cid1")
	cs.records[claimKey("sess-1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "cl-1", SessionID: "sess-1", BlockNumber: 1,
		DocCids: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 1)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "at-1", SessionID: "sess-1", BlockNumber: 1,
		DocCidsReceived: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	_, err = m.Compare(ctx, "sess-1", 1)
	require.NoError(t, err)

	ledger, _ := ls.GetBySession(ctx, "sess-1")
	assert.Equal(t, 0.0, ledger.CreditRemaining, "credit should floor at zero, not go negative")
}

// --- CrossCheckHost tests ---

func TestCrossCheckHost_NormalHost(t *testing.T) {
	cmp := newMockCompStore()
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), cmp, defaultCfg())
	ctx := context.Background()

	// 10 comparisons, 2 under-reports (20% — below 30% threshold).
	for i := 0; i < 10; i++ {
		outcome := store.OutcomeClean
		if i < 2 {
			outcome = store.OutcomeUnderReport
		}
		cmp.records = append(cmp.records, store.ComparisonResultRecord{
			SessionID: "sess-1", Outcome: outcome,
		})
	}

	outlier, err := m.CrossCheckHost(ctx, "sess-1", "host-1")
	require.NoError(t, err)
	assert.False(t, outlier)
}

func TestCrossCheckHost_OutlierHost(t *testing.T) {
	cmp := newMockCompStore()
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), cmp, defaultCfg())
	ctx := context.Background()

	// 10 comparisons, 4 under-reports (40% — above 30% threshold).
	for i := 0; i < 10; i++ {
		outcome := store.OutcomeClean
		if i < 4 {
			outcome = store.OutcomeUnderReport
		}
		cmp.records = append(cmp.records, store.ComparisonResultRecord{
			SessionID: "sess-1", Outcome: outcome,
		})
	}

	outlier, err := m.CrossCheckHost(ctx, "sess-1", "host-1")
	require.NoError(t, err)
	assert.True(t, outlier)
}

func TestCrossCheckHost_NoComparisons(t *testing.T) {
	cmp := newMockCompStore()
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), cmp, defaultCfg())

	outlier, err := m.CrossCheckHost(context.Background(), "sess-1", "host-1")
	require.NoError(t, err)
	assert.False(t, outlier, "no comparisons should not flag as outlier")
}

func TestCrossCheckHost_ExactThreshold(t *testing.T) {
	cmp := newMockCompStore()
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), cmp, defaultCfg())

	// 10 comparisons, 3 under-reports (exactly 30% — not above threshold).
	for i := 0; i < 10; i++ {
		outcome := store.OutcomeClean
		if i < 3 {
			outcome = store.OutcomeUnderReport
		}
		cmp.records = append(cmp.records, store.ComparisonResultRecord{
			SessionID: "sess-1", Outcome: outcome,
		})
	}

	outlier, err := m.CrossCheckHost(context.Background(), "sess-1", "host-1")
	require.NoError(t, err)
	assert.False(t, outlier, "exactly 30% should not trigger (threshold is >0.3)")
}

func TestCrossCheckHost_StoreError(t *testing.T) {
	cmp := newMockCompStore()
	cmp.err = fmt.Errorf("db error")
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), cmp, defaultCfg())

	_, err := m.CrossCheckHost(context.Background(), "sess-1", "host-1")
	assert.Error(t, err)
}

// --- CreateSessionLedger tests ---

func TestCreateSessionLedger_Success(t *testing.T) {
	ls := newMockLedgerStore()
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), ls, newMockCompStore(), defaultCfg())

	err := m.CreateSessionLedger(context.Background(), "sess-1", "host-1", "idx-1", 50.0, 0.25)
	require.NoError(t, err)

	rec, err := ls.GetBySession(context.Background(), "sess-1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "sess-1", rec.SessionID)
	assert.Equal(t, "host-1", rec.HostID)
	assert.Equal(t, "idx-1", rec.IndexerID)
	assert.Equal(t, 50.0, rec.InitialEscrow)
	assert.Equal(t, 50.0, rec.CreditRemaining)
	assert.Equal(t, 0.25, rec.PricePerBlock)
	assert.Equal(t, 0, rec.BlocksVerified)
	assert.Equal(t, 0, rec.LastComparedBlock)
	assert.NotEmpty(t, rec.LedgerID)
	assert.NotEmpty(t, rec.UpdatedAt)
}

func TestCreateSessionLedger_StoreError(t *testing.T) {
	ls := newMockLedgerStore()
	ls.err = fmt.Errorf("db unavailable")
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), ls, newMockCompStore(), defaultCfg())

	err := m.CreateSessionLedger(context.Background(), "sess-1", "host-1", "idx-1", 50.0, 0.25)
	assert.Error(t, err)
}

// --- GetSessionLedger / GetComparisons passthrough ---

func TestGetSessionLedger(t *testing.T) {
	ls := newMockLedgerStore()
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), ls, newMockCompStore(), defaultCfg())
	ctx := context.Background()

	_ = m.CreateSessionLedger(ctx, "sess-1", "h", "i", 10, 1)
	rec, err := m.GetSessionLedger(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, "sess-1", rec.SessionID)
}

func TestGetComparisons(t *testing.T) {
	cmp := newMockCompStore()
	cmp.records = []store.ComparisonResultRecord{
		{SessionID: "sess-1", Outcome: store.OutcomeClean},
		{SessionID: "sess-1", Outcome: store.OutcomeMismatch},
		{SessionID: "other", Outcome: store.OutcomeClean},
	}
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), cmp, defaultCfg())

	results, err := m.GetComparisons(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// --- VerifyContentAddressing ---

func TestVerifyContentAddressing_Valid(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	ok, err := m.VerifyContentAddressing(context.Background(), "idx-1", 10, cidJSON("cid1", "cid2"))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestVerifyContentAddressing_Empty(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	ok, err := m.VerifyContentAddressing(context.Background(), "idx-1", 10, "[]")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestVerifyContentAddressing_InvalidJSON(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.VerifyContentAddressing(context.Background(), "idx-1", 10, "not-json")
	assert.Error(t, err)
}

// --- compareDocCids edge cases ---

func TestCompareDocCids_SameLengthDifferentContent(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	// Same count but different CIDs → mismatch.
	result, err := m.compareDocCids(cidJSON("a", "b"), cidJSON("a", "c"))
	require.NoError(t, err)
	assert.Equal(t, store.OutcomeMismatch, result)
}

func TestCompareDocCids_AttestMoreThanClaimed(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	// Host attested more CIDs than claimed → mismatch (not under-report).
	result, err := m.compareDocCids(cidJSON("a"), cidJSON("a", "b"))
	require.NoError(t, err)
	assert.Equal(t, store.OutcomeMismatch, result)
}

func TestCompareDocCids_SubsetButDifferent(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	// Attested fewer but includes a CID not in the claim set → mismatch.
	result, err := m.compareDocCids(cidJSON("a", "b", "c"), cidJSON("a", "x"))
	require.NoError(t, err)
	assert.Equal(t, store.OutcomeMismatch, result)
}

func TestCompareDocCids_EmptyBoth(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	result, err := m.compareDocCids("[]", "[]")
	require.NoError(t, err)
	assert.Equal(t, store.OutcomeClean, result)
}

func TestCompareDocCids_InvalidClaimJSON(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.compareDocCids("not-json", cidJSON("a"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal claim CIDs")
}

func TestCompareDocCids_InvalidAttestJSON(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.compareDocCids(cidJSON("a"), "not-json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal attestation CIDs")
}

func TestCompare_InvalidDocCidsJSON(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	cmp := newMockCompStore()
	m := newTestManager(cs, as, ls, cmp, defaultCfg())

	ctx := context.Background()

	// Seed claim with invalid DocCids JSON.
	cs.records["s1:1"] = &store.DeliveryClaimRecord{
		ClaimID: "c1", SessionID: "s1", BlockNumber: 1,
		DocCids: "not-valid-json", Status: store.StatusPending,
	}
	as.records["s1:1"] = &store.AttestationRecord{
		AttestationID: "a1", SessionID: "s1", BlockNumber: 1,
		DocCidsReceived: cidJSON("a"), Status: store.StatusPending,
	}

	_, err := m.Compare(ctx, "s1", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "compare session s1 block 1")
}

// --- Multiple consecutive clean deliveries ---

func TestCompare_MultipleCleanDeliveries_IncrementalLedger(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	m := newTestManager(cs, as, ls, newMockCompStore(), defaultCfg())
	ctx := context.Background()

	_ = m.CreateSessionLedger(ctx, "sess-1", "h", "i", 10.0, 1.0)

	for blockN := 1; blockN <= 5; blockN++ {
		cids := cidJSON(fmt.Sprintf("cid-%d", blockN))
		cs.records[claimKey("sess-1", blockN)] = &store.DeliveryClaimRecord{
			DocID: fmt.Sprintf("c-%d", blockN), ClaimID: fmt.Sprintf("cl-%d", blockN),
			SessionID: "sess-1", BlockNumber: blockN, DocCids: cids,
			Status: store.StatusPending, SubmittedAt: time.Now().UTC().Format(time.RFC3339),
		}
		as.records[claimKey("sess-1", blockN)] = &store.AttestationRecord{
			DocID: fmt.Sprintf("a-%d", blockN), AttestationID: fmt.Sprintf("at-%d", blockN),
			SessionID: "sess-1", BlockNumber: blockN, DocCidsReceived: cids,
			Status: store.StatusPending, SubmittedAt: time.Now().UTC().Format(time.RFC3339),
		}

		result, err := m.Compare(ctx, "sess-1", blockN)
		require.NoError(t, err)
		assert.Equal(t, store.OutcomeClean, result.Outcome)
	}

	ledger, _ := ls.GetBySession(ctx, "sess-1")
	assert.Equal(t, 5, ledger.BlocksVerified)
	assert.Equal(t, 5.0, ledger.CreditRemaining) // 10 - 5*1.0
	assert.Equal(t, 5, ledger.LastComparedBlock)
}

// --- StartComparisonLoop / Stop ---

func TestStartComparisonLoop_StopsCleanly(t *testing.T) {
	cfg := defaultCfg()
	cfg.ComparisonIntervalSeconds = 1
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), cfg)

	ctx, cancel := context.WithCancel(context.Background())
	m.StartComparisonLoop(ctx)

	// Let it tick at least once.
	time.Sleep(1200 * time.Millisecond)
	cancel()
	m.Stop() // should not hang
}

func TestStartComparisonLoop_DefaultInterval(t *testing.T) {
	cfg := defaultCfg()
	cfg.ComparisonIntervalSeconds = 0 // triggers default 30s fallback
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), cfg)

	ctx, cancel := context.WithCancel(context.Background())
	m.StartComparisonLoop(ctx)
	cancel()
	m.Stop()
}

func TestStartComparisonLoop_ProcessesPending(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	cmp := newMockCompStore()
	cfg := defaultCfg()
	cfg.ComparisonIntervalSeconds = 1
	m := newTestManager(cs, as, newMockLedgerStore(), cmp, cfg)
	ctx := context.Background()

	// Seed a pending claim with matching attestation.
	cids := cidJSON("cid1")
	cs.records[claimKey("sess-1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "cl-1", SessionID: "sess-1", BlockNumber: 1,
		DocCids: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	cs.pending = []store.DeliveryClaimRecord{*cs.records[claimKey("sess-1", 1)]}
	as.records[claimKey("sess-1", 1)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "at-1", SessionID: "sess-1", BlockNumber: 1,
		DocCidsReceived: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	m.StartComparisonLoop(ctx)
	time.Sleep(1500 * time.Millisecond)
	m.Stop()

	// The comparison loop should have processed the pending claim.
	results, err := cmp.ListBySession(ctx, "sess-1")
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	assert.Equal(t, store.OutcomeClean, results[0].Outcome)
}

// --- runComparisons with store error ---

func TestRunComparisons_ListPendingError(t *testing.T) {
	cs := newMockClaimStore()
	cs.err = fmt.Errorf("list pending failed")
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	// Should not panic; just logs the error.
	m.runComparisons(context.Background())
}

func TestRunComparisons_CompareError(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	m := newTestManager(cs, as, newMockLedgerStore(), newMockCompStore(), defaultCfg())

	// Add a pending claim but make attestation store return error during Compare.
	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "sess-1", BlockNumber: 1, Status: store.StatusPending},
	}
	as.err = fmt.Errorf("attest lookup failed")

	// Should not panic.
	m.runComparisons(context.Background())
}

func TestRunComparisons_DeduplicatesKeys(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	cmp := newMockCompStore()
	m := newTestManager(cs, as, newMockLedgerStore(), cmp, defaultCfg())

	// Multiple pending claims for the same (session, block) should be deduped.
	cids := cidJSON("cid1")
	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "sess-1", BlockNumber: 1, Status: store.StatusPending},
		{SessionID: "sess-1", BlockNumber: 1, Status: store.StatusPending}, // duplicate
		{SessionID: "sess-1", BlockNumber: 2, Status: store.StatusPending},
	}
	cs.records[claimKey("sess-1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "cl-1", SessionID: "sess-1", BlockNumber: 1,
		DocCids: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 1)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "at-1", SessionID: "sess-1", BlockNumber: 1,
		DocCidsReceived: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	cs.records[claimKey("sess-1", 2)] = &store.DeliveryClaimRecord{
		DocID: "c-2", ClaimID: "cl-2", SessionID: "sess-1", BlockNumber: 2,
		DocCids: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 2)] = &store.AttestationRecord{
		DocID: "a-2", AttestationID: "at-2", SessionID: "sess-1", BlockNumber: 2,
		DocCidsReceived: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	m.runComparisons(context.Background())

	// Should produce exactly 2 comparison results (not 3).
	results, _ := cmp.ListBySession(context.Background(), "sess-1")
	assert.Len(t, results, 2)
}

// --- withinAttestationWindow ---

func TestWithinAttestationWindow_InvalidTimestamp(t *testing.T) {
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	// Invalid timestamp should return false (treat as expired).
	assert.False(t, m.withinAttestationWindow("not-a-timestamp"))
}

func TestWithinAttestationWindow_RecentTimestamp(t *testing.T) {
	cfg := defaultCfg()
	cfg.AttestationWindowSeconds = 3600
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), cfg)

	recent := time.Now().UTC().Format(time.RFC3339)
	assert.True(t, m.withinAttestationWindow(recent))
}

func TestWithinAttestationWindow_ExpiredTimestamp(t *testing.T) {
	cfg := defaultCfg()
	cfg.AttestationWindowSeconds = 1
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), cfg)

	old := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	assert.False(t, m.withinAttestationWindow(old))
}

// --- updateLedger edge cases ---

func TestUpdateLedger_MissingLedger(t *testing.T) {
	ls := newMockLedgerStore()
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), ls, newMockCompStore(), defaultCfg())

	// No ledger exists — should not panic.
	m.updateLedger(context.Background(), "nonexistent", 1)
}

func TestUpdateLedger_StoreGetError(t *testing.T) {
	ls := newMockLedgerStore()
	ls.err = fmt.Errorf("get failed")
	m := newTestManager(newMockClaimStore(), newMockAttestStore(), ls, newMockCompStore(), defaultCfg())

	// Should not panic on store error.
	m.updateLedger(context.Background(), "sess-1", 1)
}

// --- Compare records comparison result even when compSt errors ---

func TestCompare_ComparisonStoreError_StillReturnsResult(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	cmp := newMockCompStore()
	cmp.err = fmt.Errorf("comp store down")
	m := newTestManager(cs, as, newMockLedgerStore(), cmp, defaultCfg())
	ctx := context.Background()

	cids := cidJSON("cid1")
	cs.records[claimKey("sess-1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c-1", ClaimID: "cl-1", SessionID: "sess-1", BlockNumber: 1,
		DocCids: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("sess-1", 1)] = &store.AttestationRecord{
		DocID: "a-1", AttestationID: "at-1", SessionID: "sess-1", BlockNumber: 1,
		DocCidsReceived: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Compare should still return the result even if persisting to compSt fails.
	result, err := m.Compare(ctx, "sess-1", 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, store.OutcomeClean, result.Outcome)
}

// --- Escalation tests ---

type mockEscalator struct {
	mismatchCalls    []string
	underReportCalls []string
	mismatchErr      error
	underReportErr   error
}

func (m *mockEscalator) OnMismatch(_ context.Context, sessionID string, claimID, attestID string) error {
	m.mismatchCalls = append(m.mismatchCalls, sessionID)
	return m.mismatchErr
}

func (m *mockEscalator) OnUnderReportExpired(_ context.Context, sessionID string, claimID, attestID string) error {
	m.underReportCalls = append(m.underReportCalls, sessionID)
	return m.underReportErr
}

func TestCompare_MismatchTriggersEscalation(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{}

	m := newTestManager(cs, as, ls, cmp, defaultCfg())
	m.WithEscalation(esc)

	ctx := context.Background()
	cs.records[claimKey("s1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c1", ClaimID: "c1", SessionID: "s1", BlockNumber: 1,
		DocCids: cidJSON("cid-a"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("s1", 1)] = &store.AttestationRecord{
		DocID: "a1", AttestationID: "a1", SessionID: "s1", BlockNumber: 1,
		DocCidsReceived: cidJSON("cid-x"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "s1", 1)
	require.NoError(t, err)
	assert.Equal(t, store.OutcomeMismatch, result.Outcome)
	assert.Len(t, esc.mismatchCalls, 1)
	assert.Equal(t, "s1", esc.mismatchCalls[0])
}

func TestCompare_MismatchEscalationErrorDoesNotBreak(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{mismatchErr: fmt.Errorf("hub down")}

	m := newTestManager(cs, as, ls, cmp, defaultCfg())
	m.WithEscalation(esc)

	ctx := context.Background()
	cs.records[claimKey("s1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c1", ClaimID: "c1", SessionID: "s1", BlockNumber: 1,
		DocCids: cidJSON("cid-a"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("s1", 1)] = &store.AttestationRecord{
		DocID: "a1", AttestationID: "a1", SessionID: "s1", BlockNumber: 1,
		DocCidsReceived: cidJSON("cid-x"), Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "s1", 1)
	require.NoError(t, err)
	assert.Equal(t, store.OutcomeMismatch, result.Outcome)
}

func TestCompare_CleanDoesNotEscalate(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{}

	m := newTestManager(cs, as, ls, cmp, defaultCfg())
	m.WithEscalation(esc)

	ctx := context.Background()
	cids := cidJSON("cid-a", "cid-b")
	cs.records[claimKey("s1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c1", ClaimID: "c1", SessionID: "s1", BlockNumber: 1,
		DocCids: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	as.records[claimKey("s1", 1)] = &store.AttestationRecord{
		DocID: "a1", AttestationID: "a1", SessionID: "s1", BlockNumber: 1,
		DocCidsReceived: cids, Status: store.StatusPending,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}

	result, err := m.Compare(ctx, "s1", 1)
	require.NoError(t, err)
	assert.Equal(t, store.OutcomeClean, result.Outcome)
	assert.Empty(t, esc.mismatchCalls)
	assert.Empty(t, esc.underReportCalls)
}

func TestEscalateExpiredUnderReports(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{}

	cfg := defaultCfg()
	cfg.UnderReportGraceSeconds = 1 // 1 second grace

	m := newTestManager(cs, as, ls, cmp, cfg)
	m.WithEscalation(esc)

	ctx := context.Background()

	// Add a pending claim so the session shows up in runComparisons.
	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "s1", BlockNumber: 1, Status: store.StatusPending},
	}

	// Add an expired under-report comparison result.
	cmp.records = append(cmp.records, store.ComparisonResultRecord{
		ComparisonID: "comp-1", SessionID: "s1", BlockNumber: 1,
		Outcome: store.OutcomeUnderReport, ClaimID: "c1", AttestationID: "a1",
		ComparedAt: time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339),
	})

	m.escalateExpiredUnderReports(ctx)
	assert.Len(t, esc.underReportCalls, 1)
	assert.Equal(t, "s1", esc.underReportCalls[0])
}

func TestEscalateExpiredUnderReports_NilEscalationHandler(t *testing.T) {
	cs := newMockClaimStore()
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())
	// escalation is nil by default — should return immediately without panic.
	m.escalateExpiredUnderReports(context.Background())
}

func TestEscalateExpiredUnderReports_ZeroGraceSeconds(t *testing.T) {
	cs := newMockClaimStore()
	esc := &mockEscalator{}
	cfg := defaultCfg()
	cfg.UnderReportGraceSeconds = 0 // zero grace → early return

	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), cfg)
	m.WithEscalation(esc)

	m.escalateExpiredUnderReports(context.Background())
	assert.Empty(t, esc.underReportCalls)
}

func TestEscalateExpiredUnderReports_ListPendingError(t *testing.T) {
	cs := newMockClaimStore()
	esc := &mockEscalator{}
	cfg := defaultCfg()
	cfg.UnderReportGraceSeconds = 1

	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), cfg)
	m.WithEscalation(esc)

	// Force ListPending to error.
	cs.err = fmt.Errorf("db down")
	m.escalateExpiredUnderReports(context.Background())
	assert.Empty(t, esc.underReportCalls)
}

func TestEscalateExpiredUnderReports_ListBySessionError(t *testing.T) {
	cs := newMockClaimStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{}
	cfg := defaultCfg()
	cfg.UnderReportGraceSeconds = 1

	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), cmp, cfg)
	m.WithEscalation(esc)

	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "s1", BlockNumber: 1, Status: store.StatusPending},
	}
	// Force compSt.ListBySession to error.
	cmp.err = fmt.Errorf("comp store error")

	m.escalateExpiredUnderReports(context.Background())
	assert.Empty(t, esc.underReportCalls)
}

func TestEscalateExpiredUnderReports_TimeParseError(t *testing.T) {
	cs := newMockClaimStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{}
	cfg := defaultCfg()
	cfg.UnderReportGraceSeconds = 1

	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), cmp, cfg)
	m.WithEscalation(esc)

	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "s1", BlockNumber: 1, Status: store.StatusPending},
	}
	// Add under-report with invalid ComparedAt timestamp.
	cmp.records = append(cmp.records, store.ComparisonResultRecord{
		ComparisonID: "comp-1", SessionID: "s1", BlockNumber: 1,
		Outcome: store.OutcomeUnderReport, ClaimID: "c1", AttestationID: "a1",
		ComparedAt: "not-a-timestamp",
	})

	m.escalateExpiredUnderReports(context.Background())
	assert.Empty(t, esc.underReportCalls, "invalid timestamp should be skipped")
}

func TestEscalateExpiredUnderReports_OnUnderReportExpiredError(t *testing.T) {
	cs := newMockClaimStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{underReportErr: fmt.Errorf("hub failed")}
	cfg := defaultCfg()
	cfg.UnderReportGraceSeconds = 1

	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), cmp, cfg)
	m.WithEscalation(esc)

	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "s1", BlockNumber: 1, Status: store.StatusPending},
	}
	cmp.records = append(cmp.records, store.ComparisonResultRecord{
		ComparisonID: "comp-1", SessionID: "s1", BlockNumber: 1,
		Outcome: store.OutcomeUnderReport, ClaimID: "c1", AttestationID: "a1",
		ComparedAt: time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339),
	})

	// Should not panic; error is logged.
	m.escalateExpiredUnderReports(context.Background())
	assert.Len(t, esc.underReportCalls, 1)
}

func TestSubmitDeliveryClaim_GetBySessionAndBlockError(t *testing.T) {
	cs := newMockClaimStore()
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	// Seed a record then set error so GetBySessionAndBlock fails.
	cs.records[claimKey("s1", 1)] = &store.DeliveryClaimRecord{
		DocID: "c1", ClaimID: "c1", SessionID: "s1", BlockNumber: 1,
		DocCids: cidJSON("cid1"), Status: store.StatusPending,
	}
	cs.err = fmt.Errorf("get failed")

	_, err := m.SubmitDeliveryClaim(context.Background(), SubmitClaimRequest{
		SessionID: "s1", IndexerID: "i1", BlockNumber: 1,
		DocCids: cidJSON("cid1"), BlockHash: "0x1",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get failed")
}

func TestSubmitAttestation_GetBySessionAndBlockError(t *testing.T) {
	as := newMockAttestStore()
	m := newTestManager(newMockClaimStore(), as, newMockLedgerStore(), newMockCompStore(), defaultCfg())

	// Set error so GetBySessionAndBlock fails.
	as.err = fmt.Errorf("attest get failed")

	_, err := m.SubmitAttestation(context.Background(), SubmitAttestationRequest{
		SessionID: "s1", HostID: "h1", BlockNumber: 1, DocCidsReceived: cidJSON("cid1"),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "attest get failed")
}

func TestSubmitDeliveryClaim_CreateError(t *testing.T) {
	cs := newMockClaimStore()
	cs.createErr = fmt.Errorf("create failed")
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.SubmitDeliveryClaim(context.Background(), SubmitClaimRequest{
		SessionID: "s1", IndexerID: "i1", BlockNumber: 1,
		DocCids: cidJSON("cid1"), BlockHash: "0x1",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create failed")
}

func TestSubmitAttestation_CreateError(t *testing.T) {
	as := newMockAttestStore()
	as.createErr = fmt.Errorf("create attest failed")
	m := newTestManager(newMockClaimStore(), as, newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.SubmitAttestation(context.Background(), SubmitAttestationRequest{
		SessionID: "s1", HostID: "h1", BlockNumber: 1, DocCidsReceived: cidJSON("cid1"),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create attest failed")
}

func TestRunComparisons_MultiSessionSort(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	cmp := newMockCompStore()
	m := newTestManager(cs, as, newMockLedgerStore(), cmp, defaultCfg())

	// Pending claims across two different sessions to exercise session-level sort branch.
	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "sess-B", BlockNumber: 1, Status: store.StatusPending},
		{SessionID: "sess-A", BlockNumber: 2, Status: store.StatusPending},
	}
	// Neither session has matching data, so Compare returns nil results — that's fine.
	m.runComparisons(context.Background())
}

func TestEscalateExpiredUnderReports_SkipsNonUnderReportOutcomes(t *testing.T) {
	cs := newMockClaimStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{}
	cfg := defaultCfg()
	cfg.UnderReportGraceSeconds = 1

	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), cmp, cfg)
	m.WithEscalation(esc)

	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "s1", BlockNumber: 1, Status: store.StatusPending},
	}

	// Add a clean outcome (not under_report) — should be skipped.
	cmp.records = append(cmp.records, store.ComparisonResultRecord{
		ComparisonID: "comp-1", SessionID: "s1", BlockNumber: 1,
		Outcome: store.OutcomeClean, ClaimID: "c1", AttestationID: "a1",
		ComparedAt: time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339),
	})

	m.escalateExpiredUnderReports(context.Background())
	assert.Empty(t, esc.underReportCalls, "clean outcomes should not trigger escalation")
}

func TestCompare_ClaimStoreError(t *testing.T) {
	cs := newMockClaimStore()
	cs.err = fmt.Errorf("claim store down")
	m := newTestManager(cs, newMockAttestStore(), newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.Compare(context.Background(), "s1", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claim store down")
}

func TestCompare_AttestationStoreError(t *testing.T) {
	as := newMockAttestStore()
	as.err = fmt.Errorf("attest store down")
	m := newTestManager(newMockClaimStore(), as, newMockLedgerStore(), newMockCompStore(), defaultCfg())

	_, err := m.Compare(context.Background(), "s1", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "attest store down")
}

func TestEscalateExpiredUnderReports_WithinGraceNoEscalation(t *testing.T) {
	cs := newMockClaimStore()
	as := newMockAttestStore()
	ls := newMockLedgerStore()
	cmp := newMockCompStore()
	esc := &mockEscalator{}

	cfg := defaultCfg()
	cfg.UnderReportGraceSeconds = 3600 // 1 hour grace

	m := newTestManager(cs, as, ls, cmp, cfg)
	m.WithEscalation(esc)

	ctx := context.Background()

	cs.pending = []store.DeliveryClaimRecord{
		{SessionID: "s1", BlockNumber: 1, Status: store.StatusPending},
	}

	// Under-report just happened — within the grace window.
	cmp.records = append(cmp.records, store.ComparisonResultRecord{
		ComparisonID: "comp-1", SessionID: "s1", BlockNumber: 1,
		Outcome: store.OutcomeUnderReport, ClaimID: "c1", AttestationID: "a1",
		ComparedAt: time.Now().UTC().Format(time.RFC3339),
	})

	m.escalateExpiredUnderReports(ctx)
	assert.Empty(t, esc.underReportCalls)
}
