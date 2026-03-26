package settlement

import "context"

// HubBroadcaster sends messages to ShinzoHub. Implementations use Tendermint
// RPC broadcast_tx_sync with the scheduler's signing key.
type HubBroadcaster interface {
	BroadcastCloseSession(ctx context.Context, msg MsgCloseSession) (txHash string, err error)
	BroadcastBatchSettlement(ctx context.Context, msg MsgBatchSettlement) (txHash string, err error)
	BroadcastLowCredit(ctx context.Context, msg MsgSignalLowCredit) (txHash string, err error)
	BroadcastSlash(ctx context.Context, msg MsgSlash) (txHash string, err error)
}

// NoopBroadcaster is a placeholder that logs but does not broadcast.
// Used when the settlement subsystem is enabled but no key is configured.
type NoopBroadcaster struct{}

func (NoopBroadcaster) BroadcastCloseSession(_ context.Context, _ MsgCloseSession) (string, error) {
	return "", nil
}
func (NoopBroadcaster) BroadcastBatchSettlement(_ context.Context, _ MsgBatchSettlement) (string, error) {
	return "", nil
}
func (NoopBroadcaster) BroadcastLowCredit(_ context.Context, _ MsgSignalLowCredit) (string, error) {
	return "", nil
}
func (NoopBroadcaster) BroadcastSlash(_ context.Context, _ MsgSlash) (string, error) {
	return "", nil
}
