package store

// IndexerRecord mirrors the Scheduler__Indexer DefraDB collection.
// snapshotRanges and pricing are stored as JSON strings.
type IndexerRecord struct {
	DocID            string  `json:"_docID,omitempty"`
	PeerID           string  `json:"peerId"`
	DefraPK          string  `json:"defraPk"`
	HTTPUrl          string  `json:"httpUrl"`
	Multiaddr        string  `json:"multiaddr"`
	Chain            string  `json:"chain"`
	Network          string  `json:"network"`
	CurrentTip       int     `json:"currentTip"`
	SnapshotRanges   string  `json:"snapshotRanges"` // JSON array
	Pricing          string  `json:"pricing"`        // JSON object
	ReliabilityScore float64 `json:"reliabilityScore"`
	LastHeartbeat    string  `json:"lastHeartbeat"` // RFC3339
	RegisteredAt     string  `json:"registeredAt"`
	Status           string  `json:"status"`
	APIKeyHash       string  `json:"apiKeyHash"`
}

// HostRecord mirrors the Scheduler__Host DefraDB collection.
type HostRecord struct {
	DocID         string `json:"_docID,omitempty"`
	PeerID        string `json:"peerId"`
	DefraPK       string `json:"defraPk"`
	HTTPUrl       string `json:"httpUrl"`
	Multiaddr     string `json:"multiaddr"`
	Chain         string `json:"chain"`
	Network       string `json:"network"`
	LastHeartbeat string `json:"lastHeartbeat"`
	RegisteredAt  string `json:"registeredAt"`
	Status        string `json:"status"`
	APIKeyHash    string `json:"apiKeyHash"`
	Budget        string `json:"budget"` // JSON: HostBudget
}

// HostBudget is the parsed form of HostRecord.Budget JSON.
type HostBudget struct {
	MaxTipPer1kBlocks   float64 `json:"maxTipPer1kBlocks"`
	MaxSnapshotPerRange float64 `json:"maxSnapshotPerRange"`
}

// SubscriptionRecord mirrors the Scheduler__Subscription DefraDB collection.
type SubscriptionRecord struct {
	DocID          string `json:"_docID,omitempty"`
	SubscriptionID string `json:"subscriptionId"`
	HostID         string `json:"hostId"`
	IndexerID      string `json:"indexerId"`
	SubType        string `json:"subType"` // tip | snapshot
	BlockFrom      int    `json:"blockFrom"`
	BlockTo        int    `json:"blockTo"`
	Status         string `json:"status"` // pending | active | expired | cancelled
	PaymentRef     string `json:"paymentRef"`
	CreatedAt      string `json:"createdAt"`
	ActivatedAt    string `json:"activatedAt"`
	ExpiresAt      string `json:"expiresAt"`
	Metadata       string `json:"metadata"`
}

// ProbeResultRecord mirrors the Scheduler__ProbeResult DefraDB collection.
type ProbeResultRecord struct {
	DocID     string `json:"_docID,omitempty"`
	IndexerID string `json:"indexerId"`
	ProbedAt  string `json:"probedAt"`
	Success   bool   `json:"success"`
	Tip       int    `json:"tip"`
	LatencyMs int    `json:"latencyMs"`
}

// RatingRecord mirrors the Scheduler__Rating DefraDB collection.
type RatingRecord struct {
	DocID     string  `json:"_docID,omitempty"`
	IndexerID string  `json:"indexerId"`
	HostID    string  `json:"hostId"`
	Score     float64 `json:"score"`
	Comment   string  `json:"comment"`
	RatedAt   string  `json:"ratedAt"`
}

// SnapshotRange is an element in IndexerRecord.SnapshotRanges JSON.
type SnapshotRange struct {
	Start     int    `json:"start"`
	End       int    `json:"end"`
	File      string `json:"file"`
	SizeBytes int64  `json:"sizeBytes"`
	CreatedAt string `json:"createdAt"`
}

// Pricing is the parsed form of IndexerRecord.Pricing JSON.
type Pricing struct {
	TipPer1kBlocks   float64 `json:"tipPer1kBlocks"`
	SnapshotPerRange float64 `json:"snapshotPerRange"`
}

// MatchHistoryRecord mirrors the Scheduler__MatchHistory DefraDB collection.
type MatchHistoryRecord struct {
	DocID         string  `json:"_docID,omitempty"`
	MatchID       string  `json:"matchId"`
	HostID        string  `json:"hostId"`
	IndexerID     string  `json:"indexerId"`
	MatchType     string  `json:"matchType"`
	MatchedAt     string  `json:"matchedAt"`
	ClearingPrice float64 `json:"clearingPrice"`
}

// ContradictionRecord mirrors the Scheduler__Contradiction DefraDB collection.
type ContradictionRecord struct {
	DocID         string `json:"_docID,omitempty"`
	EvidenceID    string `json:"evidenceId"`
	IndexerID     string `json:"indexerId"`
	SnapshotRange string `json:"snapshotRange"` // JSON
	ProbedAt      string `json:"probedAt"`
	Resolved      bool   `json:"resolved"`
}

// DeliveryClaimRecord mirrors the Scheduler__DeliveryClaim DefraDB collection.
type DeliveryClaimRecord struct {
	DocID          string `json:"_docID,omitempty"`
	ClaimID        string `json:"claimId"`
	SessionID      string `json:"sessionId"`
	IndexerID      string `json:"indexerId"`
	BlockNumber    int    `json:"blockNumber"`
	DocCids        string `json:"docCids"` // JSON array of CID strings
	BlockHash      string `json:"blockHash"`
	BatchSignature string `json:"batchSignature"`
	SubmittedAt    string `json:"submittedAt"`
	Status         string `json:"status"`
}

// AttestationRecord mirrors the Scheduler__Attestation DefraDB collection.
type AttestationRecord struct {
	DocID           string `json:"_docID,omitempty"`
	AttestationID   string `json:"attestationId"`
	SessionID       string `json:"sessionId"`
	HostID          string `json:"hostId"`
	BlockNumber     int    `json:"blockNumber"`
	DocCidsReceived string `json:"docCidsReceived"` // JSON array of CID strings
	BatchSignature  string `json:"batchSignature"`
	SubmittedAt     string `json:"submittedAt"`
	Status          string `json:"status"`
}

// SessionLedgerRecord mirrors the Scheduler__SessionLedger DefraDB collection.
type SessionLedgerRecord struct {
	DocID             string  `json:"_docID,omitempty"`
	LedgerID          string  `json:"ledgerId"`
	SessionID         string  `json:"sessionId"`
	HostID            string  `json:"hostId"`
	IndexerID         string  `json:"indexerId"`
	BlocksVerified    int     `json:"blocksVerified"`
	CreditRemaining   float64 `json:"creditRemaining"`
	InitialEscrow     float64 `json:"initialEscrow"`
	PricePerBlock     float64 `json:"pricePerBlock"`
	LastComparedBlock int     `json:"lastComparedBlock"`
	UpdatedAt         string  `json:"updatedAt"`
}

// ComparisonResultRecord mirrors the Scheduler__ComparisonResult DefraDB collection.
type ComparisonResultRecord struct {
	DocID         string `json:"_docID,omitempty"`
	ComparisonID  string `json:"comparisonId"`
	SessionID     string `json:"sessionId"`
	BlockNumber   int    `json:"blockNumber"`
	Outcome       string `json:"outcome"`
	ClaimID       string `json:"claimId"`
	AttestationID string `json:"attestationId"`
	ComparedAt    string `json:"comparedAt"`
}

// EscrowAccountRecord mirrors the Scheduler__EscrowAccount DefraDB collection.
type EscrowAccountRecord struct {
	DocID             string  `json:"_docID,omitempty"`
	EscrowID          string  `json:"escrowId"`
	SessionID         string  `json:"sessionId"`
	HostID            string  `json:"hostId"`
	IndexerID         string  `json:"indexerId"`
	InitialBalance    float64 `json:"initialBalance"`
	CurrentBalance    float64 `json:"currentBalance"`
	PricePerBlock     float64 `json:"pricePerBlock"`
	LowWaterThreshold float64 `json:"lowWaterThreshold"`
	LowCreditSignaled bool    `json:"lowCreditSignaled"`
	GracePeriodEndsAt string  `json:"gracePeriodEndsAt"`
	Status            string  `json:"status"`
	CreatedAt         string  `json:"createdAt"`
	UpdatedAt         string  `json:"updatedAt"`
}

// SettlementRecord mirrors the Scheduler__Settlement DefraDB collection.
type SettlementRecord struct {
	DocID          string  `json:"_docID,omitempty"`
	SettlementID   string  `json:"settlementId"`
	BatchID        string  `json:"batchId"`
	SessionID      string  `json:"sessionId"`
	BlocksVerified int     `json:"blocksVerified"`
	IndexerAmount  float64 `json:"indexerAmount"`
	HostRefund     float64 `json:"hostRefund"`
	CloseReason    string  `json:"closeReason"`
	TxHash         string  `json:"txHash"`
	Status         string  `json:"status"`
	SettledAt      string  `json:"settledAt"`
}

// VerdictRecord mirrors the Scheduler__Verdict DefraDB collection.
type VerdictRecord struct {
	DocID                string `json:"_docID,omitempty"`
	VerdictID            string `json:"verdictId"`
	SessionID            string `json:"sessionId"`
	Outcome              string `json:"outcome"`
	EvidenceCids         string `json:"evidenceCids"`         // JSON array
	SchedulerSignatures  string `json:"schedulerSignatures"`  // JSON array
	CreatedAt            string `json:"createdAt"`
	SubmittedToHub       bool   `json:"submittedToHub"`
}

const (
	StatusActive    = "active"
	StatusInactive  = "inactive"
	StatusBanned    = "banned"
	StatusPending   = "pending"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"

	SubTypeTip      = "tip"
	SubTypeSnapshot = "snapshot"

	// Comparison outcomes.
	OutcomeClean        = "clean_delivery"
	OutcomeUnderReport  = "under_report"
	OutcomeMismatch     = "mismatch"
	OutcomeIndexerSilent = "indexer_silent"
	OutcomeHostSilent   = "host_silent"

	// Close reasons for session settlement.
	CloseReasonHostInitiated    = "host_initiated"
	CloseReasonCreditExhaustion = "credit_exhaustion"
	CloseReasonDispute          = "dispute"
	CloseReasonExpired          = "expired"
)
