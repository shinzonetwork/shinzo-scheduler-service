package payment

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TxEvent is a single Cosmos/Tendermint ABCI event from a transaction result.
type TxEvent struct {
	Type       string `json:"type"`
	Attributes []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"attributes"`
}

// TxResult holds the relevant fields from the Tendermint /tx RPC response.
type TxResult struct {
	Hash   string    `json:"hash"`
	Events []TxEvent `json:"events"`
}

// Client queries the Tendermint REST RPC for transaction details.
type Client struct {
	rpcURL     string
	httpClient *http.Client
}

func NewClient(rpcURL string) *Client {
	return &Client{
		rpcURL: strings.TrimRight(rpcURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetTx retrieves a transaction by its hex hash (with or without leading "0x").
func (c *Client) GetTx(ctx context.Context, txHash string) (*TxResult, error) {
	normalized := strings.TrimPrefix(txHash, "0x")
	if _, err := hex.DecodeString(normalized); err != nil || len(normalized) != 64 {
		return nil, fmt.Errorf("invalid tx hash: %q", txHash)
	}

	url := fmt.Sprintf("%s/tx?hash=0x%s", c.rpcURL, normalized)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tx lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tx lookup: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Tendermint wraps the result in: {"jsonrpc":"2.0","result":{"hash":"...","tx_result":{"events":[...]}}}
	var envelope struct {
		Result struct {
			Hash     string `json:"hash"`
			TxResult struct {
				Events []TxEvent `json:"events"`
			} `json:"tx_result"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("parse tx response: %w", err)
	}

	return &TxResult{
		Hash:   envelope.Result.Hash,
		Events: envelope.Result.TxResult.Events,
	}, nil
}

// VerifySubscriptionPayment checks that the transaction contains a SubscriptionCreated
// event matching the given subscription ID.
func (c *Client) VerifySubscriptionPayment(ctx context.Context, txHash, subscriptionID string) error {
	tx, err := c.GetTx(ctx, txHash)
	if err != nil {
		return err
	}
	for _, ev := range tx.Events {
		if ev.Type != EventSubscriptionCreated {
			continue
		}
		attrs := attributeMap(ev.Attributes)
		if id := attrs["subscription_id"]; id == subscriptionID {
			return nil
		}
	}
	return fmt.Errorf("no matching SubscriptionCreated event for subscription %s in tx %s", subscriptionID, txHash)
}
