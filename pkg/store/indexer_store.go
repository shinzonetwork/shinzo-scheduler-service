package store

import (
	"context"
	"encoding/json"
	"fmt"
)

type IndexerStore struct {
	db dbClient
}

func NewIndexerStore(db dbClient) *IndexerStore {
	return &IndexerStore{db: db}
}

func (s *IndexerStore) GetByPeerID(ctx context.Context, peerID string) (*IndexerRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Indexer(filter: {peerId: {_eq: %q}}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status
		}
	}`, peerID)
	return s.querySingle(ctx, q)
}

func (s *IndexerStore) GetByDefraPK(ctx context.Context, defraPK string) (*IndexerRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Indexer(filter: {defraPk: {_eq: %q}}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status
		}
	}`, defraPK)
	return s.querySingle(ctx, q)
}

func (s *IndexerStore) ListActive(ctx context.Context, chain, network string) ([]IndexerRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Indexer(filter: {
			chain: {_eq: %q},
			network: {_eq: %q},
			status: {_eq: "active"}
		}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status
		}
	}`, chain, network)
	return s.queryMany(ctx, q)
}

func (s *IndexerStore) ListAllActive(ctx context.Context) ([]IndexerRecord, error) {
	q := `query {
		Scheduler__Indexer(filter: {status: {_eq: "active"}}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status
		}
	}`
	return s.queryMany(ctx, q)
}

func (s *IndexerStore) Create(ctx context.Context, r *IndexerRecord) (*IndexerRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Indexer(input: {
			peerId: %q, defraPk: %q, httpUrl: %q, multiaddr: %q,
			chain: %q, network: %q, currentTip: %d,
			snapshotRanges: %q, pricing: %q,
			reliabilityScore: %g, lastHeartbeat: %q,
			registeredAt: %q, status: %q
		}) {
			_docID peerId defraPk httpUrl multiaddr chain network currentTip
			snapshotRanges pricing reliabilityScore lastHeartbeat registeredAt
			status
		}
	}`,
		r.PeerID, r.DefraPK, r.HTTPUrl, r.Multiaddr,
		r.Chain, r.Network, r.CurrentTip,
		r.SnapshotRanges, r.Pricing,
		r.ReliabilityScore, r.LastHeartbeat,
		r.RegisteredAt, r.Status,
	)
	return s.mutateOne(ctx, "create_Scheduler__Indexer", q)
}

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

func (s *IndexerStore) Delete(ctx context.Context, docID string) error {
	q := fmt.Sprintf(`mutation { delete_Scheduler__Indexer(docID: %q) { _docID } }`, docID)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("delete indexer %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

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
