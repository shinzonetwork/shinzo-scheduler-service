package store

import (
	"context"
	"encoding/json"
	"fmt"
)

type ContradictionStore struct {
	db dbClient
}

func NewContradictionStore(db dbClient) *ContradictionStore {
	return &ContradictionStore{db: db}
}

func (s *ContradictionStore) Create(ctx context.Context, r *ContradictionRecord) (*ContradictionRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Contradiction(input: {
			evidenceId: %q, indexerId: %q, snapshotRange: %q,
			probedAt: %q, resolved: %t
		}) {
			_docID evidenceId indexerId snapshotRange probedAt resolved
		}
	}`,
		r.EvidenceID, r.IndexerID, r.SnapshotRange,
		r.ProbedAt, r.Resolved,
	)
	return s.mutateOne(ctx, "create_Scheduler__Contradiction", q)
}

func (s *ContradictionStore) ListByIndexer(ctx context.Context, indexerID string) ([]ContradictionRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Contradiction(filter: {indexerId: {_eq: %q}}) {
			_docID evidenceId indexerId snapshotRange probedAt resolved
		}
	}`, indexerID)
	return s.queryMany(ctx, q)
}

func (s *ContradictionStore) ListUnresolved(ctx context.Context) ([]ContradictionRecord, error) {
	q := `query {
		Scheduler__Contradiction(filter: {resolved: {_eq: false}}) {
			_docID evidenceId indexerId snapshotRange probedAt resolved
		}
	}`
	return s.queryMany(ctx, q)
}

func (s *ContradictionStore) MarkResolved(ctx context.Context, docID string) error {
	return s.Update(ctx, docID, map[string]any{"resolved": true})
}

func (s *ContradictionStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__Contradiction(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update contradiction %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *ContradictionStore) queryMany(ctx context.Context, q string) ([]ContradictionRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query contradictions: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerContradiction []ContradictionRecord `json:"Scheduler__Contradiction"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerContradiction, nil
}

func (s *ContradictionStore) mutateOne(ctx context.Context, key, q string) (*ContradictionRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate contradiction: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]ContradictionRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
