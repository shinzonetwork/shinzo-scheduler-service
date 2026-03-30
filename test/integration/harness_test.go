//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	sdkconfig "github.com/shinzonetwork/shinzo-app-sdk/pkg/config"
	"github.com/shinzonetwork/shinzo-app-sdk/pkg/defra"
	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/accounting"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/handlers"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/auth"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/discovery"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/probe"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	schedulerschema "github.com/shinzonetwork/shinzo-scheduler-service/pkg/schema"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/settlement"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	subpkg "github.com/shinzonetwork/shinzo-scheduler-service/pkg/subscription"
	"github.com/sourcenetwork/defradb/client"
	"github.com/sourcenetwork/defradb/client/options"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	testChain   = "cosmos"
	testNetwork = "testnet"
)

// dbClient matches the store layer's interface for DefraDB operations.
type dbClient interface {
	ExecRequest(ctx context.Context, request string, opts ...options.Enumerable[options.ExecRequestOptions]) *client.RequestResult
}

// testHarness encapsulates a running scheduler server and all supporting infrastructure.
type testHarness struct {
	baseURL  string
	cfg      *config.Config
	peerKeys map[string]*secp256k1.PrivateKey // peerID → signing key for token generation

	indexerSt *store.IndexerStore
	hostSt    *store.HostStore
	subSt     *store.SubscriptionStore
	matchSt   *store.MatchStore

	// Full harness fields (nil in basic harness).
	acctMgr    *accounting.Manager
	escrowMgr  *settlement.EscrowManager
	batchProc  *settlement.BatchProcessor
	verdictMgr *settlement.VerdictManager
	hub        *recordingBroadcaster
	escrowSt   *store.EscrowStore
	ledgerSt   *store.LedgerStore
	settleSt   *store.SettlementStore
	verdictSt  *store.VerdictStore

	cleanup func()
}

// recordingBroadcaster captures hub broadcasts for assertions.
type recordingBroadcaster struct {
	mu             sync.Mutex
	lowCreditCalls []settlement.MsgSignalLowCredit
	batchCalls     []settlement.MsgBatchSettlement
	closeCalls     []settlement.MsgCloseSession
	slashCalls     []settlement.MsgSlash
}

func (r *recordingBroadcaster) BroadcastCloseSession(_ context.Context, msg settlement.MsgCloseSession) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCalls = append(r.closeCalls, msg)
	return "tx-close", nil
}

func (r *recordingBroadcaster) BroadcastBatchSettlement(_ context.Context, msg settlement.MsgBatchSettlement) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batchCalls = append(r.batchCalls, msg)
	return "tx-batch", nil
}

func (r *recordingBroadcaster) BroadcastLowCredit(_ context.Context, msg settlement.MsgSignalLowCredit) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lowCreditCalls = append(r.lowCreditCalls, msg)
	return "tx-low-credit", nil
}

func (r *recordingBroadcaster) BroadcastSlash(_ context.Context, msg settlement.MsgSlash) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slashCalls = append(r.slashCalls, msg)
	return "tx-slash", nil
}

func setupDefra(t *testing.T) (dbClient, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &sdkconfig.Config{
		DefraDB: sdkconfig.DefraDBConfig{
			KeyringSecret: "test-secret",
			P2P:           sdkconfig.DefraP2PConfig{Enabled: false},
			Store:         sdkconfig.DefraStoreConfig{Path: tmpDir + "/defradb"},
		},
	}
	ctx := context.Background()
	cl, err := defra.NewClient(cfg)
	require.NoError(t, err)
	require.NoError(t, cl.Start(ctx))
	require.NoError(t, cl.ApplySchema(ctx, schedulerschema.GraphQL))
	node := cl.GetNode()
	cleanup := func() { _ = cl.Stop(context.Background()) }
	return node.DB, cleanup
}

func testAccountingCfg() config.AccountingConfig {
	return config.AccountingConfig{
		Enabled:                   true,
		ComparisonIntervalSeconds: 30,
		AttestationWindowSeconds:  0,
		UnderReportGraceSeconds:   600,
	}
}

func testSettlementCfg() config.SettlementConfig {
	return config.SettlementConfig{
		Enabled:                true,
		DrainIntervalSeconds:   60,
		LowCreditMultiplier:    2.0,
		GracePeriodSeconds:     3600,
		SettlementWindowBlocks: 100,
		VerdictThresholdM:      2,
		VerdictThresholdN:      3,
	}
}

func newAccountingManager(db dbClient, cfg config.AccountingConfig) *accounting.Manager {
	log := zap.NewNop().Sugar()
	claimSt := store.NewClaimStore(db)
	attestSt := store.NewAttestationStore(db)
	ledgerSt := store.NewLedgerStore(db)
	compSt := store.NewComparisonStore(db)
	return accounting.NewManager(claimSt, attestSt, ledgerSt, compSt, cfg, log)
}

func cidJSON(cids ...string) string {
	b, _ := json.Marshal(cids)
	return string(b)
}

func baseSchedulerConfig() config.SchedulerConfig {
	return config.SchedulerConfig{
		Chain:   testChain,
		Network: testNetwork,
		Server: config.ServerConfig{
			Port:                0,
			ReadTimeoutSeconds:  30,
			WriteTimeoutSeconds: 30,
		},
		Probe: config.ProbeConfig{
			IntervalSeconds:          60,
			TimeoutSeconds:           5,
			TipLagThreshold:          10,
			TipExclusionThreshold:    50,
			StalenessWindowSeconds:   120,
			InactiveAfterMinutes:     10,
			ProbeHistoryLimit:        200,
			MaxConcurrentProbes:      20,
			HeartbeatIntervalSeconds: 30,
		},
		Diversity: config.DiversityConfig{
			Enabled:            true,
			RecencyWindowHours: 24,
		},
		ShinzoHub:  config.ShinzoHubConfig{Enabled: false},
		Accounting: config.AccountingConfig{Enabled: false},
		Settlement: config.SettlementConfig{Enabled: false},
	}
}

// buildHarness is the shared implementation for both newHarness and newFullHarness.
func buildHarness(t *testing.T, enableAccounting, enableSettlement bool) *testHarness {
	t.Helper()
	ctx := context.Background()

	tmpDir := t.TempDir()
	sdkCfg := &sdkconfig.Config{
		DefraDB: sdkconfig.DefraDBConfig{
			KeyringSecret: "test-secret",
			P2P:           sdkconfig.DefraP2PConfig{Enabled: false},
			Store:         sdkconfig.DefraStoreConfig{Path: tmpDir + "/defradb"},
		},
	}

	cl, err := defra.NewClient(sdkCfg)
	require.NoError(t, err)
	require.NoError(t, cl.Start(ctx))
	require.NoError(t, cl.ApplySchema(ctx, schedulerschema.GraphQL))
	defraNode := cl.GetNode()

	cfg := &config.Config{Scheduler: baseSchedulerConfig()}
	sc := cfg.Scheduler

	// Wire store layer.
	indexerSt := store.NewIndexerStore(defraNode.DB)
	hostSt := store.NewHostStore(defraNode.DB)
	subSt := store.NewSubscriptionStore(defraNode.DB)
	probeSt := store.NewProbeStore(defraNode.DB)
	matchSt := store.NewMatchStore(defraNode.DB)

	// Wire auth + registries.
	verifier := auth.NewVerifier()
	log := zap.NewNop().Sugar()
	indexerReg := registry.NewIndexerRegistry(indexerSt, verifier, log, sc.Chain, sc.Network, sc.Probe.HeartbeatIntervalSeconds)
	hostReg := registry.NewHostRegistry(hostSt, verifier, log, sc.Chain, sc.Network, sc.Probe.HeartbeatIntervalSeconds)

	// Wire discovery.
	disc := discovery.NewDiscoverer(indexerSt, matchSt, discovery.DiscovererConfig{
		StalenessWindow:       time.Duration(sc.Probe.StalenessWindowSeconds) * time.Second,
		TipExclusionThreshold: sc.Probe.TipExclusionThreshold,
		DiversityEnabled:      sc.Diversity.Enabled,
		RecencyWindow:         time.Duration(sc.Diversity.RecencyWindowHours) * time.Hour,
	})

	// Wire subscription manager.
	subMgr := subpkg.NewManager(subSt, indexerSt, log)
	subMgr.WithMatchRecorder(matchSt)

	// Wire prober.
	contradictionSt := store.NewContradictionStore(defraNode.DB)
	prober := probe.NewProber(sc.Probe, indexerReg, indexerSt, probeSt, log)
	prober.WithContradictionRecorder(contradictionSt)

	// Wire handlers.
	indexerH := handlers.NewIndexerHandler(indexerReg, indexerSt)
	hostH := handlers.NewHostHandler(hostReg, hostSt)
	discoveryH := handlers.NewDiscoveryHandler(disc)
	subscriptionH := handlers.NewSubscriptionHandler(subMgr, hostReg)
	paymentH := handlers.NewPaymentHandler(subMgr, indexerSt, hostReg)
	healthH := handlers.NewHealthHandler(indexerSt, hostSt, subSt)
	metricsH := handlers.NewMetricsHandler(indexerSt, hostSt, subSt, probeSt, log)

	h := &testHarness{
		cfg:       cfg,
		peerKeys:  make(map[string]*secp256k1.PrivateKey),
		indexerSt: indexerSt,
		hostSt:    hostSt,
		subSt:     subSt,
		matchSt:   matchSt,
	}

	// Wire optional subsystems.
	var accountingH *handlers.AccountingHandler
	var settlementH *handlers.SettlementHandler

	if enableAccounting {
		claimSt := store.NewClaimStore(defraNode.DB)
		attestSt := store.NewAttestationStore(defraNode.DB)
		ledgerSt := store.NewLedgerStore(defraNode.DB)
		compSt := store.NewComparisonStore(defraNode.DB)
		acctCfg := testAccountingCfg()
		acctMgr := accounting.NewManager(claimSt, attestSt, ledgerSt, compSt, acctCfg, log)
		accountingH = handlers.NewAccountingHandler(acctMgr)
		h.acctMgr = acctMgr
		h.ledgerSt = ledgerSt
	}

	if enableSettlement {
		hub := &recordingBroadcaster{}
		escrowSt := store.NewEscrowStore(defraNode.DB)
		ledgerSt := store.NewLedgerStore(defraNode.DB)
		settleSt := store.NewSettlementStore(defraNode.DB)
		verdictSt := store.NewVerdictStore(defraNode.DB)
		settleCfg := testSettlementCfg()

		escrowMgr := settlement.NewEscrowManager(escrowSt, ledgerSt, hub, settleCfg, log)
		batchProc := settlement.NewBatchProcessor(settleSt, escrowSt, ledgerSt, hub, log)
		verdictMgr := settlement.NewVerdictManager(verdictSt, hub, settleCfg, log)

		settlementH = handlers.NewSettlementHandler(escrowSt, settleSt, verdictSt)
		h.hub = hub
		h.escrowMgr = escrowMgr
		h.batchProc = batchProc
		h.verdictMgr = verdictMgr
		h.escrowSt = escrowSt
		h.settleSt = settleSt
		h.verdictSt = verdictSt
		if h.ledgerSt == nil {
			h.ledgerSt = ledgerSt
		}
	}

	// Pick a random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	actualAddr := ln.Addr().String()
	cfg.Scheduler.Server.Port = ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv := api.NewServer(
		cfg.Scheduler.Server,
		indexerH, hostH, discoveryH, subscriptionH, paymentH, healthH, metricsH,
		accountingH, settlementH,
		log,
	)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	baseURL := fmt.Sprintf("http://%s", actualAddr)
	require.Eventually(t, func() bool {
		resp, err := http.Get(baseURL + "/v1/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 10*time.Second, 50*time.Millisecond, "server did not become ready")

	h.baseURL = baseURL
	h.cleanup = func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		_ = cl.Stop(context.Background())
	}
	t.Cleanup(h.close)
	return h
}

// newHarness boots a scheduler without accounting/settlement.
func newHarness(t *testing.T) *testHarness {
	return buildHarness(t, false, false)
}

// newFullHarness boots a scheduler with accounting + settlement enabled.
func newFullHarness(t *testing.T) *testHarness {
	return buildHarness(t, true, true)
}

func (h *testHarness) close() {
	if h.cleanup != nil {
		h.cleanup()
		h.cleanup = nil
	}
}

// doRequest makes an HTTP request against the harness server.
func (h *testHarness) doRequest(t *testing.T, method, path string, body any, apiKey string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.baseURL+path, bodyReader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// registerIndexer seeds an indexer directly in DefraDB and returns a fresh auth token.
func (h *testHarness) registerIndexer(t *testing.T, peerID string) string {
	t.Helper()
	ctx := context.Background()

	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	pubHex := hex.EncodeToString(priv.PubKey().SerializeCompressed())
	h.peerKeys[peerID] = priv

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = h.indexerSt.Create(ctx, &store.IndexerRecord{
		PeerID:           peerID,
		DefraPK:          pubHex,
		HTTPUrl:          "http://" + peerID + ":8080",
		Multiaddr:        "/ip4/127.0.0.1/tcp/9171/p2p/" + peerID,
		Chain:            testChain,
		Network:          testNetwork,
		SnapshotRanges:   "[]",
		ReliabilityScore: 1.0,
		LastHeartbeat:    now,
		RegisteredAt:     now,
		Status:           store.StatusActive,
	})
	require.NoError(t, err)
	return auth.GenerateToken(priv, peerID)
}

// registerIndexerWithOpts seeds an indexer with caller-specified fields.
func (h *testHarness) registerIndexerWithOpts(t *testing.T, rec *store.IndexerRecord) string {
	t.Helper()
	ctx := context.Background()

	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	pubHex := hex.EncodeToString(priv.PubKey().SerializeCompressed())
	h.peerKeys[rec.PeerID] = priv
	rec.DefraPK = pubHex

	if rec.Chain == "" {
		rec.Chain = testChain
	}
	if rec.Network == "" {
		rec.Network = testNetwork
	}
	if rec.Status == "" {
		rec.Status = store.StatusActive
	}
	if rec.SnapshotRanges == "" {
		rec.SnapshotRanges = "[]"
	}
	if rec.LastHeartbeat == "" {
		rec.LastHeartbeat = time.Now().UTC().Format(time.RFC3339)
	}
	if rec.RegisteredAt == "" {
		rec.RegisteredAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err = h.indexerSt.Create(ctx, rec)
	require.NoError(t, err)
	return auth.GenerateToken(priv, rec.PeerID)
}

// registerHost seeds a host directly in DefraDB and returns a fresh auth token.
func (h *testHarness) registerHost(t *testing.T, peerID string) string {
	t.Helper()
	ctx := context.Background()

	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	pubHex := hex.EncodeToString(priv.PubKey().SerializeCompressed())
	h.peerKeys[peerID] = priv

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = h.hostSt.Create(ctx, &store.HostRecord{
		PeerID:        peerID,
		DefraPK:       pubHex,
		HTTPUrl:       "http://" + peerID + ":8080",
		Multiaddr:     "/ip4/127.0.0.1/tcp/9171/p2p/" + peerID,
		Chain:         testChain,
		Network:       testNetwork,
		LastHeartbeat: now,
		RegisteredAt:  now,
		Status:        store.StatusActive,
	})
	require.NoError(t, err)
	return auth.GenerateToken(priv, peerID)
}

// setupSession creates a subscription via HTTP, activates it, and initializes
// a session ledger and escrow. Returns the subscription ID (used as session ID).
func (h *testHarness) setupSession(t *testing.T, hostKey, indexerKey, hostID, indexerID string, initialEscrow, pricePerBlock float64) string {
	t.Helper()
	ctx := context.Background()

	// Create subscription.
	resp := h.doRequest(t, "POST", "/v1/subscriptions", map[string]any{
		"host_id":    hostID,
		"indexer_id": indexerID,
		"sub_type":   "tip",
	}, hostKey)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var subResp map[string]any
	decodeJSON(t, resp, &subResp)
	subID := subResp["subscription_id"].(string)

	// Activate via payment verify.
	resp = h.doRequest(t, "POST", "/v1/payments/verify", map[string]any{
		"subscription_id": subID,
		"payment_ref":     "test-payment-ref",
		"expires_at":      time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}, hostKey)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Initialize session ledger.
	if h.acctMgr != nil {
		require.NoError(t, h.acctMgr.CreateSessionLedger(ctx, subID, hostID, indexerID, initialEscrow, pricePerBlock))
	}

	// Initialize escrow.
	if h.escrowMgr != nil {
		require.NoError(t, h.escrowMgr.CreateEscrow(ctx, subID, hostID, indexerID, initialEscrow, pricePerBlock))
	}

	return subID
}

// submitCleanBlock submits a matching claim + attestation and runs comparison.
func (h *testHarness) submitCleanBlock(t *testing.T, sessionID, indexerID, hostID string, blockN int) {
	t.Helper()
	cids := cidJSON(fmt.Sprintf("cid-%s-%d", sessionID, blockN))
	h.submitBlock(t, sessionID, indexerID, hostID, blockN, cids, cids)
}

// submitBlock submits a claim + attestation with specified CIDs and runs comparison.
func (h *testHarness) submitBlock(t *testing.T, sessionID, indexerID, hostID string, blockN int, claimCids, attestCids string) *accounting.ComparisonResult {
	t.Helper()
	ctx := context.Background()
	require.NotNil(t, h.acctMgr, "accounting manager required for submitBlock")

	_, err := h.acctMgr.SubmitDeliveryClaim(ctx, accounting.SubmitClaimRequest{
		SessionID: sessionID, IndexerID: indexerID, BlockNumber: blockN,
		DocCids: claimCids, BlockHash: fmt.Sprintf("0x%s-%d", sessionID, blockN),
	})
	require.NoError(t, err)

	_, err = h.acctMgr.SubmitAttestation(ctx, accounting.SubmitAttestationRequest{
		SessionID: sessionID, HostID: hostID, BlockNumber: blockN,
		DocCidsReceived: attestCids,
	})
	require.NoError(t, err)

	result, err := h.acctMgr.Compare(ctx, sessionID, blockN)
	require.NoError(t, err)
	return result
}

// decodeJSON reads and unmarshals the response body into dst.
func decodeJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, dst), "body: %s", string(b))
}
