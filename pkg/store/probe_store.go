package store

import (
	"context"
	"encoding/json"
	"fmt"
)

type ProbeStore struct {
	db dbClient
}

func NewProbeStore(db dbClient) *ProbeStore {
	return &ProbeStore{db: db}
}

func (s *ProbeStore) Insert(ctx context.Context, r *ProbeResultRecord) error {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__ProbeResult(input: {
			indexerId: %q, probedAt: %q, success: %t, tip: %d, latencyMs: %d
		}) { _docID }
	}`, r.IndexerID, r.ProbedAt, r.Success, r.Tip, r.LatencyMs)

	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("insert probe result: %v", res.GQL.Errors)
	}
	return nil
}

func (s *ProbeStore) ListByIndexer(ctx context.Context, indexerID string) ([]ProbeResultRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__ProbeResult(filter: {indexerId: {_eq: %q}}) {
			_docID indexerId probedAt success tip latencyMs
		}
	}`, indexerID)
	return s.queryMany(ctx, q)
}

// DefraDB has no LIMIT+ORDER in delete, so we query all, sort, and delete the excess.
func (s *ProbeStore) PruneOldest(ctx context.Context, indexerID string, limit int) error {
	recs, err := s.ListByIndexer(ctx, indexerID)
	if err != nil {
		return err
	}
	if len(recs) <= limit {
		return nil
	}

	// Sort ascending by probedAt so the oldest come first.
	sortProbeResultsAsc(recs)

	toDelete := recs[:len(recs)-limit]
	for _, r := range toDelete {
		q := fmt.Sprintf(`mutation { delete_Scheduler__ProbeResult(docID: %q) { _docID } }`, r.DocID)
		res := s.db.ExecRequest(ctx, q)
		if len(res.GQL.Errors) > 0 {
			return fmt.Errorf("prune probe result %s: %v", r.DocID, res.GQL.Errors)
		}
	}
	return nil
}

// RecentSuccessRate returns the fraction of successful probes across all indexers
// in the most recent `limit` results. Returns 0 when there are no results.
func (s *ProbeStore) RecentSuccessRate(ctx context.Context, limit int) (float64, error) {
	q := `query {
		Scheduler__ProbeResult {
			_docID probedAt success
		}
	}`
	recs, err := s.queryMany(ctx, q)
	if err != nil {
		return 0, err
	}
	if len(recs) == 0 {
		return 0, nil
	}
	// Take the most recent `limit` entries (sort desc, take first limit).
	sortProbeResultsAsc(recs) // ascending — we want the tail
	if len(recs) > limit {
		recs = recs[len(recs)-limit:]
	}
	var successes int
	for _, r := range recs {
		if r.Success {
			successes++
		}
	}
	return float64(successes) / float64(len(recs)), nil
}

func (s *ProbeStore) queryMany(ctx context.Context, q string) ([]ProbeResultRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query probe results: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerProbeResult []ProbeResultRecord `json:"Scheduler__ProbeResult"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerProbeResult, nil
}

func sortProbeResultsAsc(recs []ProbeResultRecord) {
	for i := 1; i < len(recs); i++ {
		for j := i; j > 0 && recs[j].ProbedAt < recs[j-1].ProbedAt; j-- {
			recs[j], recs[j-1] = recs[j-1], recs[j]
		}
	}
}
