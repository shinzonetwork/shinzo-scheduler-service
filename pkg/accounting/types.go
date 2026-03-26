package accounting

// SubmitClaimRequest is the input for submitting a delivery claim.
type SubmitClaimRequest struct {
	SessionID      string `json:"session_id"`
	IndexerID      string `json:"indexer_id"`
	BlockNumber    int    `json:"block_number"`
	DocCids        string `json:"doc_cids"`         // JSON array of CID strings
	BlockHash      string `json:"block_hash"`
	BatchSignature string `json:"batch_signature"`
}

// SubmitAttestationRequest is the input for submitting a host attestation.
type SubmitAttestationRequest struct {
	SessionID       string `json:"session_id"`
	HostID          string `json:"host_id"`
	BlockNumber     int    `json:"block_number"`
	DocCidsReceived string `json:"doc_cids_received"` // JSON array of CID strings
	BatchSignature  string `json:"batch_signature"`
}

// ComparisonResult holds the outcome of comparing a claim against an attestation.
type ComparisonResult struct {
	SessionID   string
	BlockNumber int
	Outcome     string
	ClaimID     string
	AttestID    string
}
