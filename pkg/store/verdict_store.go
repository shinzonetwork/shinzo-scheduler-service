package store

import (
	"context"
	"encoding/json"
	"fmt"
)

type VerdictStore struct {
	db dbClient
}

func NewVerdictStore(db dbClient) *VerdictStore {
	return &VerdictStore{db: db}
}

const verdictFields = "_docID verdictId sessionId outcome evidenceCids schedulerSignatures createdAt submittedToHub"

func (s *VerdictStore) Create(ctx context.Context, r *VerdictRecord) (*VerdictRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Verdict(input: {
			verdictId: %q, sessionId: %q, outcome: %q,
			evidenceCids: %q, schedulerSignatures: %q,
			createdAt: %q, submittedToHub: %t
		}) { %s }
	}`,
		r.VerdictID, r.SessionID, r.Outcome,
		r.EvidenceCids, r.SchedulerSignatures,
		r.CreatedAt, r.SubmittedToHub,
		verdictFields,
	)
	return s.mutateOne(ctx, "create_Scheduler__Verdict", q)
}

func (s *VerdictStore) GetBySession(ctx context.Context, sessionID string) (*VerdictRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Verdict(filter: {sessionId: {_eq: %q}}) { %s }
	}`, sessionID, verdictFields)
	many, err := s.queryMany(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, nil
	}
	return &many[0], nil
}

func (s *VerdictStore) ListBySession(ctx context.Context, sessionID string) ([]VerdictRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Verdict(filter: {sessionId: {_eq: %q}}) { %s }
	}`, sessionID, verdictFields)
	return s.queryMany(ctx, q)
}

func (s *VerdictStore) ListUnsubmitted(ctx context.Context) ([]VerdictRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Verdict(filter: {submittedToHub: {_eq: false}}) { %s }
	}`, verdictFields)
	return s.queryMany(ctx, q)
}

func (s *VerdictStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__Verdict(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update verdict %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *VerdictStore) queryMany(ctx context.Context, q string) ([]VerdictRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query verdicts: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerVerdict []VerdictRecord `json:"Scheduler__Verdict"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerVerdict, nil
}

func (s *VerdictStore) mutateOne(ctx context.Context, key, q string) (*VerdictRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate verdict: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]VerdictRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
