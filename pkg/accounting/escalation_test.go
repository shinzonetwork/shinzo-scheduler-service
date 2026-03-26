package accounting

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockSlashBroadcaster struct {
	calls []slashCall
	err   error
}

type slashCall struct {
	indexerID   string
	evidenceCID string
	reason      string
}

func (m *mockSlashBroadcaster) BroadcastSlash(_ context.Context, indexerID, evidenceCID, reason string) error {
	m.calls = append(m.calls, slashCall{indexerID, evidenceCID, reason})
	return m.err
}

func TestNewHubEscalator(t *testing.T) {
	log, _ := zap.NewDevelopment()
	bc := &mockSlashBroadcaster{}
	esc := NewHubEscalator(bc, log.Sugar())

	require.NotNil(t, esc)
	assert.Equal(t, bc, esc.broadcaster)
	assert.NotNil(t, esc.log)
}

func TestOnMismatch_Success(t *testing.T) {
	bc := &mockSlashBroadcaster{}
	log, _ := zap.NewDevelopment()
	esc := NewHubEscalator(bc, log.Sugar())

	err := esc.OnMismatch(context.Background(), "sess-1", "claim-1", "attest-1")
	require.NoError(t, err)
	require.Len(t, bc.calls, 1)
	assert.Equal(t, "sess-1", bc.calls[0].indexerID)
	assert.Equal(t, "claim:claim-1,attestation:attest-1", bc.calls[0].evidenceCID)
	assert.Contains(t, bc.calls[0].reason, "cid_mismatch")
	assert.Contains(t, bc.calls[0].reason, "sess-1")
}

func TestOnMismatch_Error(t *testing.T) {
	bc := &mockSlashBroadcaster{err: fmt.Errorf("broadcast failed")}
	log, _ := zap.NewDevelopment()
	esc := NewHubEscalator(bc, log.Sugar())

	err := esc.OnMismatch(context.Background(), "sess-1", "c1", "a1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broadcast failed")
}

func TestOnUnderReportExpired_Success(t *testing.T) {
	bc := &mockSlashBroadcaster{}
	log, _ := zap.NewDevelopment()
	esc := NewHubEscalator(bc, log.Sugar())

	err := esc.OnUnderReportExpired(context.Background(), "sess-2", "claim-2", "attest-2")
	require.NoError(t, err)
	require.Len(t, bc.calls, 1)
	assert.Equal(t, "sess-2", bc.calls[0].indexerID)
	assert.Equal(t, "claim:claim-2,attestation:attest-2", bc.calls[0].evidenceCID)
	assert.Contains(t, bc.calls[0].reason, "under_report_unresolved")
	assert.Contains(t, bc.calls[0].reason, "sess-2")
}

func TestOnUnderReportExpired_Error(t *testing.T) {
	bc := &mockSlashBroadcaster{err: fmt.Errorf("hub unreachable")}
	log, _ := zap.NewDevelopment()
	esc := NewHubEscalator(bc, log.Sugar())

	err := esc.OnUnderReportExpired(context.Background(), "sess-2", "c2", "a2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hub unreachable")
}
