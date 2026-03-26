package accounting

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// SlashBroadcaster is the subset of settlement.HubBroadcaster used for escalation.
type SlashBroadcaster interface {
	BroadcastSlash(ctx context.Context, indexerID, evidenceCID, reason string) error
}

// HubEscalator implements EscalationHandler by broadcasting slash messages to ShinzoHub.
type HubEscalator struct {
	broadcaster SlashBroadcaster
	log         *zap.SugaredLogger
}

func NewHubEscalator(broadcaster SlashBroadcaster, log *zap.SugaredLogger) *HubEscalator {
	return &HubEscalator{broadcaster: broadcaster, log: log}
}

// OnMismatch escalates a CID divergence by submitting evidence to ShinzoHub.
func (h *HubEscalator) OnMismatch(ctx context.Context, sessionID string, claimID, attestID string) error {
	evidenceCID := fmt.Sprintf("claim:%s,attestation:%s", claimID, attestID)
	reason := fmt.Sprintf("cid_mismatch in session %s", sessionID)

	h.log.Warnw("escalating CID mismatch to ShinzoHub",
		"session", sessionID, "claim", claimID, "attestation", attestID)

	return h.broadcaster.BroadcastSlash(ctx, sessionID, evidenceCID, reason)
}

// OnUnderReportExpired escalates an unresolved under-report after the grace window.
func (h *HubEscalator) OnUnderReportExpired(ctx context.Context, sessionID string, claimID, attestID string) error {
	evidenceCID := fmt.Sprintf("claim:%s,attestation:%s", claimID, attestID)
	reason := fmt.Sprintf("under_report_unresolved in session %s", sessionID)

	h.log.Warnw("escalating expired under-report to ShinzoHub",
		"session", sessionID, "claim", claimID, "attestation", attestID)

	return h.broadcaster.BroadcastSlash(ctx, sessionID, evidenceCID, reason)
}
