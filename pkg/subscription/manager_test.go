package subscription

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- mock stores ---

type mockSubStore struct {
	records   map[string]*store.SubscriptionRecord
	nextDoc   int
	err       error
	updateErr error
}

func newMockSubStore() *mockSubStore {
	return &mockSubStore{records: make(map[string]*store.SubscriptionRecord)}
}

func (m *mockSubStore) Create(_ context.Context, r *store.SubscriptionRecord) (*store.SubscriptionRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("doc-%d", m.nextDoc)
	cp := *r
	m.records[r.SubscriptionID] = &cp
	return &cp, nil
}

func (m *mockSubStore) GetByID(_ context.Context, subID string) (*store.SubscriptionRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	r, ok := m.records[subID]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *mockSubStore) ListByHost(_ context.Context, hostID string) ([]store.SubscriptionRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []store.SubscriptionRecord
	for _, r := range m.records {
		if r.HostID == hostID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockSubStore) ListByStatus(_ context.Context, status string) ([]store.SubscriptionRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []store.SubscriptionRecord
	for _, r := range m.records {
		if r.Status == status {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockSubStore) Update(_ context.Context, docID string, fields map[string]any) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.err != nil {
		return m.err
	}
	for _, r := range m.records {
		if r.DocID == docID {
			if s, ok := fields["status"].(string); ok {
				r.Status = s
			}
			if s, ok := fields["paymentRef"].(string); ok {
				r.PaymentRef = s
			}
			if s, ok := fields["activatedAt"].(string); ok {
				r.ActivatedAt = s
			}
			if s, ok := fields["expiresAt"].(string); ok {
				r.ExpiresAt = s
			}
			return nil
		}
	}
	return fmt.Errorf("doc not found: %s", docID)
}

type mockIndexerQuerier struct {
	record *store.IndexerRecord
	err    error
}

func (m *mockIndexerQuerier) GetByPeerID(_ context.Context, _ string) (*store.IndexerRecord, error) {
	return m.record, m.err
}

// --- helpers ---

func newManager(subSt subStore, idxSt indexerQuerier) *Manager {
	log, _ := zap.NewDevelopment()
	return NewManager(subSt, idxSt, log.Sugar())
}

func activeIndexer(peerID string) *store.IndexerRecord {
	return &store.IndexerRecord{
		DocID:     "idx-doc",
		PeerID:    peerID,
		Multiaddr: "/ip4/127.0.0.1/tcp/4001",
		Status:    store.StatusActive,
	}
}

type mockMatchRecorder struct {
	records []*store.MatchHistoryRecord
	err     error
}

func (m *mockMatchRecorder) Create(_ context.Context, r *store.MatchHistoryRecord) (*store.MatchHistoryRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	cp := *r
	m.records = append(m.records, &cp)
	return &cp, nil
}

// --- tests ---

func TestWithMatchRecorder(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{record: activeIndexer("idx1")})
	mr := &mockMatchRecorder{}
	mgr.WithMatchRecorder(mr)
	assert.Equal(t, mr, mgr.matchSt)
}

func TestCreate_RecordsMatchHistory(t *testing.T) {
	mr := &mockMatchRecorder{}
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{record: activeIndexer("idx1")})
	mgr.WithMatchRecorder(mr)

	sub, err := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	require.NoError(t, err)
	require.NotNil(t, sub)
	require.Len(t, mr.records, 1)
	assert.Equal(t, "host1", mr.records[0].HostID)
	assert.Equal(t, "idx1", mr.records[0].IndexerID)
	assert.Equal(t, store.SubTypeTip, mr.records[0].MatchType)
}

func TestCreate_MatchRecorderErrorDoesNotFail(t *testing.T) {
	mr := &mockMatchRecorder{err: fmt.Errorf("write fail")}
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{record: activeIndexer("idx1")})
	mgr.WithMatchRecorder(mr)

	sub, err := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	require.NoError(t, err)
	require.NotNil(t, sub)
}

func TestCreate_SetsPendingStatus(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, err := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	require.NoError(t, err)
	assert.Equal(t, store.StatusPending, sub.Status)
	assert.NotEmpty(t, sub.SubscriptionID)
}

func TestCreate_FailsWhenIndexerUnavailable(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{record: nil})
	_, err := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "missing", SubType: store.SubTypeTip,
	})
	assert.Error(t, err)
}

func TestCreate_FailsWhenIndexerInactive(t *testing.T) {
	inactive := activeIndexer("idx1")
	inactive.Status = store.StatusInactive
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{record: inactive})
	_, err := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	assert.Error(t, err)
}

func TestCreate_ValidationErrors(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{record: activeIndexer("idx1")})

	tests := []struct {
		name string
		req  CreateRequest
	}{
		{"missing host_id", CreateRequest{IndexerID: "idx1", SubType: store.SubTypeTip}},
		{"missing indexer_id", CreateRequest{HostID: "host1", SubType: store.SubTypeTip}},
		{"invalid sub_type", CreateRequest{HostID: "host1", IndexerID: "idx1", SubType: "unknown"}},
		{"snapshot missing blocks", CreateRequest{HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeSnapshot}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.Create(context.Background(), tt.req)
			assert.Error(t, err)
		})
	}
}

func TestActivate_TransitionsPendingToActive(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})

	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	err := mgr.Activate(context.Background(), ActivateRequest{
		SubscriptionID: sub.SubscriptionID,
		PaymentRef:     "tx-abc",
		ExpiresAt:      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err)

	updated, _ := ss.GetByID(context.Background(), sub.SubscriptionID)
	assert.Equal(t, store.StatusActive, updated.Status)
	assert.Equal(t, "tx-abc", updated.PaymentRef)
}

func TestActivate_FailsWhenNotPending(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	// Activate once
	_ = mgr.Activate(context.Background(), ActivateRequest{
		SubscriptionID: sub.SubscriptionID, PaymentRef: "tx1",
	})
	// Activate again — should fail
	err := mgr.Activate(context.Background(), ActivateRequest{
		SubscriptionID: sub.SubscriptionID, PaymentRef: "tx2",
	})
	assert.Error(t, err)
}

func TestCancel_SetsStatusCancelled(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	err := mgr.Cancel(context.Background(), sub.SubscriptionID)
	require.NoError(t, err)

	updated, _ := ss.GetByID(context.Background(), sub.SubscriptionID)
	assert.Equal(t, store.StatusCancelled, updated.Status)
}

func TestCancel_FailsWhenAlreadyExpired(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	// Force status to expired
	_ = ss.Update(context.Background(), sub.DocID, map[string]any{"status": store.StatusExpired})
	err := mgr.Cancel(context.Background(), sub.SubscriptionID)
	assert.Error(t, err)
}

func TestGet_ReturnsMultiaddrOnlyWhenActive(t *testing.T) {
	ss := newMockSubStore()
	idxStore := &mockIndexerQuerier{record: activeIndexer("idx1")}
	mgr := newManager(ss, idxStore)

	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})

	// Before activation — indexer should be nil
	_, indexer, err := mgr.Get(context.Background(), sub.SubscriptionID)
	require.NoError(t, err)
	assert.Nil(t, indexer)

	// Activate
	_ = mgr.Activate(context.Background(), ActivateRequest{
		SubscriptionID: sub.SubscriptionID, PaymentRef: "tx1",
	})

	// After activation — indexer should be returned
	_, indexer, err = mgr.Get(context.Background(), sub.SubscriptionID)
	require.NoError(t, err)
	require.NotNil(t, indexer)
	assert.Equal(t, "idx1", indexer.PeerID)
}

func TestListByHost(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})

	_, _ = mgr.Create(context.Background(), CreateRequest{HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip})
	_, _ = mgr.Create(context.Background(), CreateRequest{HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip})
	_, _ = mgr.Create(context.Background(), CreateRequest{HostID: "host2", IndexerID: "idx1", SubType: store.SubTypeTip})

	subs, err := mgr.ListByHost(context.Background(), "host1")
	require.NoError(t, err)
	assert.Len(t, subs, 2)
}

func TestCreate_IndexerStoreError(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{err: fmt.Errorf("db down")})
	_, err := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	assert.Error(t, err)
}

func TestCreate_SubStoreCreateError(t *testing.T) {
	ss := &mockSubStore{records: make(map[string]*store.SubscriptionRecord), err: fmt.Errorf("write fail")}
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	_, err := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	assert.Error(t, err)
}

func TestActivate_GetByIDError(t *testing.T) {
	ss := &mockSubStore{records: make(map[string]*store.SubscriptionRecord), err: fmt.Errorf("db error")}
	mgr := newManager(ss, &mockIndexerQuerier{})
	err := mgr.Activate(context.Background(), ActivateRequest{SubscriptionID: "sub-1", PaymentRef: "tx1"})
	assert.Error(t, err)
}

func TestActivate_NotFound(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{})
	err := mgr.Activate(context.Background(), ActivateRequest{SubscriptionID: "nonexistent", PaymentRef: "tx1"})
	assert.Error(t, err)
}

func TestActivate_UpdateError(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	ss.updateErr = fmt.Errorf("update failed")
	err := mgr.Activate(context.Background(), ActivateRequest{
		SubscriptionID: sub.SubscriptionID, PaymentRef: "tx1",
	})
	assert.Error(t, err)
}

func TestActivate_WithoutExpiresAt(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	err := mgr.Activate(context.Background(), ActivateRequest{
		SubscriptionID: sub.SubscriptionID, PaymentRef: "tx1",
		// ExpiresAt intentionally empty
	})
	require.NoError(t, err)
	updated, _ := ss.GetByID(context.Background(), sub.SubscriptionID)
	assert.Equal(t, store.StatusActive, updated.Status)
	assert.Empty(t, updated.ExpiresAt)
}

func TestCancel_GetByIDError(t *testing.T) {
	ss := &mockSubStore{records: make(map[string]*store.SubscriptionRecord), err: fmt.Errorf("db error")}
	mgr := newManager(ss, &mockIndexerQuerier{})
	err := mgr.Cancel(context.Background(), "sub-1")
	assert.Error(t, err)
}

func TestCancel_NotFound(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{})
	err := mgr.Cancel(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestCancel_AlreadyCancelled(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	_ = ss.Update(context.Background(), sub.DocID, map[string]any{"status": store.StatusCancelled})
	err := mgr.Cancel(context.Background(), sub.SubscriptionID)
	assert.Error(t, err)
}

func TestCancel_UpdateError(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	ss.updateErr = fmt.Errorf("update failed")
	err := mgr.Cancel(context.Background(), sub.SubscriptionID)
	assert.Error(t, err)
}

func TestGet_GetByIDError(t *testing.T) {
	ss := &mockSubStore{records: make(map[string]*store.SubscriptionRecord), err: fmt.Errorf("db error")}
	mgr := newManager(ss, &mockIndexerQuerier{})
	_, _, err := mgr.Get(context.Background(), "sub-1")
	assert.Error(t, err)
}

func TestGet_NotFound(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{})
	_, _, err := mgr.Get(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestGet_IndexerError(t *testing.T) {
	ss := newMockSubStore()
	idxQ := &mockIndexerQuerier{record: activeIndexer("idx1")}
	mgr := newManager(ss, idxQ)
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	_ = mgr.Activate(context.Background(), ActivateRequest{SubscriptionID: sub.SubscriptionID, PaymentRef: "tx1"})
	// Now make the indexer store return an error.
	idxQ.err = fmt.Errorf("indexer store down")
	_, _, err := mgr.Get(context.Background(), sub.SubscriptionID)
	assert.Error(t, err)
}

func TestExpireOverdue_ListError(t *testing.T) {
	ss := &mockSubStore{records: make(map[string]*store.SubscriptionRecord), err: fmt.Errorf("list fail")}
	mgr := newManager(ss, &mockIndexerQuerier{})
	// should log and not panic
	mgr.expireOverdue(context.Background())
}

func TestExpireOverdue_EmptyExpiresAt(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	// Activate without ExpiresAt.
	_ = mgr.Activate(context.Background(), ActivateRequest{SubscriptionID: sub.SubscriptionID, PaymentRef: "tx1"})

	mgr.expireOverdue(context.Background())

	updated, _ := ss.GetByID(context.Background(), sub.SubscriptionID)
	assert.Equal(t, store.StatusActive, updated.Status)
}

func TestExpireOverdue_InvalidTime(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	_ = mgr.Activate(context.Background(), ActivateRequest{SubscriptionID: sub.SubscriptionID, PaymentRef: "tx1"})
	_ = ss.Update(context.Background(), sub.DocID, map[string]any{"expiresAt": "not-a-time"})

	mgr.expireOverdue(context.Background())

	updated, _ := ss.GetByID(context.Background(), sub.SubscriptionID)
	assert.Equal(t, store.StatusActive, updated.Status)
}

func TestExpireOverdue_UpdateError(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})
	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	_ = mgr.Activate(context.Background(), ActivateRequest{
		SubscriptionID: sub.SubscriptionID, PaymentRef: "tx1", ExpiresAt: past,
	})
	ss.updateErr = fmt.Errorf("update failed")
	// should log warn and not panic
	mgr.expireOverdue(context.Background())
}

func TestStartExpiryLoop_Stop(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.StartExpiryLoop(ctx)
	done := make(chan struct{})
	go func() {
		mgr.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() deadlocked")
	}
}

func TestExpireOverdue(t *testing.T) {
	ss := newMockSubStore()
	mgr := newManager(ss, &mockIndexerQuerier{record: activeIndexer("idx1")})

	sub, _ := mgr.Create(context.Background(), CreateRequest{
		HostID: "host1", IndexerID: "idx1", SubType: store.SubTypeTip,
	})
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	_ = mgr.Activate(context.Background(), ActivateRequest{
		SubscriptionID: sub.SubscriptionID,
		PaymentRef:     "tx1",
		ExpiresAt:      past,
	})

	mgr.expireOverdue(context.Background())

	updated, _ := ss.GetByID(context.Background(), sub.SubscriptionID)
	assert.Equal(t, store.StatusExpired, updated.Status)
}

func TestStartExpiryLoop_ContextCancel(t *testing.T) {
	mgr := newManager(newMockSubStore(), &mockIndexerQuerier{})
	ctx, cancel := context.WithCancel(context.Background())
	mgr.StartExpiryLoop(ctx)
	done := make(chan struct{})
	go func() {
		cancel()
		mgr.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine did not exit after context cancel")
	}
}
