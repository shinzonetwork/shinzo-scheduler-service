package settlement

// MsgCloseSession represents the on-chain session close message.
type MsgCloseSession struct {
	SessionID      string  `json:"session_id"`
	CloseReason    string  `json:"close_reason"`
	BlocksVerified int     `json:"blocks_verified"`
	IndexerAmount  float64 `json:"indexer_amount"`
	HostRefund     float64 `json:"host_refund"`
	VerdictCID     string  `json:"verdict_cid"`
}

// MsgBatchSettlement represents the on-chain batch settlement message.
type MsgBatchSettlement struct {
	BatchID  string            `json:"batch_id"`
	Sessions []MsgCloseSession `json:"sessions"`
}

// MsgSignalLowCredit represents the on-chain low credit notification.
type MsgSignalLowCredit struct {
	SessionID       string  `json:"session_id"`
	CreditRemaining float64 `json:"credit_remaining"`
	PricePerBlock   float64 `json:"price_per_block"`
}

// MsgSlash represents the on-chain slashing message for contradiction proof.
type MsgSlash struct {
	IndexerID   string `json:"indexer_id"`
	EvidenceCID string `json:"evidence_cid"`
	Reason      string `json:"reason"`
}
