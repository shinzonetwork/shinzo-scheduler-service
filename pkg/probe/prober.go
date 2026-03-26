package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/discovery"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/registry"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

type indexerLister interface {
	ListAllActive(ctx context.Context) ([]store.IndexerRecord, error)
}

type reliabilityUpdater interface {
	UpdateReliability(ctx context.Context, docID string, score float64, tip int, status string) error
}

type probeInserter interface {
	Insert(ctx context.Context, r *store.ProbeResultRecord) error
	PruneOldest(ctx context.Context, indexerID string, limit int) error
}

type contradictionRecorder interface {
	Create(ctx context.Context, r *store.ContradictionRecord) (*store.ContradictionRecord, error)
}

// healthResponse is the subset of fields we care about from GET /health.
type healthResponse struct {
	CurrentBlock int    `json:"current_block"`
	Status       string `json:"status"`
}

type Prober struct {
	cfg             config.ProbeConfig
	indexerReg      reliabilityUpdater
	indexerSt       indexerLister
	probeSt         probeInserter
	contradictionSt contradictionRecorder // nil disables contradiction recording
	httpClient      *http.Client
	log             *zap.SugaredLogger
	stopCh          chan struct{}
	wg              sync.WaitGroup
	sem             chan struct{} // limits concurrent outbound probe connections
}

func NewProber(
	cfg config.ProbeConfig,
	indexerReg *registry.IndexerRegistry,
	indexerSt *store.IndexerStore,
	probeSt *store.ProbeStore,
	log *zap.SugaredLogger,
) *Prober {
	maxConc := cfg.MaxConcurrentProbes
	if maxConc <= 0 {
		maxConc = 20
	}
	return &Prober{
		cfg:        cfg,
		indexerReg: indexerReg,
		indexerSt:  indexerSt,
		probeSt:    probeSt,
		httpClient: &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
		log:        log,
		stopCh:     make(chan struct{}),
		sem:        make(chan struct{}, maxConc),
	}
}

// WithContradictionRecorder attaches a store for recording snapshot delivery contradictions.
func (p *Prober) WithContradictionRecorder(cr contradictionRecorder) {
	p.contradictionSt = cr
}

func (p *Prober) Start(ctx context.Context) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(time.Duration(p.cfg.IntervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-p.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.probeAll(ctx)
			}
		}
	}()
	p.log.Infof("prober started (interval=%ds)", p.cfg.IntervalSeconds)
}

func (p *Prober) Stop() {
	close(p.stopCh)
	p.wg.Wait()
}

func (p *Prober) probeAll(ctx context.Context) {
	indexers, err := p.indexerSt.ListAllActive(ctx)
	if err != nil {
		p.log.Warnf("prober: list active indexers failed: %v", err)
		return
	}

	inactiveThreshold := time.Duration(p.cfg.InactiveAfterMinutes) * time.Minute

	for _, idx := range indexers {
		p.sem <- struct{}{} // acquire slot before spawning goroutine
		go func(i store.IndexerRecord) {
			defer func() { <-p.sem }()
			p.probeOne(ctx, i, inactiveThreshold)
		}(idx)
	}
}

func (p *Prober) probeOne(ctx context.Context, idx store.IndexerRecord, inactiveThreshold time.Duration) {
	start := time.Now()
	tip, success := p.fetchHealth(idx.HTTPUrl)
	latency := int(time.Since(start).Milliseconds())

	// Record the result.
	result := &store.ProbeResultRecord{
		IndexerID: idx.PeerID,
		ProbedAt:  start.UTC().Format(time.RFC3339),
		Success:   success,
		Tip:       tip,
		LatencyMs: latency,
	}
	if err := p.probeSt.Insert(ctx, result); err != nil {
		p.log.Warnf("prober: insert result for %s: %v", idx.PeerID, err)
	}

	// Prune history to keep storage bounded.
	if err := p.probeSt.PruneOldest(ctx, idx.PeerID, p.cfg.ProbeHistoryLimit); err != nil {
		p.log.Debugf("prober: prune for %s: %v", idx.PeerID, err)
	}

	newScore := discovery.UpdateEMA(idx.ReliabilityScore, success)

	// Determine if the indexer should be marked inactive based on heartbeat silence.
	newStatus := ""
	if idx.LastHeartbeat != "" {
		if last, err := time.Parse(time.RFC3339, idx.LastHeartbeat); err == nil {
			if time.Since(last) > inactiveThreshold && idx.Status == store.StatusActive {
				newStatus = store.StatusInactive
				p.log.Warnf("prober: marking indexer %s inactive (last heartbeat %s)", idx.PeerID, idx.LastHeartbeat)
			}
		}
	}

	// Record contradiction if indexer declared snapshots but probe failed.
	if !success && p.contradictionSt != nil && idx.SnapshotRanges != "" && idx.SnapshotRanges != "[]" {
		p.recordContradiction(ctx, idx)
	}

	if err := p.indexerReg.UpdateReliability(ctx, idx.DocID, newScore, tip, newStatus); err != nil {
		p.log.Warnf("prober: update reliability for %s: %v", idx.PeerID, err)
	}
}

func (p *Prober) recordContradiction(ctx context.Context, idx store.IndexerRecord) {
	rec := &store.ContradictionRecord{
		EvidenceID:    fmt.Sprintf("probe-%s-%d", idx.PeerID, time.Now().UnixMilli()),
		IndexerID:     idx.PeerID,
		SnapshotRange: idx.SnapshotRanges,
		ProbedAt:      time.Now().UTC().Format(time.RFC3339),
		Resolved:      false,
	}
	if _, err := p.contradictionSt.Create(ctx, rec); err != nil {
		p.log.Debugf("prober: record contradiction for %s: %v", idx.PeerID, err)
	} else {
		p.log.Warnf("prober: snapshot contradiction recorded for indexer %s", idx.PeerID)
	}
}

func (p *Prober) fetchHealth(httpUrl string) (tip int, ok bool) {
	url := fmt.Sprintf("%s/health", httpUrl)
	resp, err := p.httpClient.Get(url)
	if err != nil {
		return 0, false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, false
	}

	var h healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return 0, false
	}
	return h.CurrentBlock, true
}
