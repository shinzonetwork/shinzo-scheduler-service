package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// AttestationStore provides CRUD operations on the Scheduler__Attestation collection.
type AttestationStore struct {
	db dbClient
}

func NewAttestationStore(db dbClient) *AttestationStore {
	return &AttestationStore{db: db}
}

const attestFields = "_docID attestationId sessionId hostId blockNumber docCidsReceived batchSignature submittedAt status"

func (s *AttestationStore) Create(ctx context.Context, r *AttestationRecord) (*AttestationRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Attestation(input: {
			attestationId: %q, sessionId: %q, hostId: %q, blockNumber: %d,
			docCidsReceived: %q, batchSignature: %q,
			submittedAt: %q, status: %q
		}) { %s }
	}`,
		r.AttestationID, r.SessionID, r.HostID, r.BlockNumber,
		r.DocCidsReceived, r.BatchSignature,
		r.SubmittedAt, r.Status,
		attestFields,
	)
	return s.mutateOne(ctx, "create_Scheduler__Attestation", q)
}

func (s *AttestationStore) GetBySessionAndBlock(ctx context.Context, sessionID string, blockN int) (*AttestationRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Attestation(filter: {sessionId: {_eq: %q}, blockNumber: {_eq: %d}}) {
			%s
		}
	}`, sessionID, blockN, attestFields)
	return s.querySingle(ctx, q)
}

func (s *AttestationStore) ListBySession(ctx context.Context, sessionID string) ([]AttestationRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Attestation(filter: {sessionId: {_eq: %q}}) { %s }
	}`, sessionID, attestFields)
	return s.queryMany(ctx, q)
}

// ListPending returns all attestations in pending status.
func (s *AttestationStore) ListPending(ctx context.Context) ([]AttestationRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Attestation(filter: {status: {_eq: %q}}) { %s }
	}`, StatusPending, attestFields)
	return s.queryMany(ctx, q)
}

func (s *AttestationStore) UpdateStatus(ctx context.Context, docID, status string) error {
	return s.Update(ctx, docID, map[string]any{"status": status})
}

func (s *AttestationStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__Attestation(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update attestation %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *AttestationStore) querySingle(ctx context.Context, q string) (*AttestationRecord, error) {
	many, err := s.queryMany(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, nil
	}
	return &many[0], nil
}

func (s *AttestationStore) queryMany(ctx context.Context, q string) ([]AttestationRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query attestations: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerAttestation []AttestationRecord `json:"Scheduler__Attestation"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerAttestation, nil
}

func (s *AttestationStore) mutateOne(ctx context.Context, key, q string) (*AttestationRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate attestation: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]AttestationRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
