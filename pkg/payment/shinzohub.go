package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Event types emitted by ShinzoHub for payment/subscription flows.
const (
	EventSubscriptionCreated = "SubscriptionCreated"
	EventSubscriptionExpired = "SubscriptionExpired"
)

// SubscriptionCreatedEvent carries the on-chain subscription details.
type SubscriptionCreatedEvent struct {
	SubscriptionID string `json:"subscription_id"`
	HostID         string `json:"host_id"`
	IndexerID      string `json:"indexer_id"`
	SubType        string `json:"sub_type"`
	BlockFrom      int    `json:"block_from"`
	BlockTo        int    `json:"block_to"`
	PaymentAmount  string `json:"payment_amount"`
	ExpiresAt      string `json:"expires_at"`
}

// SubscriptionExpiredEvent is emitted when a subscription's payment runs out.
type SubscriptionExpiredEvent struct {
	SubscriptionID string `json:"subscription_id"`
}

// wsConn abstracts a WebSocket connection for testability.
type wsConn interface {
	WriteJSON(v any) error
	ReadMessage() (int, []byte, error)
	Close() error
}

// wsDialer abstracts WebSocket dialing for testability.
type wsDialer interface {
	Dial(url string, header http.Header) (wsConn, *http.Response, error)
}

type defaultDialer struct{}

func (defaultDialer) Dial(url string, header http.Header) (wsConn, *http.Response, error) {
	return websocket.DefaultDialer.Dial(url, header)
}

type ShinzoHubSubscriber struct {
	rpcURL    string
	epochSize int
	log       *zap.SugaredLogger
	dialer    wsDialer

	OnSubscriptionCreated func(SubscriptionCreatedEvent)
	OnSubscriptionExpired func(SubscriptionExpiredEvent)
}

// NewShinzoHubSubscriber creates a subscriber. epochSize > 0 delays subscription
// activation until the chain has advanced by that many blocks from the event height.
func NewShinzoHubSubscriber(rpcURL string, epochSize int, log *zap.SugaredLogger) *ShinzoHubSubscriber {
	return &ShinzoHubSubscriber{rpcURL: rpcURL, epochSize: epochSize, log: log, dialer: defaultDialer{}}
}

func (s *ShinzoHubSubscriber) WithDialer(d wsDialer) {
	s.dialer = d
}

// Start connects to the Tendermint WebSocket and dispatches events until ctx is cancelled.
func (s *ShinzoHubSubscriber) Start(ctx context.Context) error {
	conn, _, err := s.dialer.Dial(s.rpcURL, nil)
	if err != nil {
		return fmt.Errorf("shinzohub connect: %w", err)
	}

	queries := []string{
		fmt.Sprintf("tm.event='Tx' AND %s.subscription_id EXISTS", EventSubscriptionCreated),
		fmt.Sprintf("tm.event='Tx' AND %s.subscription_id EXISTS", EventSubscriptionExpired),
	}
	for i, q := range queries {
		msg := map[string]any{
			"jsonrpc": "2.0",
			"method":  "subscribe",
			"id":      i + 1,
			"params":  map[string]any{"query": q},
		}
		if err := conn.WriteJSON(msg); err != nil {
			_ = conn.Close()
			return fmt.Errorf("shinzohub subscribe: %w", err)
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			_ = conn.Close()
			return fmt.Errorf("shinzohub subscribe ack: %w", err)
		}
	}

	// Keep-alive ping.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Errorw("shinzohub keepalive goroutine panicked", "recover", r)
			}
		}()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "status", "id": 999, "params": map[string]any{}}); err != nil {
					s.log.Warnf("shinzohub keepalive ping: %v", err)
				}
			}
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Errorw("shinzohub reader goroutine panicked", "recover", r)
			}
		}()
		defer func() { _ = conn.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				_, msg, err := conn.ReadMessage()
				if err != nil {
					s.log.Warnf("shinzohub ws read: %v", err)
					return
				}
				s.dispatch(ctx, msg)
			}
		}
	}()

	s.log.Infof("shinzohub subscriber started (%s)", s.rpcURL)
	return nil
}

func (s *ShinzoHubSubscriber) dispatch(ctx context.Context, raw []byte) {
	var envelope struct {
		Result struct {
			Data struct {
				Value struct {
					TxResult struct {
						Height int64 `json:"height"`
						Result struct {
							Events []struct {
								Type       string `json:"type"`
								Attributes []struct {
									Key   string `json:"key"`
									Value string `json:"value"`
								} `json:"attributes"`
							} `json:"events"`
						} `json:"result"`
					} `json:"TxResult"`
				} `json:"value"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return
	}

	eventHeight := envelope.Result.Data.Value.TxResult.Height

	for _, ev := range envelope.Result.Data.Value.TxResult.Result.Events {
		attrs := attributeMap(ev.Attributes)
		switch ev.Type {
		case EventSubscriptionCreated:
			if s.OnSubscriptionCreated == nil {
				continue
			}
			data, ok := attrs["data"]
			if !ok {
				s.log.Warnw("shinzohub: SubscriptionCreated event missing 'data' attribute")
				continue
			}
			var e SubscriptionCreatedEvent
			if err := json.Unmarshal([]byte(data), &e); err != nil {
				s.log.Warnw("shinzohub: failed to parse SubscriptionCreated data", "error", err)
				continue
			}
			if err := validateSubscriptionCreated(e); err != nil {
				s.log.Warnw("shinzohub: malformed SubscriptionCreated event", "error", err)
				continue
			}
			if s.epochSize > 0 && eventHeight > 0 {
				go s.waitEpochThenActivate(ctx, eventHeight, e)
			} else {
				s.OnSubscriptionCreated(e)
			}

		case EventSubscriptionExpired:
			if s.OnSubscriptionExpired == nil {
				continue
			}
			subID := attrs["subscription_id"]
			if subID == "" {
				s.log.Warnw("shinzohub: SubscriptionExpired event missing subscription_id")
				continue
			}
			s.OnSubscriptionExpired(SubscriptionExpiredEvent{SubscriptionID: subID})
		}
	}
}

// waitEpochThenActivate polls the chain height every 2 seconds until the chain
// has advanced by epochSize blocks from fromHeight, then fires OnSubscriptionCreated.
func (s *ShinzoHubSubscriber) waitEpochThenActivate(ctx context.Context, fromHeight int64, ev SubscriptionCreatedEvent) {
	target := fromHeight + int64(s.epochSize)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			h, err := s.GetChainHeight(ctx)
			if err != nil {
				s.log.Warnf("shinzohub: get chain height: %v", err)
				continue
			}
			if h >= target {
				s.OnSubscriptionCreated(ev)
				return
			}
		}
	}
}

// GetChainHeight queries the Tendermint /status endpoint and returns the latest block height.
func (s *ShinzoHubSubscriber) GetChainHeight(ctx context.Context) (int64, error) {
	url := "http://" + s.rpcURL + "/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("status request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var status struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return 0, fmt.Errorf("parse status: %w", err)
	}
	h, err := strconv.ParseInt(status.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse block height %q: %w", status.Result.SyncInfo.LatestBlockHeight, err)
	}
	return h, nil
}

// validateSubscriptionCreated returns an error when required fields are absent.
func validateSubscriptionCreated(e SubscriptionCreatedEvent) error {
	missing := []string{}
	if e.SubscriptionID == "" {
		missing = append(missing, "subscription_id")
	}
	if e.HostID == "" {
		missing = append(missing, "host_id")
	}
	if e.IndexerID == "" {
		missing = append(missing, "indexer_id")
	}
	if e.ExpiresAt == "" {
		missing = append(missing, "expires_at")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing fields: %v", missing)
	}
	return nil
}

func attributeMap(attrs []struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value
	}
	return m
}
