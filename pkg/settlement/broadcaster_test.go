package settlement

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func successHandler(hash string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"code": 0,
				"hash": hash,
				"log":  "",
			},
		})
	}
}

func failureHandler(code int, log string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"code": code,
				"hash": "",
				"log":  log,
			},
		})
	}
}

func TestNewTendermintBroadcaster(t *testing.T) {
	b := NewTendermintBroadcaster("http://localhost:26657/", "node-1", testLogger())
	assert.Equal(t, "http://localhost:26657", b.rpcURL)
	assert.Equal(t, "node-1", b.schedulerID)
	assert.NotNil(t, b.httpClient)
	assert.NotNil(t, b.log)
}

func TestBroadcastCloseSession_Success(t *testing.T) {
	ts := httptest.NewServer(successHandler("AABB1122"))
	defer ts.Close()

	b := NewTendermintBroadcaster(ts.URL, "s1", testLogger())
	hash, err := b.BroadcastCloseSession(context.Background(), MsgCloseSession{
		SessionID:      "sess-1",
		CloseReason:    "completed",
		BlocksVerified: 100,
		IndexerAmount:  50.0,
		HostRefund:     10.0,
		VerdictCID:     "QmTest",
	})
	require.NoError(t, err)
	assert.Equal(t, "AABB1122", hash)
}

func TestBroadcastBatchSettlement_Success(t *testing.T) {
	ts := httptest.NewServer(successHandler("CC33DD44"))
	defer ts.Close()

	b := NewTendermintBroadcaster(ts.URL, "s1", testLogger())
	hash, err := b.BroadcastBatchSettlement(context.Background(), MsgBatchSettlement{
		BatchID: "batch-1",
		Sessions: []MsgCloseSession{
			{SessionID: "s1", CloseReason: "done", BlocksVerified: 10, IndexerAmount: 5},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "CC33DD44", hash)
}

func TestBroadcastLowCredit_Success(t *testing.T) {
	ts := httptest.NewServer(successHandler("EE55FF66"))
	defer ts.Close()

	b := NewTendermintBroadcaster(ts.URL, "s1", testLogger())
	hash, err := b.BroadcastLowCredit(context.Background(), MsgSignalLowCredit{
		SessionID:       "sess-2",
		CreditRemaining: 1.5,
		PricePerBlock:   0.01,
	})
	require.NoError(t, err)
	assert.Equal(t, "EE55FF66", hash)
}

func TestBroadcastSlash_Success(t *testing.T) {
	ts := httptest.NewServer(successHandler("1122AABB"))
	defer ts.Close()

	b := NewTendermintBroadcaster(ts.URL, "s1", testLogger())
	hash, err := b.BroadcastSlash(context.Background(), MsgSlash{
		IndexerID:   "idx-1",
		EvidenceCID: "QmEvidence",
		Reason:      "contradiction",
	})
	require.NoError(t, err)
	assert.Equal(t, "1122AABB", hash)
}

func TestBroadcast_NonZeroCode(t *testing.T) {
	ts := httptest.NewServer(failureHandler(5, "insufficient funds"))
	defer ts.Close()

	b := NewTendermintBroadcaster(ts.URL, "s1", testLogger())
	_, err := b.BroadcastCloseSession(context.Background(), MsgCloseSession{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "code=5")
	assert.Contains(t, err.Error(), "insufficient funds")
}

func TestBroadcast_ConnectionError(t *testing.T) {
	b := NewTendermintBroadcaster("http://127.0.0.1:1", "s1", testLogger())
	_, err := b.BroadcastCloseSession(context.Background(), MsgCloseSession{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broadcast close_session")
}

func TestBroadcast_MalformedResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer ts.Close()

	b := NewTendermintBroadcaster(ts.URL, "s1", testLogger())
	_, err := b.BroadcastCloseSession(context.Background(), MsgCloseSession{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse broadcast response")
}

func TestBroadcast_RequestPayload(t *testing.T) {
	// Verify the request body structure and base64-encoded tx.
	var captured []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"code": 0, "hash": "ABCD"},
		})
	}))
	defer ts.Close()

	msg := MsgCloseSession{SessionID: "s1", CloseReason: "test"}
	b := NewTendermintBroadcaster(ts.URL, "s1", testLogger())
	_, err := b.BroadcastCloseSession(context.Background(), msg)
	require.NoError(t, err)

	var rpcReq struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  struct {
			Tx string `json:"tx"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal(captured, &rpcReq))
	assert.Equal(t, "2.0", rpcReq.JSONRPC)
	assert.Equal(t, "broadcast_tx_sync", rpcReq.Method)

	decoded, err := base64.StdEncoding.DecodeString(rpcReq.Params.Tx)
	require.NoError(t, err)

	var got MsgCloseSession
	require.NoError(t, json.Unmarshal(decoded, &got))
	assert.Equal(t, "s1", got.SessionID)
	assert.Equal(t, "test", got.CloseReason)
}
