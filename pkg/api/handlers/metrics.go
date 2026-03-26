package handlers

import (
	"context"
	"net/http"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

type probeRater interface {
	RecentSuccessRate(ctx context.Context, limit int) (float64, error)
}

type MetricsHandler struct {
	indexerSt nodeCounter
	hostSt    nodeCounter
	subSt     subStatusLister
	probeSt   probeRater
	log       *zap.SugaredLogger
}

func NewMetricsHandler(
	indexerSt *store.IndexerStore,
	hostSt *store.HostStore,
	subSt *store.SubscriptionStore,
	probeSt *store.ProbeStore,
	log *zap.SugaredLogger,
) *MetricsHandler {
	return &MetricsHandler{indexerSt: indexerSt, hostSt: hostSt, subSt: subSt, probeSt: probeSt, log: log}
}

type metricsResponse struct {
	ActiveIndexers   int     `json:"active_indexers"`
	InactiveIndexers int     `json:"inactive_indexers"`
	ActiveHosts      int     `json:"active_hosts"`
	ActiveSubs       int     `json:"active_subs"`
	PendingSubs      int     `json:"pending_subs"`
	ExpiredSubs      int     `json:"expired_subs"`
	ProbeSuccessRate float64 `json:"probe_success_rate"`
}

func (h *MetricsHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	activeIdx, err := h.indexerSt.Count(ctx, store.StatusActive)
	if err != nil {
		h.log.Warnw("metrics: count active indexers", "error", err)
	}
	inactiveIdx, err := h.indexerSt.Count(ctx, store.StatusInactive)
	if err != nil {
		h.log.Warnw("metrics: count inactive indexers", "error", err)
	}
	activeHosts, err := h.hostSt.Count(ctx, store.StatusActive)
	if err != nil {
		h.log.Warnw("metrics: count active hosts", "error", err)
	}

	activeSubs, err := subCount(ctx, h.subSt, store.StatusActive)
	if err != nil {
		h.log.Warnw("metrics: count active subscriptions", "error", err)
	}
	pendingSubs, err := subCount(ctx, h.subSt, store.StatusPending)
	if err != nil {
		h.log.Warnw("metrics: count pending subscriptions", "error", err)
	}
	expiredSubs, err := subCount(ctx, h.subSt, store.StatusExpired)
	if err != nil {
		h.log.Warnw("metrics: count expired subscriptions", "error", err)
	}

	rate, err := h.probeSt.RecentSuccessRate(ctx, 100)
	if err != nil {
		h.log.Warnw("metrics: probe success rate", "error", err)
	}

	writeJSON(w, http.StatusOK, metricsResponse{
		ActiveIndexers:   activeIdx,
		InactiveIndexers: inactiveIdx,
		ActiveHosts:      activeHosts,
		ActiveSubs:       activeSubs,
		PendingSubs:      pendingSubs,
		ExpiredSubs:      expiredSubs,
		ProbeSuccessRate: rate,
	})
}

func subCount(ctx context.Context, st subStatusLister, status string) (int, error) {
	recs, err := st.ListByStatus(ctx, status)
	return len(recs), err
}
