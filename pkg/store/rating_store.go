package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// RatingStore provides operations on Scheduler__Rating documents.
type RatingStore struct {
	db dbClient
}

func NewRatingStore(db dbClient) *RatingStore {
	return &RatingStore{db: db}
}

func (s *RatingStore) Create(ctx context.Context, r *RatingRecord) error {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Rating(input: {
			indexerId: %q, hostId: %q, score: %g, comment: %q, ratedAt: %q
		}) { _docID }
	}`, r.IndexerID, r.HostID, r.Score, r.Comment, r.RatedAt)

	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("create rating: %v", res.GQL.Errors)
	}
	return nil
}

func (s *RatingStore) ListByIndexer(ctx context.Context, indexerID string) ([]RatingRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Rating(filter: {indexerId: {_eq: %q}}) {
			_docID indexerId hostId score comment ratedAt
		}
	}`, indexerID)
	return s.queryMany(ctx, q)
}

func (s *RatingStore) queryMany(ctx context.Context, q string) ([]RatingRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query ratings: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerRating []RatingRecord `json:"Scheduler__Rating"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerRating, nil
}
