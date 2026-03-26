package settlement

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

// contradictionStore defines the subset of store.ContradictionStore needed by the aggregator.
type contradictionStore interface {
	ListUnresolved(ctx context.Context) ([]store.ContradictionRecord, error)
	MarkResolved(ctx context.Context, docID string) error
}

type ContradictionAggregator struct {
	contradictionSt contradictionStore
	hub             HubBroadcaster
	cfg             config.SettlementConfig
	log             *zap.SugaredLogger
	stopCh          chan struct{}
	wg              sync.WaitGroup
}

func NewContradictionAggregator(
	contradictionSt contradictionStore,
	hub HubBroadcaster,
	cfg config.SettlementConfig,
	log *zap.SugaredLogger,
) *ContradictionAggregator {
	return &ContradictionAggregator{
		contradictionSt: contradictionSt,
		hub:             hub,
		cfg:             cfg,
		log:             log,
		stopCh:          make(chan struct{}),
	}
}

func (ca *ContradictionAggregator) Start(ctx context.Context) {
	ca.wg.Add(1)
	go func() {
		defer ca.wg.Done()
		interval := time.Duration(ca.cfg.ContradictionCheckIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 300 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ca.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				ca.CheckAndSlash(ctx)
			}
		}
	}()
	ca.log.Infof("contradiction aggregator started (interval=%ds, threshold=%d)",
		ca.cfg.ContradictionCheckIntervalSeconds, ca.cfg.ContradictionThreshold)
}

func (ca *ContradictionAggregator) Stop() {
	close(ca.stopCh)
	ca.wg.Wait()
}

// CheckAndSlash lists unresolved contradictions, groups them by indexer, and
// slashes any indexer that has accumulated at least ContradictionThreshold records.
func (ca *ContradictionAggregator) CheckAndSlash(ctx context.Context) {
	records, err := ca.contradictionSt.ListUnresolved(ctx)
	if err != nil {
		ca.log.Warnf("contradiction aggregator: list unresolved: %v", err)
		return
	}
	if len(records) == 0 {
		return
	}

	// Group by indexer.
	grouped := make(map[string][]store.ContradictionRecord)
	for _, r := range records {
		grouped[r.IndexerID] = append(grouped[r.IndexerID], r)
	}

	threshold := ca.cfg.ContradictionThreshold
	if threshold <= 0 {
		threshold = 3
	}

	for indexerID, recs := range grouped {
		if len(recs) < threshold {
			continue
		}

		// Build comma-separated evidence IDs.
		evidenceIDs := make([]string, len(recs))
		for i, r := range recs {
			evidenceIDs[i] = r.EvidenceID
		}

		msg := MsgSlash{
			IndexerID:   indexerID,
			EvidenceCID: strings.Join(evidenceIDs, ","),
			Reason:      fmt.Sprintf("%d unresolved snapshot contradictions", len(recs)),
		}

		if _, err := ca.hub.BroadcastSlash(ctx, msg); err != nil {
			ca.log.Warnf("contradiction aggregator: slash broadcast for %s failed: %v", indexerID, err)
			continue
		}

		// Mark all contradictions as resolved only after successful broadcast.
		for _, r := range recs {
			if err := ca.contradictionSt.MarkResolved(ctx, r.DocID); err != nil {
				ca.log.Warnf("contradiction aggregator: mark resolved %s: %v", r.DocID, err)
			}
		}
		ca.log.Infof("contradiction aggregator: slashed indexer %s (%d contradictions)", indexerID, len(recs))
	}
}
