package settlement

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// SessionCloser handles session termination paths.
type SessionCloser struct {
	batch *BatchProcessor
	hub   HubBroadcaster
	log   *zap.SugaredLogger
}

func NewSessionCloser(batch *BatchProcessor, hub HubBroadcaster, log *zap.SugaredLogger) *SessionCloser {
	return &SessionCloser{batch: batch, hub: hub, log: log}
}

// CloseSession terminates a session by any path and submits MsgCloseSession.
func (sc *SessionCloser) CloseSession(ctx context.Context, sessionID, reason string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	// Process as a single-session batch for consistency.
	return sc.batch.ProcessBatch(ctx, []string{sessionID}, reason)
}
