package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// HostStore provides CRUD operations on the Scheduler__Host collection.
type HostStore struct {
	db dbClient
}

func NewHostStore(db dbClient) *HostStore {
	return &HostStore{db: db}
}

func (s *HostStore) GetByPeerID(ctx context.Context, peerID string) (*HostRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Host(filter: {peerId: {_eq: %q}}) {
			_docID peerId defraPk httpUrl multiaddr chain network
			lastHeartbeat registeredAt status apiKeyHash budget
		}
	}`, peerID)
	return s.querySingle(ctx, q)
}

func (s *HostStore) GetByDefraPK(ctx context.Context, defraPK string) (*HostRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Host(filter: {defraPk: {_eq: %q}}) {
			_docID peerId defraPk httpUrl multiaddr chain network
			lastHeartbeat registeredAt status apiKeyHash budget
		}
	}`, defraPK)
	return s.querySingle(ctx, q)
}

func (s *HostStore) Create(ctx context.Context, r *HostRecord) (*HostRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Host(input: {
			peerId: %q, defraPk: %q, httpUrl: %q, multiaddr: %q,
			chain: %q, network: %q, lastHeartbeat: %q,
			registeredAt: %q, status: %q, apiKeyHash: %q, budget: %q
		}) {
			_docID peerId defraPk httpUrl multiaddr chain network
			lastHeartbeat registeredAt status apiKeyHash budget
		}
	}`,
		r.PeerID, r.DefraPK, r.HTTPUrl, r.Multiaddr,
		r.Chain, r.Network, r.LastHeartbeat,
		r.RegisteredAt, r.Status, r.APIKeyHash, r.Budget,
	)
	return s.mutateOne(ctx, "create_Scheduler__Host", q)
}

func (s *HostStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__Host(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update host %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *HostStore) Delete(ctx context.Context, docID string) error {
	q := fmt.Sprintf(`mutation { delete_Scheduler__Host(docID: %q) { _docID } }`, docID)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("delete host %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

// UpdateAPIKeyHash replaces the stored API key hash for a host.
func (s *HostStore) UpdateAPIKeyHash(ctx context.Context, docID, hash string) error {
	return s.Update(ctx, docID, map[string]any{"apiKeyHash": hash})
}

func (s *HostStore) Count(ctx context.Context, status string) (int, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Host(filter: {status: {_eq: %q}}) { _docID }
	}`, status)
	recs, err := s.queryMany(ctx, q)
	if err != nil {
		return 0, err
	}
	return len(recs), nil
}

func (s *HostStore) querySingle(ctx context.Context, q string) (*HostRecord, error) {
	many, err := s.queryMany(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, nil
	}
	return &many[0], nil
}

func (s *HostStore) queryMany(ctx context.Context, q string) ([]HostRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query hosts: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerHost []HostRecord `json:"Scheduler__Host"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerHost, nil
}

func (s *HostStore) mutateOne(ctx context.Context, key, q string) (*HostRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate host: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]HostRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
