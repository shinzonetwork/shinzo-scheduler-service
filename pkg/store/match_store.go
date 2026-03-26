package store

import (
	"context"
	"encoding/json"
	"fmt"
)

type MatchStore struct {
	db dbClient
}

func NewMatchStore(db dbClient) *MatchStore {
	return &MatchStore{db: db}
}

func (s *MatchStore) Create(ctx context.Context, r *MatchHistoryRecord) (*MatchHistoryRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__MatchHistory(input: {
			matchId: %q, hostId: %q, indexerId: %q, matchType: %q,
			matchedAt: %q, clearingPrice: %g
		}) {
			_docID matchId hostId indexerId matchType matchedAt clearingPrice
		}
	}`,
		r.MatchID, r.HostID, r.IndexerID, r.MatchType,
		r.MatchedAt, r.ClearingPrice,
	)
	return s.mutateOne(ctx, "create_Scheduler__MatchHistory", q)
}

func (s *MatchStore) ListByHost(ctx context.Context, hostID string) ([]MatchHistoryRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__MatchHistory(filter: {hostId: {_eq: %q}}) {
			_docID matchId hostId indexerId matchType matchedAt clearingPrice
		}
	}`, hostID)
	return s.queryMany(ctx, q)
}

func (s *MatchStore) ListByHostAndIndexer(ctx context.Context, hostID, indexerID string) ([]MatchHistoryRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__MatchHistory(filter: {hostId: {_eq: %q}, indexerId: {_eq: %q}}) {
			_docID matchId hostId indexerId matchType matchedAt clearingPrice
		}
	}`, hostID, indexerID)
	return s.queryMany(ctx, q)
}

func (s *MatchStore) queryMany(ctx context.Context, q string) ([]MatchHistoryRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query match history: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerMatchHistory []MatchHistoryRecord `json:"Scheduler__MatchHistory"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerMatchHistory, nil
}

func (s *MatchStore) mutateOne(ctx context.Context, key, q string) (*MatchHistoryRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate match history: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]MatchHistoryRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
