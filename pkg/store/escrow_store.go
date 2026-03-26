package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// EscrowStore provides CRUD operations on the Scheduler__EscrowAccount collection.
type EscrowStore struct {
	db dbClient
}

func NewEscrowStore(db dbClient) *EscrowStore {
	return &EscrowStore{db: db}
}

const escrowFields = "_docID escrowId sessionId hostId indexerId initialBalance currentBalance pricePerBlock lowWaterThreshold lowCreditSignaled gracePeriodEndsAt status createdAt updatedAt"

func (s *EscrowStore) Create(ctx context.Context, r *EscrowAccountRecord) (*EscrowAccountRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__EscrowAccount(input: {
			escrowId: %q, sessionId: %q, hostId: %q, indexerId: %q,
			initialBalance: %g, currentBalance: %g, pricePerBlock: %g,
			lowWaterThreshold: %g, lowCreditSignaled: %t,
			gracePeriodEndsAt: %q, status: %q, createdAt: %q, updatedAt: %q
		}) { %s }
	}`,
		r.EscrowID, r.SessionID, r.HostID, r.IndexerID,
		r.InitialBalance, r.CurrentBalance, r.PricePerBlock,
		r.LowWaterThreshold, r.LowCreditSignaled,
		r.GracePeriodEndsAt, r.Status, r.CreatedAt, r.UpdatedAt,
		escrowFields,
	)
	return s.mutateOne(ctx, "create_Scheduler__EscrowAccount", q)
}

func (s *EscrowStore) GetBySession(ctx context.Context, sessionID string) (*EscrowAccountRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__EscrowAccount(filter: {sessionId: {_eq: %q}}) { %s }
	}`, sessionID, escrowFields)
	many, err := s.queryMany(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, nil
	}
	return &many[0], nil
}

// ListActive returns all escrow accounts with active status.
func (s *EscrowStore) ListActive(ctx context.Context) ([]EscrowAccountRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__EscrowAccount(filter: {status: {_eq: %q}}) { %s }
	}`, StatusActive, escrowFields)
	return s.queryMany(ctx, q)
}

func (s *EscrowStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__EscrowAccount(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update escrow %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *EscrowStore) queryMany(ctx context.Context, q string) ([]EscrowAccountRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query escrow accounts: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerEscrowAccount []EscrowAccountRecord `json:"Scheduler__EscrowAccount"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerEscrowAccount, nil
}

func (s *EscrowStore) mutateOne(ctx context.Context, key, q string) (*EscrowAccountRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate escrow account: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]EscrowAccountRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
