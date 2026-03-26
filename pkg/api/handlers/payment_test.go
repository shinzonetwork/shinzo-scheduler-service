package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/payment"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	subpkg "github.com/shinzonetwork/shinzo-scheduler-service/pkg/subscription"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mocks ---

type mockSubscriptionActivator struct {
	err error
}

func (m *mockSubscriptionActivator) Activate(_ context.Context, _ subpkg.ActivateRequest) error {
	return m.err
}

type mockIndexerLookup struct {
	record *store.IndexerRecord
	err    error
}

func (m *mockIndexerLookup) GetByPeerID(_ context.Context, _ string) (*store.IndexerRecord, error) {
	return m.record, m.err
}

// --- tests ---

func TestPaymentHandler_Quote_MissingParams(t *testing.T) {
	h := &PaymentHandler{
		mgr:       &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{},
		hostReg:   &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPaymentHandler_Quote_IndexerNotFound(t *testing.T) {
	h := &PaymentHandler{
		mgr:       &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: nil},
		hostReg:   &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPaymentHandler_Quote_TipSuccess(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: `{"tipPer1kBlocks":2.0,"snapshotPerRange":5.0}`,
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip&blocks=1000", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var q payment.Quote
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &q))
	assert.Equal(t, "idx1", q.IndexerID)
	assert.InDelta(t, 2.0, q.PriceTokens, 0.001)
}

func TestPaymentHandler_Quote_SnapshotSuccess(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: `{"tipPer1kBlocks":2.0,"snapshotPerRange":5.0}`,
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=snapshot", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var q payment.Quote
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &q))
	assert.InDelta(t, 5.0, q.PriceTokens, 0.001)
}

func TestPaymentHandler_Quote_BelowFloorPrice(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: `{"tipPer1kBlocks":0.1,"snapshotPerRange":0.5}`,
		}},
		hostReg:               &mockHostVerifier{},
		floorTipPer1kBlocks:   1.0,
		floorSnapshotPerRange: 1.0,
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip&blocks=1000", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPaymentHandler_VerifyPayment_Unauthorized(t *testing.T) {
	h := &PaymentHandler{
		mgr:       &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{},
		hostReg:   &mockHostVerifier{err: errors.New("bad key")},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/payments/verify", bytes.NewBufferString("{}"))
	r = injectAPIKey(r, "bad.key")
	h.VerifyPayment(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPaymentHandler_VerifyPayment_MissingFields(t *testing.T) {
	h := &PaymentHandler{
		mgr:       &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{},
		hostReg:   &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	body, _ := json.Marshal(payment.VerifyPaymentRequest{SubscriptionID: "sub-1"}) // missing payment_ref
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/payments/verify", bytes.NewBuffer(body))
	r = injectAPIKey(r, "host1.key.val")
	h.VerifyPayment(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPaymentHandler_VerifyPayment_Success(t *testing.T) {
	h := &PaymentHandler{
		mgr:       &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{},
		hostReg:   &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	body, _ := json.Marshal(payment.VerifyPaymentRequest{
		SubscriptionID: "sub-1", PaymentRef: "tx-abc",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/payments/verify", bytes.NewBuffer(body))
	r = injectAPIKey(r, "host1.key.val")
	h.VerifyPayment(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPaymentHandler_VerifyPayment_ActivationFails(t *testing.T) {
	h := &PaymentHandler{
		mgr:       &mockSubscriptionActivator{err: errors.New("not pending")},
		indexerSt: &mockIndexerLookup{},
		hostReg:   &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	body, _ := json.Marshal(payment.VerifyPaymentRequest{
		SubscriptionID: "sub-1", PaymentRef: "tx-abc",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/payments/verify", bytes.NewBuffer(body))
	r = injectAPIKey(r, "host1.key.val")
	h.VerifyPayment(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

type mockTxVerifier struct {
	err error
}

func (m *mockTxVerifier) VerifySubscriptionPayment(_ context.Context, _, _ string) error {
	return m.err
}

func TestPaymentHandler_VerifyPayment_BadBody(t *testing.T) {
	h := &PaymentHandler{
		mgr:       &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{},
		hostReg:   &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/payments/verify", bytes.NewBufferString("not-json"))
	r = injectAPIKey(r, "host1.key.val")
	h.VerifyPayment(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPaymentHandler_VerifyPayment_TxVerifierError(t *testing.T) {
	h := &PaymentHandler{
		mgr:        &mockSubscriptionActivator{},
		indexerSt:  &mockIndexerLookup{},
		hostReg:    &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
		txVerifier: &mockTxVerifier{err: errors.New("tx invalid")},
	}
	body, _ := json.Marshal(payment.VerifyPaymentRequest{
		SubscriptionID: "sub-1", PaymentRef: "tx-abc", TxHash: "deadbeefdeadbeef",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/payments/verify", bytes.NewBuffer(body))
	r = injectAPIKey(r, "host1.key.val")
	h.VerifyPayment(w, r)
	assert.Equal(t, http.StatusPaymentRequired, w.Code)
}

func TestPaymentHandler_VerifyPayment_TxVerifierSuccess(t *testing.T) {
	h := &PaymentHandler{
		mgr:        &mockSubscriptionActivator{},
		indexerSt:  &mockIndexerLookup{},
		hostReg:    &mockHostVerifier{record: &store.HostRecord{PeerID: "host1"}},
		txVerifier: &mockTxVerifier{},
	}
	body, _ := json.Marshal(payment.VerifyPaymentRequest{
		SubscriptionID: "sub-1", PaymentRef: "tx-abc", TxHash: "deadbeefdeadbeef",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/payments/verify", bytes.NewBuffer(body))
	r = injectAPIKey(r, "host1.key.val")
	h.VerifyPayment(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPaymentHandler_WithFloorPricing(t *testing.T) {
	h := &PaymentHandler{}
	h.WithFloorPricing(1.0, 2.0)
	assert.Equal(t, 1.0, h.floorTipPer1kBlocks)
	assert.Equal(t, 2.0, h.floorSnapshotPerRange)
}

func TestPaymentHandler_WithTxVerifier(t *testing.T) {
	h := &PaymentHandler{}
	v := &mockTxVerifier{}
	h.WithTxVerifier(v)
	assert.Equal(t, v, h.txVerifier)
}

func TestPaymentHandler_Quote_InvalidType(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: `{"tipPer1kBlocks":1.0,"snapshotPerRange":2.0}`,
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=unknown", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPaymentHandler_Quote_TipDefaultBlocks(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: `{"tipPer1kBlocks":2.0,"snapshotPerRange":5.0}`,
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	// No blocks param — should default to 1000.
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var q payment.Quote
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &q))
	assert.InDelta(t, 2.0, q.PriceTokens, 0.001)
}

func TestPaymentHandler_Quote_EmptyBracesPricing(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: "{}",
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip&blocks=1000", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPaymentHandler_Quote_IndexerLookupError(t *testing.T) {
	h := &PaymentHandler{
		mgr:       &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{err: errors.New("db error")},
		hostReg:   &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPaymentHandler_Quote_SnapshotWithBlockRange(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: `{"tipPer1kBlocks":2.0,"snapshotPerRange":5.0}`,
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=snapshot&block_from=100&block_to=200", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var q payment.Quote
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &q))
	assert.Equal(t, 100, q.BlockFrom)
	assert.Equal(t, 200, q.BlockTo)
}

func TestPaymentHandler_Quote_EmptyPricing(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: "", // empty — should use zero-value pricing
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip&blocks=1000", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNewPaymentHandler(t *testing.T) {
	h := NewPaymentHandler(nil, nil, nil)
	assert.NotNil(t, h)
}

func TestPaymentHandler_Quote_InvalidBlocksParam(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: `{"tipPer1kBlocks":2.0,"snapshotPerRange":5.0}`,
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip&blocks=invalid", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPaymentHandler_Quote_CorruptPricingJSON(t *testing.T) {
	h := &PaymentHandler{
		mgr: &mockSubscriptionActivator{},
		indexerSt: &mockIndexerLookup{record: &store.IndexerRecord{
			PeerID:  "idx1",
			Pricing: "not-json",
		}},
		hostReg: &mockHostVerifier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/quotes?indexer_id=idx1&type=tip&blocks=1000", nil)
	h.Quote(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
