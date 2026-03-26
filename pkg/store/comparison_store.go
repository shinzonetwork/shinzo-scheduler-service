package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// ComparisonStore provides CRUD operations on the Scheduler__ComparisonResult collection.
type ComparisonStore struct {
	db dbClient
}

func NewComparisonStore(db dbClient) *ComparisonStore {
	return &ComparisonStore{db: db}
}

const compFields = "_docID comparisonId sessionId blockNumber outcome claimId attestationId comparedAt"

func (s *ComparisonStore) Create(ctx context.Context, r *ComparisonResultRecord) (*ComparisonResultRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__ComparisonResult(input: {
			comparisonId: %q, sessionId: %q, blockNumber: %d,
			outcome: %q, claimId: %q, attestationId: %q, comparedAt: %q
		}) { %s }
	}`,
		r.ComparisonID, r.SessionID, r.BlockNumber,
		r.Outcome, r.ClaimID, r.AttestationID, r.ComparedAt,
		compFields,
	)
	return s.mutateOne(ctx, "create_Scheduler__ComparisonResult", q)
}

func (s *ComparisonStore) ListBySession(ctx context.Context, sessionID string) ([]ComparisonResultRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__ComparisonResult(filter: {sessionId: {_eq: %q}}) { %s }
	}`, sessionID, compFields)
	return s.queryMany(ctx, q)
}

func (s *ComparisonStore) queryMany(ctx context.Context, q string) ([]ComparisonResultRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query comparison results: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerComparisonResult []ComparisonResultRecord `json:"Scheduler__ComparisonResult"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerComparisonResult, nil
}

func (s *ComparisonStore) mutateOne(ctx context.Context, key, q string) (*ComparisonResultRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate comparison result: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]ComparisonResultRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
