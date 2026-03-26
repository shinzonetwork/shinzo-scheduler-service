package store

import (
	"context"
	"encoding/json"
	"fmt"
)

type LedgerStore struct {
	db dbClient
}

func NewLedgerStore(db dbClient) *LedgerStore {
	return &LedgerStore{db: db}
}

const ledgerFields = "_docID ledgerId sessionId hostId indexerId blocksVerified creditRemaining initialEscrow pricePerBlock lastComparedBlock updatedAt"

func (s *LedgerStore) Create(ctx context.Context, r *SessionLedgerRecord) (*SessionLedgerRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__SessionLedger(input: {
			ledgerId: %q, sessionId: %q, hostId: %q, indexerId: %q,
			blocksVerified: %d, creditRemaining: %g, initialEscrow: %g,
			pricePerBlock: %g, lastComparedBlock: %d, updatedAt: %q
		}) { %s }
	}`,
		r.LedgerID, r.SessionID, r.HostID, r.IndexerID,
		r.BlocksVerified, r.CreditRemaining, r.InitialEscrow,
		r.PricePerBlock, r.LastComparedBlock, r.UpdatedAt,
		ledgerFields,
	)
	return s.mutateOne(ctx, "create_Scheduler__SessionLedger", q)
}

func (s *LedgerStore) GetBySession(ctx context.Context, sessionID string) (*SessionLedgerRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__SessionLedger(filter: {sessionId: {_eq: %q}}) { %s }
	}`, sessionID, ledgerFields)
	many, err := s.queryMany(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, nil
	}
	return &many[0], nil
}

func (s *LedgerStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__SessionLedger(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update ledger %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *LedgerStore) ListAll(ctx context.Context) ([]SessionLedgerRecord, error) {
	q := fmt.Sprintf(`query { Scheduler__SessionLedger { %s } }`, ledgerFields)
	return s.queryMany(ctx, q)
}

func (s *LedgerStore) queryMany(ctx context.Context, q string) ([]SessionLedgerRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query session ledger: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerSessionLedger []SessionLedgerRecord `json:"Scheduler__SessionLedger"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerSessionLedger, nil
}

func (s *LedgerStore) mutateOne(ctx context.Context, key, q string) (*SessionLedgerRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate session ledger: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]SessionLedgerRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
