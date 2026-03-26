package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// SubscriptionStore provides CRUD operations on Scheduler__Subscription.
type SubscriptionStore struct {
	db dbClient
}

func NewSubscriptionStore(db dbClient) *SubscriptionStore {
	return &SubscriptionStore{db: db}
}

func (s *SubscriptionStore) GetByID(ctx context.Context, subID string) (*SubscriptionRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Subscription(filter: {subscriptionId: {_eq: %q}}) {
			_docID subscriptionId hostId indexerId subType blockFrom blockTo
			status paymentRef createdAt activatedAt expiresAt metadata
		}
	}`, subID)
	return s.querySingle(ctx, q)
}

func (s *SubscriptionStore) ListByHost(ctx context.Context, hostID string) ([]SubscriptionRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Subscription(filter: {hostId: {_eq: %q}}) {
			_docID subscriptionId hostId indexerId subType blockFrom blockTo
			status paymentRef createdAt activatedAt expiresAt metadata
		}
	}`, hostID)
	return s.queryMany(ctx, q)
}

func (s *SubscriptionStore) ListByStatus(ctx context.Context, status string) ([]SubscriptionRecord, error) {
	q := fmt.Sprintf(`query {
		Scheduler__Subscription(filter: {status: {_eq: %q}}) {
			_docID subscriptionId hostId indexerId subType blockFrom blockTo
			status paymentRef createdAt activatedAt expiresAt metadata
		}
	}`, status)
	return s.queryMany(ctx, q)
}

func (s *SubscriptionStore) Create(ctx context.Context, r *SubscriptionRecord) (*SubscriptionRecord, error) {
	q := fmt.Sprintf(`mutation {
		create_Scheduler__Subscription(input: {
			subscriptionId: %q, hostId: %q, indexerId: %q, subType: %q,
			blockFrom: %d, blockTo: %d, status: %q, paymentRef: %q,
			createdAt: %q, activatedAt: %q, expiresAt: %q, metadata: %q
		}) {
			_docID subscriptionId hostId indexerId subType blockFrom blockTo
			status paymentRef createdAt activatedAt expiresAt metadata
		}
	}`,
		r.SubscriptionID, r.HostID, r.IndexerID, r.SubType,
		r.BlockFrom, r.BlockTo, r.Status, r.PaymentRef,
		r.CreatedAt, r.ActivatedAt, r.ExpiresAt, r.Metadata,
	)
	return s.mutateOne(ctx, "create_Scheduler__Subscription", q)
}

func (s *SubscriptionStore) Update(ctx context.Context, docID string, fields map[string]any) error {
	input := buildInputLiteral(fields)
	q := fmt.Sprintf(`mutation {
		update_Scheduler__Subscription(docID: %q, input: {%s}) { _docID }
	}`, docID, input)
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return fmt.Errorf("update subscription %s: %v", docID, res.GQL.Errors)
	}
	return nil
}

func (s *SubscriptionStore) querySingle(ctx context.Context, q string) (*SubscriptionRecord, error) {
	many, err := s.queryMany(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, nil
	}
	return &many[0], nil
}

func (s *SubscriptionStore) queryMany(ctx context.Context, q string) ([]SubscriptionRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("query subscriptions: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		SchedulerSubscription []SubscriptionRecord `json:"Scheduler__Subscription"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.SchedulerSubscription, nil
}

func (s *SubscriptionStore) mutateOne(ctx context.Context, key, q string) (*SubscriptionRecord, error) {
	res := s.db.ExecRequest(ctx, q)
	if len(res.GQL.Errors) > 0 {
		return nil, fmt.Errorf("mutate subscription: %v", res.GQL.Errors)
	}
	raw, err := json.Marshal(res.GQL.Data)
	if err != nil {
		return nil, err
	}
	var wrapper map[string][]SubscriptionRecord
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	recs, ok := wrapper[key]
	if !ok || len(recs) == 0 {
		return nil, fmt.Errorf("mutation returned no records")
	}
	return &recs[0], nil
}
