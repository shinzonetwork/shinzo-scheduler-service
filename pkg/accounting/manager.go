package accounting

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

type claimStore interface {
	Create(ctx context.Context, r *store.DeliveryClaimRecord) (*store.DeliveryClaimRecord, error)
	GetBySessionAndBlock(ctx context.Context, sessionID string, blockN int) (*store.DeliveryClaimRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]store.DeliveryClaimRecord, error)
	ListPending(ctx context.Context) ([]store.DeliveryClaimRecord, error)
	UpdateStatus(ctx context.Context, docID, status string) error
}

type attestationStore interface {
	Create(ctx context.Context, r *store.AttestationRecord) (*store.AttestationRecord, error)
	GetBySessionAndBlock(ctx context.Context, sessionID string, blockN int) (*store.AttestationRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]store.AttestationRecord, error)
	UpdateStatus(ctx context.Context, docID, status string) error
}

type ledgerStore interface {
	Create(ctx context.Context, r *store.SessionLedgerRecord) (*store.SessionLedgerRecord, error)
	GetBySession(ctx context.Context, sessionID string) (*store.SessionLedgerRecord, error)
	Update(ctx context.Context, docID string, fields map[string]any) error
}

type comparisonStore interface {
	Create(ctx context.Context, r *store.ComparisonResultRecord) (*store.ComparisonResultRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]store.ComparisonResultRecord, error)
}

// EscalationHandler is called when a comparison outcome requires on-chain action.
type EscalationHandler interface {
	OnMismatch(ctx context.Context, sessionID string, claimID, attestID string) error
	OnUnderReportExpired(ctx context.Context, sessionID string, claimID, attestID string) error
}

// Manager handles the accounting lifecycle: delivery claims, attestations, and comparisons.
type Manager struct {
	claimSt    claimStore
	attestSt   attestationStore
	ledgerSt   ledgerStore
	compSt     comparisonStore
	escalation EscalationHandler // nil disables on-chain escalation
	cfg        config.AccountingConfig
	log        *zap.SugaredLogger
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

func NewManager(
	claimSt claimStore,
	attestSt attestationStore,
	ledgerSt ledgerStore,
	compSt comparisonStore,
	cfg config.AccountingConfig,
	log *zap.SugaredLogger,
) *Manager {
	return &Manager{
		claimSt:  claimSt,
		attestSt: attestSt,
		ledgerSt: ledgerSt,
		compSt:   compSt,
		cfg:      cfg,
		log:      log,
		stopCh:   make(chan struct{}),
	}
}

// WithEscalation attaches an escalation handler for mismatch and under-report outcomes.
func (m *Manager) WithEscalation(h EscalationHandler) {
	m.escalation = h
}

// SubmitDeliveryClaim validates and stores a delivery claim from an indexer.
func (m *Manager) SubmitDeliveryClaim(ctx context.Context, req SubmitClaimRequest) (*store.DeliveryClaimRecord, error) {
	if req.SessionID == "" || req.IndexerID == "" || req.BlockNumber <= 0 {
		return nil, fmt.Errorf("session_id, indexer_id, and block_number are required")
	}
	if req.DocCids == "" || req.BlockHash == "" {
		return nil, fmt.Errorf("doc_cids and block_hash are required")
	}

	// Reject duplicate claims with different doc_cids for the same (session, block).
	existing, err := m.claimSt.GetBySessionAndBlock(ctx, req.SessionID, req.BlockNumber)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.DocCids != req.DocCids {
			return nil, fmt.Errorf("duplicate claim for session %s block %d with different doc_cids (content-addressing fraud)", req.SessionID, req.BlockNumber)
		}
		return existing, nil // idempotent
	}

	rec := &store.DeliveryClaimRecord{
		ClaimID:        uuid.New().String(),
		SessionID:      req.SessionID,
		IndexerID:      req.IndexerID,
		BlockNumber:    req.BlockNumber,
		DocCids:        req.DocCids,
		BlockHash:      req.BlockHash,
		BatchSignature: req.BatchSignature,
		SubmittedAt:    time.Now().UTC().Format(time.RFC3339),
		Status:         store.StatusPending,
	}

	created, err := m.claimSt.Create(ctx, rec)
	if err != nil {
		return nil, err
	}
	m.log.Debugw("delivery claim submitted", "session", req.SessionID, "block", req.BlockNumber)
	return created, nil
}

// SubmitAttestation validates and stores a host attestation.
func (m *Manager) SubmitAttestation(ctx context.Context, req SubmitAttestationRequest) (*store.AttestationRecord, error) {
	if req.SessionID == "" || req.HostID == "" || req.BlockNumber <= 0 {
		return nil, fmt.Errorf("session_id, host_id, and block_number are required")
	}
	if req.DocCidsReceived == "" {
		return nil, fmt.Errorf("doc_cids_received is required")
	}

	// Append-only: reject if attestation already exists.
	existing, err := m.attestSt.GetBySessionAndBlock(ctx, req.SessionID, req.BlockNumber)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("attestation already exists for session %s block %d (append-only)", req.SessionID, req.BlockNumber)
	}

	rec := &store.AttestationRecord{
		AttestationID:   uuid.New().String(),
		SessionID:       req.SessionID,
		HostID:          req.HostID,
		BlockNumber:     req.BlockNumber,
		DocCidsReceived: req.DocCidsReceived,
		BatchSignature:  req.BatchSignature,
		SubmittedAt:     time.Now().UTC().Format(time.RFC3339),
		Status:          store.StatusPending,
	}

	created, err := m.attestSt.Create(ctx, rec)
	if err != nil {
		return nil, err
	}
	m.log.Debugw("attestation submitted", "session", req.SessionID, "block", req.BlockNumber)
	return created, nil
}

// CreateSessionLedger initializes a session ledger when a subscription activates.
func (m *Manager) CreateSessionLedger(ctx context.Context, sessionID, hostID, indexerID string, initialEscrow, pricePerBlock float64) error {
	rec := &store.SessionLedgerRecord{
		LedgerID:          uuid.New().String(),
		SessionID:         sessionID,
		HostID:            hostID,
		IndexerID:         indexerID,
		BlocksVerified:    0,
		CreditRemaining:   initialEscrow,
		InitialEscrow:     initialEscrow,
		PricePerBlock:     pricePerBlock,
		LastComparedBlock: 0,
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	_, err := m.ledgerSt.Create(ctx, rec)
	return err
}

// GetSessionLedger returns the current session ledger.
func (m *Manager) GetSessionLedger(ctx context.Context, sessionID string) (*store.SessionLedgerRecord, error) {
	return m.ledgerSt.GetBySession(ctx, sessionID)
}

// GetComparisons returns comparison results for a session.
func (m *Manager) GetComparisons(ctx context.Context, sessionID string) ([]store.ComparisonResultRecord, error) {
	return m.compSt.ListBySession(ctx, sessionID)
}

// StartComparisonLoop runs the comparison engine in the background.
func (m *Manager) StartComparisonLoop(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		interval := time.Duration(m.cfg.ComparisonIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 30 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.runComparisons(ctx)
			}
		}
	}()
	m.log.Infof("accounting comparison loop started (interval=%ds)", m.cfg.ComparisonIntervalSeconds)
}

// Stop halts the comparison loop.
func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// Compare performs the comparison for a specific (session, block) pair.
// It handles all comparison outcomes including clean delivery, under-report, mismatch, and silent parties.
func (m *Manager) Compare(ctx context.Context, sessionID string, blockN int) (*ComparisonResult, error) {
	claim, err := m.claimSt.GetBySessionAndBlock(ctx, sessionID, blockN)
	if err != nil {
		return nil, err
	}
	attest, err := m.attestSt.GetBySessionAndBlock(ctx, sessionID, blockN)
	if err != nil {
		return nil, err
	}

	// Hold comparison until both sides are present (within window).
	if claim == nil && attest == nil {
		return nil, nil
	}

	var outcome string
	claimID := ""
	attestID := ""

	switch {
	case claim != nil && attest != nil:
		claimID = claim.ClaimID
		attestID = attest.AttestationID
		var cmpErr error
		outcome, cmpErr = m.compareDocCids(claim.DocCids, attest.DocCidsReceived)
		if cmpErr != nil {
			return nil, fmt.Errorf("compare session %s block %d: %w", sessionID, blockN, cmpErr)
		}

	case claim != nil && attest == nil:
		// Host silent: indexer claimed but no attestation.
		claimID = claim.ClaimID
		if m.withinAttestationWindow(claim.SubmittedAt) {
			return nil, nil // still within window, wait
		}
		outcome = store.OutcomeHostSilent

	case claim == nil && attest != nil:
		// Indexer silent: host attested but no claim.
		attestID = attest.AttestationID
		if m.withinAttestationWindow(attest.SubmittedAt) {
			return nil, nil // still within window, wait
		}
		outcome = store.OutcomeIndexerSilent
	}

	// Mark documents as confirmed.
	if claim != nil {
		if err := m.claimSt.UpdateStatus(ctx, claim.DocID, "confirmed"); err != nil {
			m.log.Warnw("failed to confirm claim status", "session", sessionID, "block", blockN, "error", err)
		}
	}
	if attest != nil {
		if err := m.attestSt.UpdateStatus(ctx, attest.DocID, "confirmed"); err != nil {
			m.log.Warnw("failed to confirm attestation status", "session", sessionID, "block", blockN, "error", err)
		}
	}

	// Record comparison result.
	compRec := &store.ComparisonResultRecord{
		ComparisonID:  uuid.New().String(),
		SessionID:     sessionID,
		BlockNumber:   blockN,
		Outcome:       outcome,
		ClaimID:       claimID,
		AttestationID: attestID,
		ComparedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := m.compSt.Create(ctx, compRec); err != nil {
		m.log.Warnf("accounting: record comparison: %v", err)
	}

	// Update session ledger on clean delivery.
	if outcome == store.OutcomeClean {
		m.updateLedger(ctx, sessionID, blockN)
	}

	// Escalate mismatch immediately to ShinzoHub.
	if outcome == store.OutcomeMismatch && m.escalation != nil {
		if err := m.escalation.OnMismatch(ctx, sessionID, claimID, attestID); err != nil {
			m.log.Warnf("escalation: mismatch for session %s: %v", sessionID, err)
		}
	}

	return &ComparisonResult{
		SessionID:   sessionID,
		BlockNumber: blockN,
		Outcome:     outcome,
		ClaimID:     claimID,
		AttestID:    attestID,
	}, nil
}

// compareDocCids determines the outcome based on CID comparison.
func (m *Manager) compareDocCids(claimCids, attestCids string) (string, error) {
	var claimed, attested []string
	if err := json.Unmarshal([]byte(claimCids), &claimed); err != nil {
		return "", fmt.Errorf("unmarshal claim CIDs: %w", err)
	}
	if err := json.Unmarshal([]byte(attestCids), &attested); err != nil {
		return "", fmt.Errorf("unmarshal attestation CIDs: %w", err)
	}

	claimSet := make(map[string]bool, len(claimed))
	for _, c := range claimed {
		claimSet[c] = true
	}

	attestSet := make(map[string]bool, len(attested))
	for _, a := range attested {
		attestSet[a] = true
	}

	// Exact match: clean delivery.
	if len(claimed) == len(attested) {
		allMatch := true
		for _, c := range claimed {
			if !attestSet[c] {
				allMatch = false
				break
			}
		}
		if allMatch {
			return store.OutcomeClean, nil
		}
	}

	// Host attested fewer CIDs: under-report.
	if len(attested) < len(claimed) {
		allPresent := true
		for _, a := range attested {
			if !claimSet[a] {
				allPresent = false
				break
			}
		}
		if allPresent {
			return store.OutcomeUnderReport, nil
		}
	}

	// CIDs diverge: mismatch.
	return store.OutcomeMismatch, nil
}

func (m *Manager) withinAttestationWindow(submittedAtStr string) bool {
	submitted, err := time.Parse(time.RFC3339, submittedAtStr)
	if err != nil {
		return false
	}
	window := time.Duration(m.cfg.AttestationWindowSeconds) * time.Second
	return time.Since(submitted) < window
}

func (m *Manager) updateLedger(ctx context.Context, sessionID string, blockN int) {
	ledger, err := m.ledgerSt.GetBySession(ctx, sessionID)
	if err != nil || ledger == nil {
		return
	}
	newVerified := ledger.BlocksVerified + 1
	newCredit := ledger.CreditRemaining - ledger.PricePerBlock
	if newCredit < 0 {
		newCredit = 0
	}
	if err := m.ledgerSt.Update(ctx, ledger.DocID, map[string]any{
		"blocksVerified":    newVerified,
		"creditRemaining":   newCredit,
		"lastComparedBlock": blockN,
		"updatedAt":         time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		m.log.Warnw("failed to update session ledger", "session", sessionID, "block", blockN, "error", err)
	}
}

// runComparisons scans pending claims and runs comparison logic.
func (m *Manager) runComparisons(ctx context.Context) {
	pending, err := m.claimSt.ListPending(ctx)
	if err != nil {
		m.log.Warnf("accounting: list pending claims: %v", err)
		return
	}

	// Deduplicate by (sessionID, blockNumber) and process in order.
	type key struct {
		session string
		block   int
	}
	seen := make(map[key]bool)
	var toCompare []key
	for _, c := range pending {
		k := key{c.SessionID, c.BlockNumber}
		if !seen[k] {
			seen[k] = true
			toCompare = append(toCompare, k)
		}
	}

	sort.Slice(toCompare, func(i, j int) bool {
		if toCompare[i].session != toCompare[j].session {
			return toCompare[i].session < toCompare[j].session
		}
		return toCompare[i].block < toCompare[j].block
	})

	for _, k := range toCompare {
		if _, err := m.Compare(ctx, k.session, k.block); err != nil {
			m.log.Debugw("accounting: compare", "session", k.session, "block", k.block, "error", err)
		}
	}

	// Escalate under-reports that exceeded the grace window.
	m.escalateExpiredUnderReports(ctx)
}

// escalateExpiredUnderReports scans comparison results for under-report outcomes
// older than the grace window and triggers on-chain escalation.
func (m *Manager) escalateExpiredUnderReports(ctx context.Context) {
	if m.escalation == nil || m.cfg.UnderReportGraceSeconds <= 0 {
		return
	}
	grace := time.Duration(m.cfg.UnderReportGraceSeconds) * time.Second

	// Scan all pending claims to find sessions with under-report outcomes.
	pending, err := m.claimSt.ListPending(ctx)
	if err != nil {
		return
	}
	sessions := make(map[string]bool)
	for _, c := range pending {
		sessions[c.SessionID] = true
	}

	for sid := range sessions {
		comparisons, err := m.compSt.ListBySession(ctx, sid)
		if err != nil {
			continue
		}
		for _, comp := range comparisons {
			if comp.Outcome != store.OutcomeUnderReport {
				continue
			}
			comparedAt, err := time.Parse(time.RFC3339, comp.ComparedAt)
			if err != nil {
				continue
			}
			if time.Since(comparedAt) > grace {
				if err := m.escalation.OnUnderReportExpired(ctx, sid, comp.ClaimID, comp.AttestationID); err != nil {
					m.log.Warnf("escalation: under-report grace expired for session %s block %d: %v", sid, comp.BlockNumber, err)
				}
			}
		}
	}
}
