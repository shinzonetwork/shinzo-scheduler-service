package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/handlers"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/middleware"
	"go.uber.org/zap"
)

type Server struct {
	httpServer *http.Server
	log        *zap.SugaredLogger
}

func NewServer(
	cfg config.ServerConfig,
	indexerH *handlers.IndexerHandler,
	hostH *handlers.HostHandler,
	discoveryH *handlers.DiscoveryHandler,
	subscriptionH *handlers.SubscriptionHandler,
	paymentH *handlers.PaymentHandler,
	healthH *handlers.HealthHandler,
	metricsH *handlers.MetricsHandler,
	accountingH *handlers.AccountingHandler,
	settlementH *handlers.SettlementHandler,
	log *zap.SugaredLogger,
) *Server {
	r := mux.NewRouter()

	// Global middleware.
	r.Use(middleware.Logging(log))
	r.Use(middleware.MaxBodySize(1 << 20)) // 1 MB
	r.Use(middleware.RateLimit(50, 100))

	// Public routes.
	r.HandleFunc("/v1/health", healthH.Health).Methods(http.MethodGet)
	r.HandleFunc("/v1/stats", healthH.Stats).Methods(http.MethodGet)
	r.HandleFunc("/v1/metrics", metricsH.Metrics).Methods(http.MethodGet)

	// Registration routes (public, tighter per-IP rate limit).
	reg := r.NewRoute().Subrouter()
	reg.Use(middleware.RateLimit(5, 10))
	reg.HandleFunc("/v1/indexers/register", indexerH.Register).Methods(http.MethodPost)
	reg.HandleFunc("/v1/hosts/register", hostH.Register).Methods(http.MethodPost)

	// Public read-only indexer route.
	r.HandleFunc("/v1/indexers/{id}", indexerH.Get).Methods(http.MethodGet)

	// Authenticated indexer routes.
	indexed := r.NewRoute().Subrouter()
	indexed.Use(middleware.RequireAPIKey)
	indexed.Use(middleware.RateLimitByKey(10, 30))
	indexed.HandleFunc("/v1/indexers/{id}/heartbeat", indexerH.Heartbeat).Methods(http.MethodPost)
	indexed.HandleFunc("/v1/indexers/{id}", indexerH.Deregister).Methods(http.MethodDelete)

	// Authenticated host routes.
	hosted := r.NewRoute().Subrouter()
	hosted.Use(middleware.RequireAPIKey)
	hosted.Use(middleware.RateLimitByKey(10, 30))
	hosted.HandleFunc("/v1/hosts/{id}/heartbeat", hostH.Heartbeat).Methods(http.MethodPost)
	hosted.HandleFunc("/v1/hosts/{id}", hostH.Get).Methods(http.MethodGet)
	hosted.HandleFunc("/v1/hosts/{id}", hostH.Deregister).Methods(http.MethodDelete)

	// Discovery (requires host API key).
	disc := r.NewRoute().Subrouter()
	disc.Use(middleware.RequireAPIKey)
	disc.Use(middleware.RateLimitByKey(10, 30))
	disc.HandleFunc("/v1/discover/indexers", discoveryH.Indexers).Methods(http.MethodGet)
	disc.HandleFunc("/v1/discover/snapshots", discoveryH.Snapshots).Methods(http.MethodGet)
	disc.HandleFunc("/v1/discover/match", discoveryH.Match).Methods(http.MethodGet)

	// Subscriptions.
	subs := r.NewRoute().Subrouter()
	subs.Use(middleware.RequireAPIKey)
	subs.Use(middleware.RateLimitByKey(10, 30))
	subs.HandleFunc("/v1/subscriptions", subscriptionH.Create).Methods(http.MethodPost)
	subs.HandleFunc("/v1/subscriptions", subscriptionH.List).Methods(http.MethodGet)
	subs.HandleFunc("/v1/subscriptions/{id}", subscriptionH.Get).Methods(http.MethodGet)
	subs.HandleFunc("/v1/subscriptions/{id}", subscriptionH.Cancel).Methods(http.MethodDelete)

	// Payments.
	pay := r.NewRoute().Subrouter()
	pay.Use(middleware.RequireAPIKey)
	pay.Use(middleware.RateLimitByKey(10, 30))
	pay.HandleFunc("/v1/quotes", paymentH.Quote).Methods(http.MethodGet)
	pay.HandleFunc("/v1/payments/verify", paymentH.VerifyPayment).Methods(http.MethodPost)

	// Accounting (delivery claims, attestations, session ledger).
	if accountingH != nil {
		acct := r.NewRoute().Subrouter()
		acct.Use(middleware.RequireAPIKey)
		acct.Use(middleware.RateLimitByKey(10, 30))
		acct.HandleFunc("/v1/claims", accountingH.SubmitClaim).Methods(http.MethodPost)
		acct.HandleFunc("/v1/attestations", accountingH.SubmitAttestation).Methods(http.MethodPost)
		acct.HandleFunc("/v1/sessions/{id}/ledger", accountingH.SessionLedger).Methods(http.MethodGet)
		acct.HandleFunc("/v1/sessions/{id}/comparisons", accountingH.Comparisons).Methods(http.MethodGet)
	}

	// Settlement (escrow, settlements, verdicts).
	if settlementH != nil {
		setl := r.NewRoute().Subrouter()
		setl.Use(middleware.RequireAPIKey)
		setl.Use(middleware.RateLimitByKey(10, 30))
		setl.HandleFunc("/v1/escrow/{session_id}", settlementH.Escrow).Methods(http.MethodGet)
		setl.HandleFunc("/v1/settlements/{session_id}", settlementH.Settlements).Methods(http.MethodGet)
		setl.HandleFunc("/v1/verdicts/{session_id}", settlementH.Verdicts).Methods(http.MethodGet)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.ReadTimeoutSeconds) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeoutSeconds) * time.Second,
	}

	return &Server{httpServer: srv, log: log}
}

func (s *Server) Start() error {
	s.log.Infof("scheduler API listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
