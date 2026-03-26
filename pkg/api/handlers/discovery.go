package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/discovery"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
)

type discoverer interface {
	FindForTip(ctx context.Context, q discovery.TipQuery) ([]discovery.IndexerMatch, error)
	FindForSnapshot(ctx context.Context, q discovery.SnapshotQuery) ([]discovery.IndexerMatch, error)
}

type DiscoveryHandler struct {
	disc discoverer
}

func NewDiscoveryHandler(disc *discovery.Discoverer) *DiscoveryHandler {
	return &DiscoveryHandler{disc: disc}
}

// Indexers handles GET /v1/discover/indexers.
// Query params: chain, network, min_reliability, limit, host_id,
// max_tip_per_1k, max_snapshot_per_range.
func (h *DiscoveryHandler) Indexers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	chain := q.Get("chain")
	network := q.Get("network")

	if chain == "" || network == "" {
		writeError(w, http.StatusBadRequest, "chain and network are required")
		return
	}

	minRel, _ := strconv.ParseFloat(q.Get("min_reliability"), 64)
	if minRel < 0 {
		minRel = 0
	}
	if minRel > 1 {
		minRel = 1
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 0 {
		limit = 0
	}

	tq := discovery.TipQuery{
		Chain:          chain,
		Network:        network,
		MinReliability: minRel,
		Limit:          limit,
		HostID:         q.Get("host_id"),
		HostBudget:     parseBudget(q),
	}

	matches, err := h.disc.FindForTip(r.Context(), tq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, matches)
}

// Snapshots handles GET /v1/discover/snapshots.
// Query params: chain, network, block_from, block_to, limit, host_id,
// max_tip_per_1k, max_snapshot_per_range.
func (h *DiscoveryHandler) Snapshots(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	chain := q.Get("chain")
	network := q.Get("network")
	blockFrom, _ := strconv.Atoi(q.Get("block_from"))
	blockTo, _ := strconv.Atoi(q.Get("block_to"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 0 {
		limit = 0
	}

	if chain == "" || network == "" {
		writeError(w, http.StatusBadRequest, "chain and network are required")
		return
	}
	if blockFrom == 0 || blockTo == 0 {
		writeError(w, http.StatusBadRequest, "block_from and block_to are required")
		return
	}
	if blockFrom >= blockTo {
		writeError(w, http.StatusBadRequest, "block_from must be less than block_to")
		return
	}

	sq := discovery.SnapshotQuery{
		Chain:      chain,
		Network:    network,
		BlockFrom:  blockFrom,
		BlockTo:    blockTo,
		Limit:      limit,
		HostID:     q.Get("host_id"),
		HostBudget: parseBudget(q),
	}

	matches, err := h.disc.FindForSnapshot(r.Context(), sq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, matches)
}

// Match handles GET /v1/discover/match — routes to tip or snapshot discovery.
func (h *DiscoveryHandler) Match(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("block_from") != "" && q.Get("block_to") != "" {
		h.Snapshots(w, r)
	} else {
		h.Indexers(w, r)
	}
}

// parseBudget extracts optional host budget params from the query string.
func parseBudget(q map[string][]string) *store.HostBudget {
	tipMax, tipErr := strconv.ParseFloat(getFirst(q, "max_tip_per_1k"), 64)
	snapMax, snapErr := strconv.ParseFloat(getFirst(q, "max_snapshot_per_range"), 64)
	if tipErr != nil && snapErr != nil {
		return nil
	}
	return &store.HostBudget{
		MaxTipPer1kBlocks:   tipMax,
		MaxSnapshotPerRange: snapMax,
	}
}

func getFirst(q map[string][]string, key string) string {
	if vals, ok := q[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}
