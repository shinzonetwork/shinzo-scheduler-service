package store

import (
	"context"
	"encoding/json"
	"fmt"
)

type ClaimStore struct {
	db dbClient
}

func NewClaimStore(db dbClient) *ClaimStore {
	return &ClaimStore{db: db}
}

const claimFields = "_docID claimId sessionId indexerId blockNumber docCids blockHash batchSignature submittedAt status"

func (s *ClaimStore) Create(ctx context.Context, r *DeliveryClaimRecord) (*DeliveryClaimRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__DeliveryClaim(input: {
			claimId: %q, sessionId: %q, indexerId: %q, blockNumber: %d,
			docCids: %q, blockHash: %q, batchSignature: %q,
			submittedAt: %q, status: %q
		}) { %s }
	}`,
		r.ClaimID, r.SessionID, r.IndexerID, r.BlockNumber,
		r.DocCids, r.BlockHash, r.BatchSignature,
		r.SubmittedAt, r.Status,
		claimFields,
	)
	return s.mutateOne(ctx, "create_Scheduler__DeliveryClaim", q)
}

func (s *ClaimStore) GetBySessionAndBlock(ctx context.Context, sessionID string, blockN int) (*DeliveryClaimRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__DeliveryClaim(filter: {sessionId: {_eq: %q}, blockNumber: {_eq: %d}}) {
			%s
		}
	}`, sessionID, blockN, claimFields)
	return s.querySingle(ctx, q)
}

func (s *ClaimStore) ListBySession(ctx context.Context, sessionID string) ([]DeliveryClaimRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__DeliveryClaim(filter: {sessionId: {_eq: %q}}) { %s }
	}`, sessionID, claimFields)
	return s.queryMany(ctx, q)
}

func (s *ClaimStore) ListPending(ctx context.Context) ([]DeliveryClaimRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__DeliveryClaim(filter: {status: {_eq: %q}}) { %s }
	}`, StatusPending, claimFields)
	return s.queryMany(ctx, q)
}

func (s *ClaimStore) UpdateStatus(ctx context.Context, docID, status string) error {
	return s.Update(ctx, docID, map[string]any{"status": status})
}

func (s *ClaimStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__DeliveryClaim(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update claim %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *ClaimStore) querySingle(ctx context.Context, q string) (*DeliveryClaimRecord, error) {
	many, err := s.queryMany(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, nil
	}
	return &many[0], nil
}

func (s *ClaimStore) queryMany(ctx context.Context, q string) ([]DeliveryClaimRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query claims: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerDeliveryClaim []DeliveryClaimRecord `json:"Scheduler__DeliveryClaim"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerDeliveryClaim, nil
}

func (s *ClaimStore) mutateOne(ctx context.Context, key, q string) (*DeliveryClaimRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate claim: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]DeliveryClaimRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
