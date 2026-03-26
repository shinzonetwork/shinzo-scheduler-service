package subscription

// CreateRequest is the body for POST /v1/subscriptions.
type CreateRequest struct {
	HostID    string `json:"host_id"`
	IndexerID string `json:"indexer_id"`
	SubType   string `json:"sub_type"`   // tip | snapshot
	BlockFrom int    `json:"block_from"` // required for snapshot
	BlockTo   int    `json:"block_to"`   // required for snapshot
	Metadata  string `json:"metadata"`   // optional JSON
}

// ActivateRequest is used by POST /v1/payments/verify to activate a pending subscription.
type ActivateRequest struct {
	SubscriptionID string `json:"subscription_id"`
	PaymentRef     string `json:"payment_ref"`
	ExpiresAt      string `json:"expires_at"` // RFC3339; empty = no expiry
}
