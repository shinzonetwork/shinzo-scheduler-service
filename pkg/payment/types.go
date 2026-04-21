package payment

type Quote struct {
	IndexerID   string  `json:"indexer_id"`
	SubType     string  `json:"sub_type"`
	BlockFrom   int     `json:"block_from,omitempty"`
	BlockTo     int     `json:"block_to,omitempty"`
	PriceTokens float64 `json:"price_tokens"`
	Currency    string  `json:"currency"`    // e.g. "SHINZO"
	ValidUntil  string  `json:"valid_until"` // RFC3339
}

// VerifyPaymentRequest activates a subscription. TxHash is optional: when set and
// a TxVerifier is configured, the payment is verified on-chain against ShinzoHub;
// otherwise activation is trust-based on PaymentRef alone.
type VerifyPaymentRequest struct {
	SubscriptionID string `json:"subscription_id"`
	PaymentRef     string `json:"payment_ref"`
	ExpiresAt      string `json:"expires_at"` // RFC3339; empty = no expiry
	TxHash         string `json:"tx_hash,omitempty"`
}
