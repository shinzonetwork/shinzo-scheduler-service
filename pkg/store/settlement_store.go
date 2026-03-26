package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// SettlementStore provides CRUD operations on the Scheduler__Settlement collection.
type SettlementStore struct {
	db dbClient
}

func NewSettlementStore(db dbClient) *SettlementStore {
	return &SettlementStore{db: db}
}

const settlementFields = "_docID settlementId batchId sessionId blocksVerified indexerAmount hostRefund closeReason txHash status settledAt"

func (s *SettlementStore) Create(ctx context.Context, r *SettlementRecord) (*SettlementRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Settlement(input: {
			settlementId: %q, batchId: %q, sessionId: %q,
			blocksVerified: %d, indexerAmount: %g, hostRefund: %g,
			closeReason: %q, txHash: %q, status: %q, settledAt: %q
		}) { %s }
	}`,
		r.SettlementID, r.BatchID, r.SessionID,
		r.BlocksVerified, r.IndexerAmount, r.HostRefund,
		r.CloseReason, r.TxHash, r.Status, r.SettledAt,
		settlementFields,
	)
	return s.mutateOne(ctx, "create_Scheduler__Settlement", q)
}

func (s *SettlementStore) ListBySession(ctx context.Context, sessionID string) ([]SettlementRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Settlement(filter: {sessionId: {_eq: %q}}) { %s }
	}`, sessionID, settlementFields)
	return s.queryMany(ctx, q)
}

func (s *SettlementStore) ListByBatch(ctx context.Context, batchID string) ([]SettlementRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Settlement(filter: {batchId: {_eq: %q}}) { %s }
	}`, batchID, settlementFields)
	return s.queryMany(ctx, q)
}

func (s *SettlementStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__Settlement(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update settlement %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *SettlementStore) queryMany(ctx context.Context, q string) ([]SettlementRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query settlements: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerSettlement []SettlementRecord `json:"Scheduler__Settlement"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerSettlement, nil
}

func (s *SettlementStore) mutateOne(ctx context.Context, key, q string) (*SettlementRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate settlement: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]SettlementRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
