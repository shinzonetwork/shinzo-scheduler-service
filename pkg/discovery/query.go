package discovery

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
)

// TipQuery parameters for discovering indexers at chain tip.
type TipQuery struct {
	Chain          string
	Network        string
	MinReliability float64           // 0.0–1.0; 0 means no filter
	Limit          int               // 0 means no limit
	HostBudget     *store.HostBudget // optional price filter
	HostID         string            // optional; enables diversity weighting
}

// SnapshotQuery parameters for discovering indexers serving a block range.
type SnapshotQuery struct {
	Chain      string
	Network    string
	BlockFrom  int
	BlockTo    int
	Limit      int
	HostBudget *store.HostBudget // optional price filter
	HostID     string            // optional; enables diversity weighting
}

// IndexerMatch is returned by discovery queries.
type IndexerMatch struct {
	PeerID                   string                `json:"peer_id"`
	HTTPUrl                  string                `json:"http_url"`
	Multiaddr                string                `json:"multiaddr"`
	Chain                    string                `json:"chain"`
	Network                  string                `json:"network"`
	CurrentTip               int                   `json:"current_tip"`
	ReliabilityScore         float64               `json:"reliability_score"`
	Pricing                  *store.Pricing        `json:"pricing,omitempty"`
	Snapshots                []store.SnapshotRange `json:"snapshots,omitempty"`
	ClearingPrice            float64               `json:"clearing_price,omitempty"`
	SnapshotCreationRequired bool                  `json:"snapshot_creation_required,omitempty"`
}

// indexerLister is the store subset used by discovery queries.
type indexerLister interface {
	ListActive(ctx context.Context, chain, network string) ([]store.IndexerRecord, error)
}

// matchHistoryLister loads match history for diversity weighting.
type matchHistoryLister interface {
	ListByHost(ctx context.Context, hostID string) ([]store.MatchHistoryRecord, error)
}

// DiscovererConfig holds discovery-time parameters.
type DiscovererConfig struct {
	StalenessWindow       time.Duration
	TipExclusionThreshold int
	DiversityEnabled      bool
	RecencyWindow         time.Duration
}

// Discoverer runs tip and snapshot discovery queries against the indexer store.
type Discoverer struct {
	indexerSt indexerLister
	matchSt   matchHistoryLister // nil disables diversity
	cfg       DiscovererConfig
}

func NewDiscoverer(indexerSt indexerLister, matchSt matchHistoryLister, cfg DiscovererConfig) *Discoverer {
	return &Discoverer{indexerSt: indexerSt, matchSt: matchSt, cfg: cfg}
}

// FindForTip returns active indexers for a chain/network, filtered by
// staleness, min reliability, tip lag, and price; sorted by effective score.
func (d *Discoverer) FindForTip(ctx context.Context, q TipQuery) ([]IndexerMatch, error) {
	all, err := d.indexerSt.ListActive(ctx, q.Chain, q.Network)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	// First pass: filter stale, build matches, find reference tip.
	var candidates []indexerCandidate
	referenceTip := 0
	for _, idx := range all {
		if d.isStale(idx, now) {
			continue
		}
		if q.MinReliability > 0 && idx.ReliabilityScore < q.MinReliability {
			continue
		}
		pricing := parsePricing(idx.Pricing)
		if !PriceOverlaps(pricing, q.HostBudget, store.SubTypeTip) {
			continue
		}
		if idx.CurrentTip > referenceTip {
			referenceTip = idx.CurrentTip
		}
		candidates = append(candidates, indexerCandidate{rec: idx, pricing: pricing})
	}

	// Second pass: hard-exclude by tip lag.
	var filtered []indexerCandidate
	for _, c := range candidates {
		if d.cfg.TipExclusionThreshold > 0 && referenceTip-c.rec.CurrentTip > d.cfg.TipExclusionThreshold {
			continue
		}
		filtered = append(filtered, c)
	}

	// Load diversity weights.
	diversityMap := d.loadDiversityMap(ctx, q.HostID)

	// Build matches with effective scores.
	matches := make([]IndexerMatch, 0, len(filtered))
	for _, c := range filtered {
		m := toMatch(c.rec)
		m.Pricing = c.pricing
		if q.HostBudget != nil && c.pricing != nil {
			m.ClearingPrice = ClearingPrice(
				AskForType(c.pricing, store.SubTypeTip),
				BidForType(q.HostBudget, store.SubTypeTip),
				c.rec.ReliabilityScore,
			)
		}
		m.ReliabilityScore = d.applyDiversity(c.rec.PeerID, c.rec.ReliabilityScore, diversityMap, len(filtered))
		matches = append(matches, m)
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].ReliabilityScore != matches[j].ReliabilityScore {
			return matches[i].ReliabilityScore > matches[j].ReliabilityScore
		}
		return matches[i].CurrentTip > matches[j].CurrentTip
	})

	if q.Limit > 0 && len(matches) > q.Limit {
		matches = matches[:q.Limit]
	}
	return matches, nil
}

// FindForSnapshot returns active indexers for a block range. Indexers with
// overlapping snapshots rank first; non-snapshot indexers appear with
// SnapshotCreationRequired=true as fallbacks.
func (d *Discoverer) FindForSnapshot(ctx context.Context, q SnapshotQuery) ([]IndexerMatch, error) {
	all, err := d.indexerSt.ListActive(ctx, q.Chain, q.Network)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	diversityMap := d.loadDiversityMap(ctx, q.HostID)

	var withSnapshot, withoutSnapshot []IndexerMatch
	for _, idx := range all {
		if d.isStale(idx, now) {
			continue
		}
		pricing := parsePricing(idx.Pricing)
		if !PriceOverlaps(pricing, q.HostBudget, store.SubTypeSnapshot) {
			continue
		}

		overlapping := overlappingRanges(idx.SnapshotRanges, q.BlockFrom, q.BlockTo)
		m := toMatch(idx)
		m.Pricing = pricing
		if q.HostBudget != nil && pricing != nil {
			m.ClearingPrice = ClearingPrice(
				AskForType(pricing, store.SubTypeSnapshot),
				BidForType(q.HostBudget, store.SubTypeSnapshot),
				idx.ReliabilityScore,
			)
		}

		totalCandidates := len(all) // approximate; used for liquidity check
		m.ReliabilityScore = d.applyDiversity(idx.PeerID, idx.ReliabilityScore, diversityMap, totalCandidates)

		if len(overlapping) > 0 {
			m.Snapshots = overlapping
			withSnapshot = append(withSnapshot, m)
		} else {
			m.SnapshotCreationRequired = true
			withoutSnapshot = append(withoutSnapshot, m)
		}
	}

	// Sort each tier by reliability desc.
	sortByReliability := func(s []IndexerMatch) {
		sort.Slice(s, func(i, j int) bool {
			return s[i].ReliabilityScore > s[j].ReliabilityScore
		})
	}
	sortByReliability(withSnapshot)
	sortByReliability(withoutSnapshot)

	// Snapshot-holding indexers first, then fallbacks.
	matches := append(withSnapshot, withoutSnapshot...)

	if q.Limit > 0 && len(matches) > q.Limit {
		matches = matches[:q.Limit]
	}
	return matches, nil
}

type indexerCandidate struct {
	rec     store.IndexerRecord
	pricing *store.Pricing
}

// isStale returns true if the indexer's heartbeat is older than the staleness window.
func (d *Discoverer) isStale(idx store.IndexerRecord, now time.Time) bool {
	if d.cfg.StalenessWindow <= 0 || idx.LastHeartbeat == "" {
		return false
	}
	lastHB, err := time.Parse(time.RFC3339, idx.LastHeartbeat)
	if err != nil {
		return true // unparseable heartbeat treated as stale
	}
	return now.Sub(lastHB) > d.cfg.StalenessWindow
}

// loadDiversityMap builds a map of indexerID→most recent match time for a host.
func (d *Discoverer) loadDiversityMap(ctx context.Context, hostID string) map[string]time.Time {
	if !d.cfg.DiversityEnabled || hostID == "" || d.matchSt == nil {
		return nil
	}
	history, err := d.matchSt.ListByHost(ctx, hostID)
	if err != nil || len(history) == 0 {
		return nil
	}
	m := make(map[string]time.Time, len(history))
	for _, h := range history {
		t, err := time.Parse(time.RFC3339, h.MatchedAt)
		if err != nil {
			continue
		}
		if existing, ok := m[h.IndexerID]; !ok || t.After(existing) {
			m[h.IndexerID] = t
		}
	}
	return m
}

// applyDiversity multiplies the score by a diversity weight if applicable.
// Liquidity preservation: if the pool is too small, skip diversity weighting.
func (d *Discoverer) applyDiversity(indexerID string, score float64, diversityMap map[string]time.Time, poolSize int) float64 {
	if diversityMap == nil {
		return score
	}
	// Liquidity preservation: skip diversity if pool is very small.
	if poolSize <= 3 {
		return score
	}
	if lastMatched, ok := diversityMap[indexerID]; ok {
		weight := DiversityWeight(lastMatched, d.cfg.RecencyWindow)
		return score * weight
	}
	return score
}

// overlappingRanges parses the snapshotRanges JSON and returns ranges that
// overlap with [from, to] (inclusive on both ends).
func overlappingRanges(rangesJSON string, from, to int) []store.SnapshotRange {
	if rangesJSON == "" || rangesJSON == "[]" {
		return nil
	}
	var ranges []store.SnapshotRange
	if err := json.Unmarshal([]byte(rangesJSON), &ranges); err != nil {
		return nil
	}
	var out []store.SnapshotRange
	for _, r := range ranges {
		if r.Start <= to && r.End >= from {
			out = append(out, r)
		}
	}
	return out
}

func parsePricing(pricingJSON string) *store.Pricing {
	if pricingJSON == "" || pricingJSON == "{}" {
		return nil
	}
	var p store.Pricing
	if err := json.Unmarshal([]byte(pricingJSON), &p); err != nil {
		return nil
	}
	return &p
}

func toMatch(idx store.IndexerRecord) IndexerMatch {
	return IndexerMatch{
		PeerID:           idx.PeerID,
		HTTPUrl:          idx.HTTPUrl,
		Multiaddr:        idx.Multiaddr,
		Chain:            idx.Chain,
		Network:          idx.Network,
		CurrentTip:       idx.CurrentTip,
		ReliabilityScore: idx.ReliabilityScore,
	}
}
