package settlement

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock verdict store ---

type mockVerdictStore struct {
	records   map[string]*store.VerdictRecord // keyed by sessionID
	nextDoc   int
	err       error
	updateErr error
	lastUpd   map[string]any
}

func newMockVerdictStore() *mockVerdictStore {
	return &mockVerdictStore{records: make(map[string]*store.VerdictRecord)}
}

func (m *mockVerdictStore) Create(_ context.Context, r *store.VerdictRecord) (*store.VerdictRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("vdoc-%d", m.nextDoc)
	cp := *r
	m.records[r.SessionID] = &cp
	return &cp, nil
}

func (m *mockVerdictStore) GetBySession(_ context.Context, sessionID string) (*store.VerdictRecord, error) {
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

func (m *mockVerdictStore) Update(_ context.Context, docID string, fields map[string]any) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.lastUpd = fields
	for _, r := range m.records {
		if r.DocID == docID {
			if v, ok := fields["schedulerSignatures"]; ok {
				r.SchedulerSignatures = v.(string)
			}
			if v, ok := fields["submittedToHub"]; ok {
				r.SubmittedToHub = v.(bool)
			}
		}
	}
	return nil
}

// --- tests ---

func TestCreateVerdict_Success(t *testing.T) {
	// Verdict document created in store before on-chain submission.
	vs := newMockVerdictStore()
	hub := &mockHub{}
	cfg := testCfg()

	vm := NewVerdictManager(vs, hub, cfg, testLogger())

	cids := []string{"bafy-evidence-1", "bafy-evidence-2"}
	rec, err := vm.CreateVerdict(context.Background(), "sess-1", "mismatch", cids)
	require.NoError(t, err)
	require.NotNil(t, rec)

	assert.Equal(t, "sess-1", rec.SessionID)
	assert.Equal(t, "mismatch", rec.Outcome)
	assert.False(t, rec.SubmittedToHub)
	assert.NotEmpty(t, rec.VerdictID)
	assert.NotEmpty(t, rec.CreatedAt)

	// Check evidence CIDs were serialized.
	var storedCids []string
	require.NoError(t, json.Unmarshal([]byte(rec.EvidenceCids), &storedCids))
	assert.Equal(t, cids, storedCids)

	// Should have one initial signature.
	var sigs []string
	require.NoError(t, json.Unmarshal([]byte(rec.SchedulerSignatures), &sigs))
	assert.Len(t, sigs, 1)
}

func TestCreateVerdict_StoreError(t *testing.T) {
	vs := newMockVerdictStore()
	vs.err = fmt.Errorf("db unavailable")
	vm := NewVerdictManager(vs, &mockHub{}, testCfg(), testLogger())

	rec, err := vm.CreateVerdict(context.Background(), "s1", "clean", nil)
	assert.Error(t, err)
	assert.Nil(t, rec)
}

func TestAddSignature_Success(t *testing.T) {
	vs := newMockVerdictStore()
	vm := NewVerdictManager(vs, &mockHub{}, testCfg(), testLogger())

	_, err := vm.CreateVerdict(context.Background(), "sess-1", "mismatch", nil)
	require.NoError(t, err)

	err = vm.AddSignature(context.Background(), "sess-1", "sig-from-node-2")
	require.NoError(t, err)

	// Read back and verify signature was appended.
	rec := vs.records["sess-1"]
	var sigs []string
	require.NoError(t, json.Unmarshal([]byte(rec.SchedulerSignatures), &sigs))
	assert.Len(t, sigs, 2)
	assert.Equal(t, "sig-from-node-2", sigs[1])
}

func TestAddSignature_VerdictNotFound(t *testing.T) {
	vs := newMockVerdictStore()
	vm := NewVerdictManager(vs, &mockHub{}, testCfg(), testLogger())

	err := vm.AddSignature(context.Background(), "nonexistent", "sig")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "verdict not found")
}

func TestHasQuorum_WithEnoughSignatures(t *testing.T) {
	// M-of-N quorum check. Config sets M=2.
	vs := newMockVerdictStore()
	cfg := testCfg()
	cfg.VerdictThresholdM = 2
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	_, err := vm.CreateVerdict(context.Background(), "sess-1", "mismatch", nil)
	require.NoError(t, err)

	// Add a second signature to meet quorum.
	require.NoError(t, vm.AddSignature(context.Background(), "sess-1", "sig-node-2"))

	ok, err := vm.HasQuorum(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestHasQuorum_InsufficientSignatures(t *testing.T) {
	vs := newMockVerdictStore()
	cfg := testCfg()
	cfg.VerdictThresholdM = 3
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	_, err := vm.CreateVerdict(context.Background(), "sess-1", "mismatch", nil)
	require.NoError(t, err)

	// Only 1 sig (the initial one), need 3.
	ok, err := vm.HasQuorum(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHasQuorum_ExactThreshold(t *testing.T) {
	vs := newMockVerdictStore()
	cfg := testCfg()
	cfg.VerdictThresholdM = 1
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	_, err := vm.CreateVerdict(context.Background(), "sess-1", "clean", nil)
	require.NoError(t, err)

	// Exactly 1 signature, threshold is 1.
	ok, err := vm.HasQuorum(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestHasQuorum_VerdictNotFound(t *testing.T) {
	vs := newMockVerdictStore()
	vm := NewVerdictManager(vs, &mockHub{}, testCfg(), testLogger())

	ok, err := vm.HasQuorum(context.Background(), "nonexistent")
	assert.False(t, ok)
	assert.NoError(t, err) // nil verdict returns false, nil error
}

func TestSubmitToHub_Success(t *testing.T) {
	vs := newMockVerdictStore()
	cfg := testCfg()
	cfg.VerdictThresholdM = 1
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	_, err := vm.CreateVerdict(context.Background(), "sess-1", "mismatch", []string{"cid-1"})
	require.NoError(t, err)

	err = vm.SubmitToHub(context.Background(), "sess-1")
	require.NoError(t, err)

	rec := vs.records["sess-1"]
	assert.True(t, rec.SubmittedToHub)
}

func TestSubmitToHub_FailsWithoutQuorum(t *testing.T) {
	// Cannot submit to hub without meeting M-of-N threshold.
	vs := newMockVerdictStore()
	cfg := testCfg()
	cfg.VerdictThresholdM = 5
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	_, err := vm.CreateVerdict(context.Background(), "sess-1", "mismatch", nil)
	require.NoError(t, err)

	err = vm.SubmitToHub(context.Background(), "sess-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not meet M-of-N threshold")

	// Verify it was NOT marked as submitted.
	rec := vs.records["sess-1"]
	assert.False(t, rec.SubmittedToHub)
}

func TestSubmitToHub_VerdictNotFoundAfterQuorum(t *testing.T) {
	// Edge case: quorum passes but second lookup fails.
	vs := newMockVerdictStore()
	cfg := testCfg()
	cfg.VerdictThresholdM = 1
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	_, err := vm.CreateVerdict(context.Background(), "sess-1", "mismatch", nil)
	require.NoError(t, err)

	// Delete the record between HasQuorum and the second GetBySession.
	// This simulates a race condition. We inject error after first successful call.
	// Since our mock uses a single error field, we handle this by deleting the record.
	delete(vs.records, "sess-1")

	// HasQuorum will fail (no record), so it won't get to the second lookup.
	err = vm.SubmitToHub(context.Background(), "sess-1")
	// HasQuorum returns false for nil verdict, so the error is about threshold.
	assert.Error(t, err)
}

func TestSubmitToHub_UpdateError(t *testing.T) {
	vs := newMockVerdictStore()
	vs.updateErr = fmt.Errorf("write failed")
	cfg := testCfg()
	cfg.VerdictThresholdM = 1
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	// Seed a verdict with 1 signature directly.
	sigsJSON, _ := json.Marshal([]string{"sig-1"})
	vs.records["sess-1"] = &store.VerdictRecord{
		DocID:               "vdoc-1",
		VerdictID:           "v-1",
		SessionID:           "sess-1",
		Outcome:             "mismatch",
		SchedulerSignatures: string(sigsJSON),
		SubmittedToHub:      false,
	}

	err := vm.SubmitToHub(context.Background(), "sess-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestAddSignature_MultipleAppends(t *testing.T) {
	vs := newMockVerdictStore()
	vm := NewVerdictManager(vs, &mockHub{}, testCfg(), testLogger())

	_, err := vm.CreateVerdict(context.Background(), "sess-1", "under_report", nil)
	require.NoError(t, err)

	for i := 0; i < 4; i++ {
		require.NoError(t, vm.AddSignature(context.Background(), "sess-1", fmt.Sprintf("sig-%d", i)))
	}

	rec := vs.records["sess-1"]
	var sigs []string
	require.NoError(t, json.Unmarshal([]byte(rec.SchedulerSignatures), &sigs))
	// 1 initial + 4 added = 5
	assert.Len(t, sigs, 5)
}

func TestCreateVerdict_NilEvidenceCids(t *testing.T) {
	vs := newMockVerdictStore()
	vm := NewVerdictManager(vs, &mockHub{}, testCfg(), testLogger())

	rec, err := vm.CreateVerdict(context.Background(), "sess-1", "clean", nil)
	require.NoError(t, err)

	var cids []string
	require.NoError(t, json.Unmarshal([]byte(rec.EvidenceCids), &cids))
	assert.Empty(t, cids)
}

// mockVerdictStoreWithGetErr wraps mockVerdictStore and injects a GetBySession
// error after a configurable number of successful calls.
type mockVerdictStoreWithGetErr struct {
	*mockVerdictStore
	getErr       error
	getCallCount int
	failAfterN   int // fail on the (failAfterN+1)th call
}

func (m *mockVerdictStoreWithGetErr) GetBySession(ctx context.Context, sessionID string) (*store.VerdictRecord, error) {
	m.getCallCount++
	if m.getErr != nil && m.getCallCount > m.failAfterN {
		return nil, m.getErr
	}
	return m.mockVerdictStore.GetBySession(ctx, sessionID)
}

func TestSubmitToHub_HasQuorumError(t *testing.T) {
	// GetBySession returns an error during HasQuorum check.
	inner := newMockVerdictStore()
	vs := &mockVerdictStoreWithGetErr{
		mockVerdictStore: inner,
		getErr:           fmt.Errorf("db read failure"),
		failAfterN:       0, // fail on first GetBySession call (inside HasQuorum)
	}
	cfg := verdictCfgWithThreshold(1)
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	err := vm.SubmitToHub(context.Background(), "sess-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db read failure")
}

func TestSubmitToHub_VerdictNotFoundAfterQuorumCheck(t *testing.T) {
	// HasQuorum succeeds but the second GetBySession returns nil (race).
	inner := newMockVerdictStore()
	sigsJSON, _ := json.Marshal([]string{"sig-1"})
	inner.records["sess-1"] = &store.VerdictRecord{
		DocID:               "vdoc-1",
		VerdictID:           "v-1",
		SessionID:           "sess-1",
		Outcome:             "mismatch",
		SchedulerSignatures: string(sigsJSON),
		SubmittedToHub:      false,
	}

	vs := &mockVerdictStoreWithGetErr{
		mockVerdictStore: inner,
		getErr:           fmt.Errorf("gone"),
		failAfterN:       1, // first call (HasQuorum) succeeds, second call fails
	}
	cfg := verdictCfgWithThreshold(1)
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	err := vm.SubmitToHub(context.Background(), "sess-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gone")
}

func TestAddSignature_CorruptSignatures(t *testing.T) {
	vs := newMockVerdictStore()
	vm := NewVerdictManager(vs, &mockHub{}, testCfg(), testLogger())

	// Seed a verdict with corrupt JSON in SchedulerSignatures.
	vs.records["sess-1"] = &store.VerdictRecord{
		DocID:               "vdoc-1",
		VerdictID:           "v-1",
		SessionID:           "sess-1",
		Outcome:             "mismatch",
		SchedulerSignatures: "not-valid-json",
	}

	err := vm.AddSignature(context.Background(), "sess-1", "sig-new")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "corrupt scheduler signatures")
}

func TestHasQuorum_CorruptSignatures(t *testing.T) {
	vs := newMockVerdictStore()
	cfg := testCfg()
	cfg.VerdictThresholdM = 1
	vm := NewVerdictManager(vs, &mockHub{}, cfg, testLogger())

	vs.records["sess-1"] = &store.VerdictRecord{
		DocID:               "vdoc-1",
		VerdictID:           "v-1",
		SessionID:           "sess-1",
		Outcome:             "mismatch",
		SchedulerSignatures: "{invalid}",
	}

	ok, err := vm.HasQuorum(context.Background(), "sess-1")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "corrupt scheduler signatures")
}

func TestHasQuorum_DBError(t *testing.T) {
	vs := newMockVerdictStore()
	vs.err = fmt.Errorf("db connection lost")
	vm := NewVerdictManager(vs, &mockHub{}, testCfg(), testLogger())

	ok, err := vm.HasQuorum(context.Background(), "sess-1")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "db connection lost")
}

func verdictCfgWithThreshold(m int) config.SettlementConfig {
	cfg := testCfg()
	cfg.VerdictThresholdM = m
	return cfg
}
