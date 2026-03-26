package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shinzonetwork/shinzo-app-sdk/pkg/defra"
	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/accounting"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/handlers"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/auth"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/discovery"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/payment"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/probe"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	schedulerschema "github.com/shinzonetwork/shinzo-scheduler-service/pkg/schema"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/settlement"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	subpkg "github.com/shinzonetwork/shinzo-scheduler-service/pkg/subscription"
	"go.uber.org/zap"
)

func main() {
	cfgPath := flag.String("config", "config/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	log := buildLogger(cfg.Logger.Development)
	defer func() { _ = log.Sync() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, log); err != nil {
		log.Fatalf("scheduler: %v", err)
	}
}

// run contains all scheduler initialization and lifecycle management.
// Extracted from main() for testability.
func run(ctx context.Context, cfg *config.Config, log *zap.SugaredLogger) error {
	client, err := defra.NewClient(&cfg.Config)
	if err != nil {
		return fmt.Errorf("create defra client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start defra: %w", err)
	}
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		if err := client.Stop(shutCtx); err != nil {
			log.Warnf("defra stop: %v", err)
		}
	}()

	if cfg.DefraDB.P2P.Enabled {
		log.Info("defradb P2P replication enabled")
	}

	if err := client.ApplySchema(ctx, schedulerschema.GraphQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	log.Info("schema applied")

	defraNode := client.GetNode()

	// Wire store layer.
	indexerSt := store.NewIndexerStore(defraNode.DB)
	hostSt := store.NewHostStore(defraNode.DB)
	subSt := store.NewSubscriptionStore(defraNode.DB)
	probeSt := store.NewProbeStore(defraNode.DB)
	matchSt := store.NewMatchStore(defraNode.DB)

	// Seed bootstrap peers.
	sc := cfg.Scheduler
	bootstrapVerifier := auth.NewVerifier(sc.Auth.HMACSecret)
	for _, bi := range sc.Bootstrap.Indexers {
		if existing, _ := indexerSt.GetByPeerID(ctx, bi.PeerID); existing != nil {
			continue
		}
		_, keyHash, err := bootstrapVerifier.IssueAPIKey(bi.PeerID)
		if err != nil {
			log.Warnf("bootstrap peer %s: issue key: %v", bi.PeerID, err)
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := indexerSt.Create(ctx, &store.IndexerRecord{
			PeerID:           bi.PeerID,
			HTTPUrl:          bi.HTTPUrl,
			Multiaddr:        bi.Multiaddr,
			Chain:            sc.Chain,
			Network:          sc.Network,
			SnapshotRanges:   "[]",
			ReliabilityScore: 1.0,
			LastHeartbeat:    now,
			RegisteredAt:     now,
			Status:           store.StatusActive,
			APIKeyHash:       keyHash,
		}); err != nil {
			log.Warnf("bootstrap peer %s: create record: %v", bi.PeerID, err)
			continue
		}
		log.Infof("bootstrap indexer seeded: peer_id=%s", bi.PeerID)
	}

	// Wire auth + registries.
	verifier := auth.NewVerifier(sc.Auth.HMACSecret)
	heartbeatInterval := sc.Probe.HeartbeatIntervalSeconds
	indexerReg := registry.NewIndexerRegistry(indexerSt, verifier, log, sc.Chain, sc.Network, heartbeatInterval)
	hostReg := registry.NewHostRegistry(hostSt, verifier, log, sc.Chain, sc.Network, heartbeatInterval)

	// Wire business logic.
	disc := discovery.NewDiscoverer(indexerSt, matchSt, discovery.DiscovererConfig{
		StalenessWindow:       time.Duration(sc.Probe.StalenessWindowSeconds) * time.Second,
		TipExclusionThreshold: sc.Probe.TipExclusionThreshold,
		DiversityEnabled:      sc.Diversity.Enabled,
		RecencyWindow:         time.Duration(sc.Diversity.RecencyWindowHours) * time.Hour,
	})
	subMgr := subpkg.NewManager(subSt, indexerSt, log)
	subMgr.WithMatchRecorder(matchSt)
	contradictionSt := store.NewContradictionStore(defraNode.DB)
	prober := probe.NewProber(sc.Probe, indexerReg, indexerSt, probeSt, log)
	prober.WithContradictionRecorder(contradictionSt)

	// Wire handlers.
	indexerH := handlers.NewIndexerHandler(indexerReg, indexerSt)
	hostH := handlers.NewHostHandler(hostReg, hostSt)
	discoveryH := handlers.NewDiscoveryHandler(disc)
	subscriptionH := handlers.NewSubscriptionHandler(subMgr, hostReg)
	paymentH := handlers.NewPaymentHandler(subMgr, indexerSt, hostReg)
	paymentH.WithFloorPricing(sc.Pricing.FloorTipPer1kBlocks, sc.Pricing.FloorSnapshotPerRange)
	healthH := handlers.NewHealthHandler(indexerSt, hostSt, subSt)
	metricsH := handlers.NewMetricsHandler(indexerSt, hostSt, subSt, probeSt, log)
	authH := handlers.NewAuthHandler(verifier, indexerSt, hostSt, log)

	// Start background workers.
	prober.Start(ctx)
	subMgr.StartExpiryLoop(ctx)

	// Optionally wire ShinzoHub subscriber.
	if sc.ShinzoHub.Enabled {
		paymentH.WithTxVerifier(payment.NewClient(sc.ShinzoHub.RPCURL))

		hub := payment.NewShinzoHubSubscriber(sc.ShinzoHub.RPCURL, sc.ShinzoHub.EpochSize, log)
		hub.OnSubscriptionCreated = func(ev payment.SubscriptionCreatedEvent) {
			activateCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			if err := subMgr.Activate(activateCtx, subpkg.ActivateRequest{
				SubscriptionID: ev.SubscriptionID,
				PaymentRef:     "shinzohub:on-chain",
				ExpiresAt:      ev.ExpiresAt,
			}); err != nil {
				log.Warnf("shinzohub activate %s: %v", ev.SubscriptionID, err)
			}
		}
		hub.OnSubscriptionExpired = func(ev payment.SubscriptionExpiredEvent) {
			expCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			if err := subMgr.Cancel(expCtx, ev.SubscriptionID); err != nil {
				log.Warnf("shinzohub expire %s: %v", ev.SubscriptionID, err)
			}
		}
		if err := hub.Start(ctx); err != nil {
			log.Warnf("shinzohub subscriber: %v (continuing without it)", err)
		}
	}

	// Wire accounting subsystem.
	var accountingH *handlers.AccountingHandler
	if sc.Accounting.Enabled {
		claimSt := store.NewClaimStore(defraNode.DB)
		attestSt := store.NewAttestationStore(defraNode.DB)
		ledgerSt := store.NewLedgerStore(defraNode.DB)
		compSt := store.NewComparisonStore(defraNode.DB)
		acctMgr := accounting.NewManager(claimSt, attestSt, ledgerSt, compSt, sc.Accounting, log)
		acctMgr.StartComparisonLoop(ctx)
		accountingH = handlers.NewAccountingHandler(acctMgr)
		log.Info("accounting subsystem enabled")
	}

	// Wire settlement subsystem.
	var settlementH *handlers.SettlementHandler
	if sc.Settlement.Enabled {
		escrowSt := store.NewEscrowStore(defraNode.DB)
		settleSt := store.NewSettlementStore(defraNode.DB)
		verdictSt := store.NewVerdictStore(defraNode.DB)
		settlementH = handlers.NewSettlementHandler(escrowSt, settleSt, verdictSt)

		var broadcaster settlement.HubBroadcaster
		if sc.ShinzoHub.Enabled && sc.ShinzoHub.RPCURL != "" {
			broadcaster = settlement.NewTendermintBroadcaster(sc.ShinzoHub.RPCURL, "scheduler-1", log)
			log.Info("settlement broadcaster: tendermint RPC")
		} else {
			broadcaster = settlement.NoopBroadcaster{}
			log.Info("settlement broadcaster: noop (hub disabled)")
		}
		aggr := settlement.NewContradictionAggregator(contradictionSt, broadcaster, sc.Settlement, log)
		aggr.Start(ctx)

		log.Info("settlement subsystem enabled")
	}

	srv := api.NewServer(
		sc.Server,
		indexerH, hostH, discoveryH, subscriptionH, paymentH, healthH, metricsH, authH,
		accountingH, settlementH,
		log,
	)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		log.Info("shutting down")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutCancel()
		prober.Stop()
		subMgr.Stop()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Warnf("http shutdown: %v", err)
		}
	}
	return nil
}

func buildLogger(dev bool) *zap.SugaredLogger {
	var base *zap.Logger
	var err error
	if dev {
		base, err = zap.NewDevelopment()
	} else {
		base, err = zap.NewProduction()
	}
	if err != nil {
		panic(err)
	}
	return base.Sugar()
}
