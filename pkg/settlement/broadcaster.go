package settlement

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// TendermintBroadcaster broadcasts settlement messages to ShinzoHub via
// the Tendermint JSON-RPC broadcast_tx_sync endpoint.
type TendermintBroadcaster struct {
	rpcURL      string
	schedulerID string
	httpClient  *http.Client
	log         *zap.SugaredLogger
}

// broadcastResponse mirrors the relevant fields of a Tendermint broadcast_tx_sync reply.
type broadcastResponse struct {
	Result struct {
		Code int    `json:"code"`
		Hash string `json:"hash"`
		Log  string `json:"log"`
	} `json:"result"`
}

// NewTendermintBroadcaster returns a broadcaster that POSTs transactions to the
// given Tendermint RPC URL.
func NewTendermintBroadcaster(rpcURL, schedulerID string, log *zap.SugaredLogger) *TendermintBroadcaster {
	return &TendermintBroadcaster{
		rpcURL:      strings.TrimRight(rpcURL, "/"),
		schedulerID: schedulerID,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		log:         log,
	}
}

func (b *TendermintBroadcaster) BroadcastCloseSession(ctx context.Context, msg MsgCloseSession) (string, error) {
	return b.broadcast(ctx, "close_session", msg)
}

func (b *TendermintBroadcaster) BroadcastBatchSettlement(ctx context.Context, msg MsgBatchSettlement) (string, error) {
	return b.broadcast(ctx, "batch_settlement", msg)
}

func (b *TendermintBroadcaster) BroadcastLowCredit(ctx context.Context, msg MsgSignalLowCredit) (string, error) {
	return b.broadcast(ctx, "signal_low_credit", msg)
}

func (b *TendermintBroadcaster) BroadcastSlash(ctx context.Context, msg MsgSlash) (string, error) {
	return b.broadcast(ctx, "slash", msg)
}

// broadcast serialises the message, base64-encodes it, and POSTs it to
// Tendermint's broadcast_tx_sync JSON-RPC endpoint.
func (b *TendermintBroadcaster) broadcast(ctx context.Context, msgType string, payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal %s: %w", msgType, err)
	}

	encoded := base64.StdEncoding.EncodeToString(raw)

	rpcBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "broadcast_tx_sync",
		"params": map[string]string{
			"tx": encoded,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal rpc envelope: %w", err)
	}

	url := b.rpcURL + "/broadcast_tx_sync"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(rpcBody)))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("broadcast %s: %w", msgType, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var br broadcastResponse
	if err := json.Unmarshal(body, &br); err != nil {
		return "", fmt.Errorf("parse broadcast response: %w", err)
	}

	if br.Result.Code != 0 {
		return "", fmt.Errorf("broadcast %s failed: code=%d log=%s", msgType, br.Result.Code, br.Result.Log)
	}

	b.log.Infof("broadcast %s tx=%s scheduler=%s", msgType, br.Result.Hash, b.schedulerID)
	return br.Result.Hash, nil
}
