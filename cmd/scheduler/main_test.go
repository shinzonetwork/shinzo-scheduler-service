package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	sdkconfig "github.com/shinzonetwork/shinzo-app-sdk/pkg/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBuildLogger_Development(t *testing.T) {
	log := buildLogger(true)
	assert.NotNil(t, log)
}

func TestBuildLogger_Production(t *testing.T) {
	log := buildLogger(false)
	assert.NotNil(t, log)
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	tmpDir := t.TempDir()
	return &config.Config{
		Config: sdkconfig.Config{
			DefraDB: sdkconfig.DefraDBConfig{
				Url:           "localhost:0",
				KeyringSecret: "test-keyring-secret",
				P2P:           sdkconfig.DefraP2PConfig{Enabled: false},
				Store:         sdkconfig.DefraStoreConfig{Path: tmpDir + "/defradb"},
			},
			Logger: sdkconfig.LoggerConfig{Development: true},
		},
		Scheduler: config.SchedulerConfig{
			Chain:   "ethereum",
			Network: "testnet",
			Server: config.ServerConfig{
				Port:                0, // random port
				ReadTimeoutSeconds:  5,
				WriteTimeoutSeconds: 5,
			},
			Probe: config.ProbeConfig{
				IntervalSeconds:          3600, // very long so it doesn't fire during tests
				TimeoutSeconds:           5,
				TipLagThreshold:          10,
				TipExclusionThreshold:    50,
				StalenessWindowSeconds:   120,
				InactiveAfterMinutes:     10,
				ProbeHistoryLimit:        200,
				MaxConcurrentProbes:      5,
				HeartbeatIntervalSeconds: 30,
			},
			Auth: config.AuthConfig{HMACSecret: "test-hmac-secret"},
			Diversity: config.DiversityConfig{
				Enabled:            true,
				RecencyWindowHours: 24,
			},
			Accounting: config.AccountingConfig{
				ComparisonIntervalSeconds: 3600,
				AttestationWindowSeconds:  300,
				UnderReportGraceSeconds:   600,
			},
			Settlement: config.SettlementConfig{
				DrainIntervalSeconds:   3600,
				LowCreditMultiplier:    2.0,
				GracePeriodSeconds:     3600,
				SettlementWindowBlocks: 100,
				VerdictThresholdM:      1,
				VerdictThresholdN:      1,
			},
		},
	}
}

func testLogger() *zap.SugaredLogger {
	log, _ := zap.NewDevelopment()
	return log.Sugar()
}

func TestRun_StartupAndShutdown(t *testing.T) {
	cfg := testConfig(t)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, testLogger())
	}()

	// Wait for server to start, then cancel.
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("run did not exit after context cancel")
	}
}

func TestRun_WithBootstrapPeer(t *testing.T) {
	cfg := testConfig(t)
	cfg.Scheduler.Bootstrap.Indexers = []config.BootstrapIndexer{
		{PeerID: "QmBootstrap1", HTTPUrl: "http://localhost:9999", Multiaddr: "/ip4/127.0.0.1/tcp/4001"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, testLogger())
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("run did not exit after context cancel")
	}
}

func TestRun_WithAccountingEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.Scheduler.Accounting.Enabled = true

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, testLogger())
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("run did not exit")
	}
}

func TestRun_WithSettlementEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.Scheduler.Settlement.Enabled = true

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, testLogger())
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("run did not exit")
	}
}

func TestRun_HealthEndpointResponds(t *testing.T) {
	cfg := testConfig(t)
	// Use a known port to be able to query it.
	cfg.Scheduler.Server.Port = 18999

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, testLogger())
	}()

	// Wait for server to start.
	time.Sleep(1 * time.Second)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/v1/health", cfg.Scheduler.Server.Port))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("run did not exit")
	}
}

func TestRun_WithShinzoHubEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.Scheduler.ShinzoHub.Enabled = true
	cfg.Scheduler.ShinzoHub.RPCURL = "ws://127.0.0.1:1" // unreachable, should warn and continue

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, testLogger())
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err) // should not fail, just warn
	case <-time.After(15 * time.Second):
		t.Fatal("run did not exit")
	}
}

func TestRun_AllSubsystemsEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.Scheduler.Accounting.Enabled = true
	cfg.Scheduler.Settlement.Enabled = true

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, testLogger())
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("run did not exit")
	}
}
