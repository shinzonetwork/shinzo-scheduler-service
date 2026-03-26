package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- mocks ---

type mockIndexerLister struct {
	records []store.IndexerRecord
	err     error
}

func (m *mockIndexerLister) ListAllActive(_ context.Context) ([]store.IndexerRecord, error) {
	return m.records, m.err
}

type mockProbeInserter struct {
	inserted  []*store.ProbeResultRecord
	pruned    []string
	insertErr error
	pruneErr  error
}

func (m *mockProbeInserter) Insert(_ context.Context, r *store.ProbeResultRecord) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = append(m.inserted, r)
	return nil
}

func (m *mockProbeInserter) PruneOldest(_ context.Context, indexerID string, _ int) error {
	if m.pruneErr != nil {
		return m.pruneErr
	}
	m.pruned = append(m.pruned, indexerID)
	return nil
}

// signalingReliabilityUpdater sends on ch every time UpdateReliability is called.
type signalingReliabilityUpdater struct {
	ch chan struct{}
}

func (m *signalingReliabilityUpdater) UpdateReliability(_ context.Context, _ string, _ float64, _ int, _ string) error {
	m.ch <- struct{}{}
	return nil
}

type mockReliabilityUpdater struct {
	calls []reliabilityCall
}

type reliabilityCall struct {
	docID  string
	score  float64
	tip    int
	status string
}

func (m *mockReliabilityUpdater) UpdateReliability(_ context.Context, docID string, score float64, tip int, status string) error {
	m.calls = append(m.calls, reliabilityCall{docID, score, tip, status})
	return nil
}

// --- helpers ---

func testProber(idxSt indexerLister, probeSt probeInserter, updater reliabilityUpdater, httpClient *http.Client) *Prober {
	log, _ := zap.NewDevelopment()
	cfg := config.ProbeConfig{
		IntervalSeconds:      60,
		TimeoutSeconds:       2,
		TipLagThreshold:      10,
		InactiveAfterMinutes: 10,
		ProbeHistoryLimit:    200,
		MaxConcurrentProbes:  5,
	}
	p := &Prober{
		cfg:        cfg,
		indexerReg: updater,
		indexerSt:  idxSt,
		probeSt:    probeSt,
		httpClient: httpClient,
		log:        log.Sugar(),
		stopCh:     make(chan struct{}),
		sem:        make(chan struct{}, 5),
	}
	return p
}

// --- fetchHealth tests ---

func TestFetchHealth_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 42, Status: "ok"})
	}))
	defer srv.Close()

	p := testProber(nil, nil, nil, srv.Client())
	tip, ok := p.fetchHealth(srv.URL)
	assert.True(t, ok)
	assert.Equal(t, 42, tip)
}

func TestFetchHealth_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := testProber(nil, nil, nil, srv.Client())
	tip, ok := p.fetchHealth(srv.URL)
	assert.False(t, ok)
	assert.Equal(t, 0, tip)
}

func TestFetchHealth_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	p := testProber(nil, nil, nil, srv.Client())
	tip, ok := p.fetchHealth(srv.URL)
	assert.False(t, ok)
	assert.Equal(t, 0, tip)
}

func TestFetchHealth_ConnectionRefused(t *testing.T) {
	p := testProber(nil, nil, nil, &http.Client{Timeout: 100 * time.Millisecond})
	tip, ok := p.fetchHealth("http://127.0.0.1:1") // nothing listening
	assert.False(t, ok)
	assert.Equal(t, 0, tip)
}

// --- probeOne tests ---

func testIndexer(httpURL, lastHeartbeat string) store.IndexerRecord {
	return store.IndexerRecord{
		DocID:            "doc-1",
		PeerID:           "peer-1",
		HTTPUrl:          httpURL,
		ReliabilityScore: 0.5,
		LastHeartbeat:    lastHeartbeat,
		Status:           store.StatusActive,
	}
}

func TestProbeOne_SuccessEMA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 100})
	}))
	defer srv.Close()

	pi := &mockProbeInserter{}
	ru := &mockReliabilityUpdater{}
	p := testProber(nil, pi, ru, srv.Client())

	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	p.probeOne(context.Background(), idx, 10*time.Minute)

	require.Len(t, ru.calls, 1)
	// EMA: 0.9 * 0.5 + 0.1 * 1.0 = 0.55
	assert.InDelta(t, 0.55, ru.calls[0].score, 0.001)
	assert.Equal(t, 100, ru.calls[0].tip)
	assert.Equal(t, "", ru.calls[0].status, "active indexer with recent heartbeat should not be marked inactive")
}

func TestProbeOne_FailureEMA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ru := &mockReliabilityUpdater{}
	p := testProber(nil, &mockProbeInserter{}, ru, srv.Client())

	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	p.probeOne(context.Background(), idx, 10*time.Minute)

	require.Len(t, ru.calls, 1)
	// EMA: 0.9 * 0.5 + 0.1 * 0.0 = 0.45
	assert.InDelta(t, 0.45, ru.calls[0].score, 0.001)
}

func TestProbeOne_MarksInactiveOnStaleHeartbeat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 50})
	}))
	defer srv.Close()

	ru := &mockReliabilityUpdater{}
	p := testProber(nil, &mockProbeInserter{}, ru, srv.Client())

	staleTime := time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339)
	idx := testIndexer(srv.URL, staleTime)
	p.probeOne(context.Background(), idx, 10*time.Minute)

	require.Len(t, ru.calls, 1)
	assert.Equal(t, store.StatusInactive, ru.calls[0].status)
}

func TestProbeOne_RecordsProbeResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 77})
	}))
	defer srv.Close()

	pi := &mockProbeInserter{}
	p := testProber(nil, pi, &mockReliabilityUpdater{}, srv.Client())

	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	p.probeOne(context.Background(), idx, 10*time.Minute)

	require.Len(t, pi.inserted, 1)
	assert.True(t, pi.inserted[0].Success)
	assert.Equal(t, "peer-1", pi.inserted[0].IndexerID)
	assert.Equal(t, 77, pi.inserted[0].Tip)

	require.Len(t, pi.pruned, 1)
	assert.Equal(t, "peer-1", pi.pruned[0])
}

func TestProbeOne_InsertError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 1})
	}))
	defer srv.Close()

	pi := &mockProbeInserter{insertErr: fmt.Errorf("db full")}
	ru := &mockReliabilityUpdater{}
	p := testProber(nil, pi, ru, srv.Client())
	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	// should not panic; reliability update still happens
	p.probeOne(context.Background(), idx, 10*time.Minute)
	require.Len(t, ru.calls, 1)
}

func TestProbeOne_PruneError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 1})
	}))
	defer srv.Close()

	pi := &mockProbeInserter{pruneErr: fmt.Errorf("prune fail")}
	ru := &mockReliabilityUpdater{}
	p := testProber(nil, pi, ru, srv.Client())
	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	// prune error should be logged only; probe must complete normally
	p.probeOne(context.Background(), idx, 10*time.Minute)
	require.Len(t, ru.calls, 1)
}

func TestProbeOne_EmptyHeartbeat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 1})
	}))
	defer srv.Close()

	ru := &mockReliabilityUpdater{}
	p := testProber(nil, &mockProbeInserter{}, ru, srv.Client())
	idx := testIndexer(srv.URL, "") // empty heartbeat
	p.probeOne(context.Background(), idx, 10*time.Minute)
	require.Len(t, ru.calls, 1)
	assert.Equal(t, "", ru.calls[0].status, "empty heartbeat should not trigger inactive transition")
}

func TestProbeAll_ListError(t *testing.T) {
	ru := &mockReliabilityUpdater{}
	lister := &mockIndexerLister{err: fmt.Errorf("store unavailable")}
	p := testProber(lister, &mockProbeInserter{}, ru, &http.Client{})
	// should log warn and return without panicking
	p.probeAll(context.Background())
	assert.Len(t, ru.calls, 0)
}

func TestProbeAll_ProbesAllIndexers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 5})
	}))
	defer srv.Close()

	ch := make(chan struct{}, 2)
	ru := &signalingReliabilityUpdater{ch: ch}

	lister := &mockIndexerLister{records: []store.IndexerRecord{
		testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339)),
		{DocID: "doc-2", PeerID: "peer-2", HTTPUrl: srv.URL, Status: store.StatusActive,
			LastHeartbeat: time.Now().UTC().Format(time.RFC3339)},
	}}
	p := testProber(lister, &mockProbeInserter{}, ru, srv.Client())
	p.probeAll(context.Background())

	// Wait for both goroutines to finish.
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for probe goroutines")
		}
	}
}

func TestStart_Stop(t *testing.T) {
	ru := &mockReliabilityUpdater{}
	lister := &mockIndexerLister{}
	p := testProber(lister, &mockProbeInserter{}, ru, &http.Client{})
	// Override to a very short interval to ensure at least one tick fires.
	p.cfg.IntervalSeconds = 1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p.Start(ctx)
	// Give the loop goroutine time to start and then stop cleanly.
	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() deadlocked")
	}
}

// --- contradiction recorder mock ---

type mockContradictionRecorder struct {
	records []*store.ContradictionRecord
	err     error
}

func (m *mockContradictionRecorder) Create(_ context.Context, r *store.ContradictionRecord) (*store.ContradictionRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.records = append(m.records, r)
	return r, nil
}

// --- WithContradictionRecorder tests ---

func TestWithContradictionRecorder_SetsField(t *testing.T) {
	cr := &mockContradictionRecorder{}
	p := testProber(&mockIndexerLister{}, &mockProbeInserter{}, &mockReliabilityUpdater{}, &http.Client{})
	assert.Nil(t, p.contradictionSt)

	p.WithContradictionRecorder(cr)
	assert.Equal(t, cr, p.contradictionSt)
}

// --- probeOne contradiction recording ---

func TestProbeOne_RecordsContradiction_WhenProbeFailsAndSnapshotsExist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cr := &mockContradictionRecorder{}
	ru := &mockReliabilityUpdater{}
	p := testProber(nil, &mockProbeInserter{}, ru, srv.Client())
	p.WithContradictionRecorder(cr)

	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	idx.SnapshotRanges = `[{"start":1,"end":100}]`
	p.probeOne(context.Background(), idx, 10*time.Minute)

	require.Len(t, cr.records, 1)
	assert.Equal(t, "peer-1", cr.records[0].IndexerID)
	assert.Equal(t, idx.SnapshotRanges, cr.records[0].SnapshotRange)
	assert.False(t, cr.records[0].Resolved)
}

func TestProbeOne_NoContradiction_WhenProbeFailsButNoSnapshots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cr := &mockContradictionRecorder{}
	ru := &mockReliabilityUpdater{}
	p := testProber(nil, &mockProbeInserter{}, ru, srv.Client())
	p.WithContradictionRecorder(cr)

	// Empty snapshot ranges — no contradiction should be recorded.
	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	idx.SnapshotRanges = ""
	p.probeOne(context.Background(), idx, 10*time.Minute)
	assert.Empty(t, cr.records)

	// Also test with "[]".
	idx.SnapshotRanges = "[]"
	p.probeOne(context.Background(), idx, 10*time.Minute)
	assert.Empty(t, cr.records)
}

func TestProbeOne_NoContradiction_WhenProbeSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 50})
	}))
	defer srv.Close()

	cr := &mockContradictionRecorder{}
	ru := &mockReliabilityUpdater{}
	p := testProber(nil, &mockProbeInserter{}, ru, srv.Client())
	p.WithContradictionRecorder(cr)

	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	idx.SnapshotRanges = `[{"start":1,"end":100}]`
	p.probeOne(context.Background(), idx, 10*time.Minute)

	assert.Empty(t, cr.records, "successful probe should not record contradiction")
}

type failingReliabilityUpdater struct {
	err error
}

func (m *failingReliabilityUpdater) UpdateReliability(_ context.Context, _ string, _ float64, _ int, _ string) error {
	return m.err
}

func TestProbeOne_UpdateReliabilityError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 10})
	}))
	defer srv.Close()

	ru := &failingReliabilityUpdater{err: fmt.Errorf("update failed")}
	p := testProber(nil, &mockProbeInserter{}, ru, srv.Client())
	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))

	// Should not panic; error is logged.
	p.probeOne(context.Background(), idx, 10*time.Minute)
}

func TestRecordContradiction_StoreCreateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cr := &mockContradictionRecorder{err: fmt.Errorf("db write failed")}
	ru := &mockReliabilityUpdater{}
	p := testProber(nil, &mockProbeInserter{}, ru, srv.Client())
	p.WithContradictionRecorder(cr)

	idx := testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339))
	idx.SnapshotRanges = `[{"start":1,"end":100}]`

	// Should not panic; error is logged.
	p.probeOne(context.Background(), idx, 10*time.Minute)
	assert.Empty(t, cr.records)
	require.Len(t, ru.calls, 1) // reliability update still happens
}

func TestNewProber(t *testing.T) {
	log, _ := zap.NewDevelopment()
	cfg := config.ProbeConfig{
		IntervalSeconds:      60,
		TimeoutSeconds:       2,
		TipLagThreshold:      10,
		InactiveAfterMinutes: 10,
		ProbeHistoryLimit:    200,
		MaxConcurrentProbes:  5,
	}
	idxReg := registry.NewIndexerRegistry(nil, nil, log.Sugar(), "eth", "mainnet", 60)
	idxSt := store.NewIndexerStore(nil)
	probeSt := store.NewProbeStore(nil)

	p := NewProber(cfg, idxReg, idxSt, probeSt, log.Sugar())
	require.NotNil(t, p)
	assert.Equal(t, cfg, p.cfg)
	assert.NotNil(t, p.httpClient)
	assert.NotNil(t, p.sem)
}

func TestNewProber_DefaultMaxConcurrent(t *testing.T) {
	log, _ := zap.NewDevelopment()
	cfg := config.ProbeConfig{
		IntervalSeconds:     60,
		TimeoutSeconds:      2,
		MaxConcurrentProbes: 0, // triggers default of 20
	}
	p := NewProber(cfg, registry.NewIndexerRegistry(nil, nil, log.Sugar(), "", "", 0),
		store.NewIndexerStore(nil), store.NewProbeStore(nil), log.Sugar())
	require.NotNil(t, p)
	assert.Equal(t, 20, cap(p.sem))
}

func TestStart_TickerFiresProbeAll(t *testing.T) {
	// Use a signaling updater to detect when probeAll runs.
	ch := make(chan struct{}, 1)
	ru := &signalingReliabilityUpdater{ch: ch}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{CurrentBlock: 1})
	}))
	defer srv.Close()

	lister := &mockIndexerLister{records: []store.IndexerRecord{
		testIndexer(srv.URL, time.Now().UTC().Format(time.RFC3339)),
	}}
	p := testProber(lister, &mockProbeInserter{}, ru, srv.Client())
	p.cfg.IntervalSeconds = 1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	select {
	case <-ch:
		// probeAll fired via ticker
	case <-time.After(3 * time.Second):
		t.Fatal("ticker did not fire probeAll")
	}
	p.Stop()
}

func TestStart_ContextCancel(t *testing.T) {
	p := testProber(&mockIndexerLister{}, &mockProbeInserter{}, &mockReliabilityUpdater{}, &http.Client{})
	p.cfg.IntervalSeconds = 60

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)

	done := make(chan struct{})
	go func() {
		cancel()
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine did not exit after context cancel")
	}
}
