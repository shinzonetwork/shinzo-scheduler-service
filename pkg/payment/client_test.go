package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// txResponse builds the Tendermint RPC envelope that GetTx expects.
func txResponse(hash string, events []TxEvent) []byte {
	body, _ := json.Marshal(map[string]any{
		"result": map[string]any{
			"hash": hash,
			"tx_result": map[string]any{
				"events": events,
			},
		},
	})
	return body
}

func TestGetTx_InvalidHash(t *testing.T) {
	c := NewClient("http://localhost:1")
	_, err := c.GetTx(context.Background(), "tooshort")
	assert.Error(t, err)
}

func TestGetTx_WrongHashLength(t *testing.T) {
	c := NewClient("http://localhost:1")
	// 63 hex chars — one short of 64
	_, err := c.GetTx(context.Background(), "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778")
	assert.Error(t, err)
}

func TestGetTx_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.GetTx(context.Background(), "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
	assert.Error(t, err)
}

func TestGetTx_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.GetTx(context.Background(), "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
	assert.Error(t, err)
}

func TestGetTx_Success(t *testing.T) {
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	events := []TxEvent{
		{
			Type: EventSubscriptionCreated,
			Attributes: []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}{{Key: "subscription_id", Value: "sub-1"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, hash)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(txResponse(hash, events))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	tx, err := c.GetTx(context.Background(), hash)
	require.NoError(t, err)
	assert.Equal(t, hash, tx.Hash)
	require.Len(t, tx.Events, 1)
	assert.Equal(t, EventSubscriptionCreated, tx.Events[0].Type)
}

func TestVerifySubscriptionPayment_Match(t *testing.T) {
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	events := []TxEvent{
		{
			Type: EventSubscriptionCreated,
			Attributes: []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}{{Key: "subscription_id", Value: "sub-42"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(txResponse(hash, events))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.VerifySubscriptionPayment(context.Background(), hash, "sub-42")
	assert.NoError(t, err)
}

func TestVerifySubscriptionPayment_WrongID(t *testing.T) {
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	events := []TxEvent{
		{
			Type: EventSubscriptionCreated,
			Attributes: []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}{{Key: "subscription_id", Value: "sub-other"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(txResponse(hash, events))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.VerifySubscriptionPayment(context.Background(), hash, "sub-42")
	assert.Error(t, err)
}

func TestVerifySubscriptionPayment_WrongEventType(t *testing.T) {
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	events := []TxEvent{
		{
			Type: "SomeOtherEvent",
			Attributes: []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}{{Key: "subscription_id", Value: "sub-42"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(txResponse(hash, events))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.VerifySubscriptionPayment(context.Background(), hash, "sub-42")
	assert.Error(t, err)
}

func TestVerifySubscriptionPayment_NoMatchingEvent(t *testing.T) {
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(txResponse(hash, []TxEvent{}))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.VerifySubscriptionPayment(context.Background(), hash, "sub-42")
	assert.Error(t, err)
}

func TestVerifySubscriptionPayment_InvalidHash(t *testing.T) {
	c := NewClient("http://localhost:1")
	err := c.VerifySubscriptionPayment(context.Background(), "tooshort", "sub-1")
	assert.Error(t, err)
}

func TestGetTx_ConnectionRefused(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // nothing listening
	_, err := c.GetTx(context.Background(), "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tx lookup")
}

func TestGetTx_WithLeading0x(t *testing.T) {
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(txResponse(hash, nil))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	tx, err := c.GetTx(context.Background(), "0x"+hash)
	require.NoError(t, err)
	assert.Equal(t, hash, tx.Hash)
}

func TestNewClient(t *testing.T) {
	c := NewClient("http://example.com/")
	assert.Equal(t, "http://example.com", c.rpcURL) // trailing slash stripped
}
