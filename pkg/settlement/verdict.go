package settlement

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shinzonetwork/shinzo-scheduler-service/config"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"go.uber.org/zap"
)

type verdictStore interface {
	Create(ctx context.Context, r *store.VerdictRecord) (*store.VerdictRecord, error)
	GetBySession(ctx context.Context, sessionID string) (*store.VerdictRecord, error)
	Update(ctx context.Context, docID string, fields map[string]any) error
}

type VerdictManager struct {
	verdictSt verdictStore
	hub       HubBroadcaster
	cfg       config.SettlementConfig
	log       *zap.SugaredLogger
}

func NewVerdictManager(
	verdictSt verdictStore,
	hub HubBroadcaster,
	cfg config.SettlementConfig,
	log *zap.SugaredLogger,
) *VerdictManager {
	return &VerdictManager{
		verdictSt: verdictSt,
		hub:       hub,
		cfg:       cfg,
		log:       log,
	}
}

func (vm *VerdictManager) CreateVerdict(ctx context.Context, sessionID, outcome string, evidenceCids []string) (*store.VerdictRecord, error) {
	evidenceJSON, err := json.Marshal(evidenceCids)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence CIDs: %w", err)
	}

	// Initial signature from this scheduler node.
	sig := fmt.Sprintf("scheduler-node-%s", time.Now().UTC().Format(time.RFC3339Nano))
	sigsJSON, err := json.Marshal([]string{sig})
	if err != nil {
		return nil, fmt.Errorf("marshal signatures: %w", err)
	}

	rec := &store.VerdictRecord{
		VerdictID:           uuid.New().String(),
		SessionID:           sessionID,
		Outcome:             outcome,
		EvidenceCids:        string(evidenceJSON),
		SchedulerSignatures: string(sigsJSON),
		CreatedAt:           time.Now().UTC().Format(time.RFC3339),
		SubmittedToHub:      false,
	}

	created, err := vm.verdictSt.Create(ctx, rec)
	if err != nil {
		return nil, err
	}

	vm.log.Infow("verdict created", "session", sessionID, "outcome", outcome, "verdict_id", rec.VerdictID)
	return created, nil
}

func (vm *VerdictManager) AddSignature(ctx context.Context, sessionID, signature string) error {
	verdict, err := vm.verdictSt.GetBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("lookup verdict for session %s: %w", sessionID, err)
	}
	if verdict == nil {
		return fmt.Errorf("verdict not found for session %s", sessionID)
	}

	var sigs []string
	if err := json.Unmarshal([]byte(verdict.SchedulerSignatures), &sigs); err != nil {
		return fmt.Errorf("corrupt scheduler signatures for session %s: %w", sessionID, err)
	}
	sigs = append(sigs, signature)
	sigsJSON, err := json.Marshal(sigs)
	if err != nil {
		return fmt.Errorf("marshal signatures: %w", err)
	}

	return vm.verdictSt.Update(ctx, verdict.DocID, map[string]any{
		"schedulerSignatures": string(sigsJSON),
	})
}

func (vm *VerdictManager) HasQuorum(ctx context.Context, sessionID string) (bool, error) {
	verdict, err := vm.verdictSt.GetBySession(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("lookup verdict for session %s: %w", sessionID, err)
	}
	if verdict == nil {
		return false, nil
	}

	var sigs []string
	if err := json.Unmarshal([]byte(verdict.SchedulerSignatures), &sigs); err != nil {
		return false, fmt.Errorf("corrupt scheduler signatures for session %s: %w", sessionID, err)
	}

	return len(sigs) >= vm.cfg.VerdictThresholdM, nil
}

func (vm *VerdictManager) SubmitToHub(ctx context.Context, sessionID string) error {
	hasQuorum, err := vm.HasQuorum(ctx, sessionID)
	if err != nil {
		return err
	}
	if !hasQuorum {
		return fmt.Errorf("verdict for session %s does not meet M-of-N threshold (%d required)", sessionID, vm.cfg.VerdictThresholdM)
	}

	verdict, err := vm.verdictSt.GetBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("lookup verdict for session %s: %w", sessionID, err)
	}
	if verdict == nil {
		return fmt.Errorf("verdict not found for session %s", sessionID)
	}

	// Mark as submitted.
	if err := vm.verdictSt.Update(ctx, verdict.DocID, map[string]any{
		"submittedToHub": true,
	}); err != nil {
		return err
	}

	vm.log.Infow("verdict submitted to ShinzoHub", "session", sessionID, "verdict_id", verdict.VerdictID)
	return nil
}
