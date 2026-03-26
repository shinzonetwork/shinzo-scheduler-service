package payment

// Quote is a pricing estimate returned by GET /v1/quotes.
type Quote struct {
	IndexerID   string  `json:"indexer_id"`
	SubType     string  `json:"sub_type"`
	BlockFrom   int     `json:"block_from,omitempty"`
	BlockTo     int     `json:"block_to,omitempty"`
	PriceTokens float64 `json:"price_tokens"`
	Currency    string  `json:"currency"`    // e.g. "SHINZO"
	ValidUntil  string  `json:"valid_until"` // RFC3339
}

// VerifyPaymentRequest is the body for POST /v1/payments/verify.
// In Phase 1 this is trust-based (operator signs off-chain).
// Phase 3 will verify a ShinzoHub transaction hash.
type VerifyPaymentRequest struct {
	SubscriptionID string `json:"subscription_id"`
	PaymentRef     string `json:"payment_ref"`
	ExpiresAt      string `json:"expires_at"` // RFC3339; empty = no expiry
	// Phase 3: ShinzoHub on-chain proof
	TxHash string `json:"tx_hash,omitempty"`
}
