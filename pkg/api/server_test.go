package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/api/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewServer_StartShutdown(t *testing.T) {
	log, _ := zap.NewDevelopment()
	// All handler pointers are nil; routes are registered but never called.
	srv := NewServer(
		config.ServerConfig{Port: 0, ReadTimeoutSeconds: 5, WriteTimeoutSeconds: 5},
		(*handlers.IndexerHandler)(nil),
		(*handlers.HostHandler)(nil),
		(*handlers.DiscoveryHandler)(nil),
		(*handlers.SubscriptionHandler)(nil),
		(*handlers.PaymentHandler)(nil),
		(*handlers.HealthHandler)(nil),
		(*handlers.MetricsHandler)(nil),
		(*handlers.AccountingHandler)(nil),
		(*handlers.SettlementHandler)(nil),
		log.Sugar(),
	)
	require.NotNil(t, srv)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	// Give the server a moment to start listening.
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	assert.NoError(t, err)
	assert.ErrorIs(t, <-errCh, http.ErrServerClosed)
}

func TestNewServer_WithAccountingAndSettlement(t *testing.T) {
	log, _ := zap.NewDevelopment()
	// Pass non-nil accounting and settlement handlers to cover the conditional route registration.
	accountingH := handlers.NewAccountingHandler(nil)
	settlementH := handlers.NewSettlementHandler(nil, nil, nil)

	srv := NewServer(
		config.ServerConfig{Port: 0, ReadTimeoutSeconds: 5, WriteTimeoutSeconds: 5},
		(*handlers.IndexerHandler)(nil),
		(*handlers.HostHandler)(nil),
		(*handlers.DiscoveryHandler)(nil),
		(*handlers.SubscriptionHandler)(nil),
		(*handlers.PaymentHandler)(nil),
		(*handlers.HealthHandler)(nil),
		(*handlers.MetricsHandler)(nil),
		accountingH,
		settlementH,
		log.Sugar(),
	)
	require.NotNil(t, srv)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	assert.NoError(t, err)
	assert.ErrorIs(t, <-errCh, http.ErrServerClosed)
}
