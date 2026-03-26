package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// IndexerStore provides CRUD operations on the Scheduler__Indexer collection.
type IndexerStore struct {
	db dbClient
}

func NewIndexerStore(db dbClient) *IndexerStore {
	return &IndexerStore{db: db}
}

// GetByPeerID returns the indexer record for the given peer ID, or nil if not found.
func (s *IndexerStore) GetByPeerID(ctx context.Context, peerID string) (*IndexerRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Indexer(filter: {peerId: {_eq: %q}}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status apiKeyHash
		}
	}`, peerID)
	return s.querySingle(ctx, q)
}

// GetByDefraPK returns the indexer record with the given secp256k1 public key.
func (s *IndexerStore) GetByDefraPK(ctx context.Context, defraPK string) (*IndexerRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Indexer(filter: {defraPk: {_eq: %q}}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status apiKeyHash
		}
	}`, defraPK)
	return s.querySingle(ctx, q)
}

// ListActive returns all indexers with status=active for the given chain+network.
func (s *IndexerStore) ListActive(ctx context.Context, chain, network string) ([]IndexerRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Indexer(filter: {
			chain: {_eq: %q},
			network: {_eq: %q},
			status: {_eq: "active"}
		}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status apiKeyHash
		}
	}`, chain, network)
	return s.queryMany(ctx, q)
}

// ListAllActive returns all indexers with status=active across all chains.
func (s *IndexerStore) ListAllActive(ctx context.Context) ([]IndexerRecord, error) {
	q := `query {
		Scheduler__Indexer(filter: {status: {_eq: "active"}}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status apiKeyHash
		}
	}`
	return s.queryMany(ctx, q)
}

// Create inserts a new indexer document. Returns the created record with _docID populated.
func (s *IndexerStore) Create(ctx context.Context, r *IndexerRecord) (*IndexerRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Indexer(input: {
			peerId: %q, defraPk: %q, httpUrl: %q, multiaddr: %q,
			chain: %q, network: %q, currentTip: %d,
			snapshotRanges: %q, pricing: %q,
			reliabilityScore: %g, lastHeartbeat: %q,
			registeredAt: %q, status: %q, apiKeyHash: %q
		}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status apiKeyHash
		}
	}`,
		r.PeerID, r.DefraPK, r.HTTPUrl, r.Multiaddr,
		r.Chain, r.Network, r.CurrentTip,
		r.SnapshotRanges, r.Pricing,
		r.ReliabilityScore, r.LastHeartbeat,
		r.RegisteredAt, r.Status, r.APIKeyHash,
	)
	return s.mutateOne(ctx, "create_Scheduler__Indexer", q)
}

// Update applies partial updates to an existing indexer document by _docID.
func (s *IndexerStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__Indexer(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update indexer %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

// Delete removes an indexer document by _docID.
func (s *IndexerStore) Delete(ctx context.Context, docID string) error {
	q := fmt.Sprintf(`mutation { delete_Scheduler__Indexer(docID: %q) { _docID } }`, docID)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("delete indexer %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

// UpdateAPIKeyHash replaces the stored API key hash for an indexer.
func (s *IndexerStore) UpdateAPIKeyHash(ctx context.Context, docID, hash string) error {
	return s.Update(ctx, docID, map[string]any{"apiKeyHash": hash})
}

// Count returns the number of indexers matching status.
func (s *IndexerStore) Count(ctx context.Context, status string) (int, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Indexer(filter: {status: {_eq: %q}}) { _docID }
	}`, status)
	recs, err := s.queryMany(ctx, q)
	if err != nil {
		return 0, err
	}
	return len(recs), nil
}

// --- helpers ---

func (s *IndexerStore) querySingle(ctx context.Context, q string) (*IndexerRecord, error) {
	many, err := s.queryMany(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, nil
	}
	return &many[0], nil
}

func (s *IndexerStore) queryMany(ctx context.Context, q string) ([]IndexerRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query indexers: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerIndexer []IndexerRecord `json:"Scheduler__Indexer"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerIndexer, nil
}

func (s *IndexerStore) mutateOne(ctx context.Context, key, q string) (*IndexerRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate indexer: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]IndexerRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
