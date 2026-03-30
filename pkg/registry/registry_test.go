package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/auth"
	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- mock indexer store ---

type mockIndexerStore struct {
	records   map[string]*store.IndexerRecord
	nextDoc   int
	err       error
	createErr error
	updateErr error
}

func newMockIndexerStore() *mockIndexerStore {
	return &mockIndexerStore{records: make(map[string]*store.IndexerRecord)}
}

func (m *mockIndexerStore) GetByPeerID(_ context.Context, peerID string) (*store.IndexerRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, r := range m.records {
		if r.PeerID == peerID {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockIndexerStore) Create(_ context.Context, r *store.IndexerRecord) (*store.IndexerRecord, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("doc-%d", m.nextDoc)
	cp := *r
	m.records[r.PeerID] = &cp
	return &cp, nil
}

func (m *mockIndexerStore) Update(_ context.Context, docID string, fields map[string]any) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.err != nil {
		return m.err
	}
	for _, r := range m.records {
		if r.DocID == docID {
			if v, ok := fields["status"]; ok {
				r.Status = v.(string)
			}
			if v, ok := fields["currentTip"]; ok {
				r.CurrentTip = v.(int)
			}
			if v, ok := fields["lastHeartbeat"]; ok {
				r.LastHeartbeat = v.(string)
			}
			return nil
		}
	}
	return fmt.Errorf("doc not found: %s", docID)
}

// --- mock host store ---

type mockHostStore struct {
	records   map[string]*store.HostRecord
	nextDoc   int
	err       error
	createErr error
	updateErr error
}

func newMockHostStore() *mockHostStore {
	return &mockHostStore{records: make(map[string]*store.HostRecord)}
}

func (m *mockHostStore) GetByPeerID(_ context.Context, peerID string) (*store.HostRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, r := range m.records {
		if r.PeerID == peerID {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockHostStore) Create(_ context.Context, r *store.HostRecord) (*store.HostRecord, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.err != nil {
		return nil, m.err
	}
	m.nextDoc++
	r.DocID = fmt.Sprintf("doc-%d", m.nextDoc)
	cp := *r
	m.records[r.PeerID] = &cp
	return &cp, nil
}

func (m *mockHostStore) Update(_ context.Context, docID string, fields map[string]any) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.err != nil {
		return m.err
	}
	for _, r := range m.records {
		if r.DocID == docID {
			if v, ok := fields["status"]; ok {
				r.Status = v.(string)
			}
			if v, ok := fields["lastHeartbeat"]; ok {
				r.LastHeartbeat = v.(string)
			}
			return nil
		}
	}
	return fmt.Errorf("doc not found: %s", docID)
}

// --- crypto helpers ---

func generateTestKeypair(t *testing.T) (*secp256k1.PrivateKey, string) {
	t.Helper()
	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	return priv, hex.EncodeToString(priv.PubKey().SerializeCompressed())
}

func signMsg(priv *secp256k1.PrivateKey, msg []byte) string {
	h := sha256.Sum256(msg)
	sig := ecdsa.Sign(priv, h[:])
	return hex.EncodeToString(sig.Serialize())
}

func signedMessages(priv *secp256k1.PrivateKey, peerID string) map[string]string {
	msgHex := hex.EncodeToString([]byte(peerID))
	return map[string]string{msgHex: signMsg(priv, []byte(peerID))}
}

func newTestVerifier() *auth.Verifier {
	return auth.NewVerifier()
}

func newSugaredLogger(t *testing.T) *zap.SugaredLogger {
	t.Helper()
	l, _ := zap.NewDevelopment()
	return l.Sugar()
}

// --- IndexerRegistry tests ---

func TestIndexerRegistry_Register(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]

	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	resp, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID:         peerID,
		DefraPK:        pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth",
		Network:        "mainnet",
		Pricing:        `{"tipPer1kBlocks":0.1,"snapshotPerRange":1.0}`,
	})
	require.NoError(t, err)
	assert.Equal(t, peerID, resp.PeerID)

	// Record should be in store
	rec, err := st.GetByPeerID(context.Background(), peerID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, store.StatusActive, rec.Status)
}

func TestIndexerRegistry_Register_ReRegistration(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	req := RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	}
	// First registration
	_, err := reg.Register(context.Background(), req)
	require.NoError(t, err)
	// Re-registration with different URL
	req.HTTPUrl = "http://newhost:9090"
	resp2, err := reg.Register(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, peerID, resp2.PeerID)
	// Only one record should exist
	assert.Len(t, st.records, 1)
}

func TestIndexerRegistry_Register_ChainMismatch(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "polygon", Network: "mainnet", // wrong chain
	})
	assert.Error(t, err)
}

func TestIndexerRegistry_Register_HeartbeatIntervalInResponse(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 45)

	resp, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	})
	require.NoError(t, err)
	assert.Equal(t, 45, resp.HeartbeatIntervalSeconds)
}

func TestIndexerRegistry_Register_BadSignature(t *testing.T) {
	_, pubHex := generateTestKeypair(t)
	otherPriv, _ := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID:         peerID,
		DefraPK:        pubHex,
		SignedMessages: signedMessages(otherPriv, peerID), // wrong key
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	})
	assert.Error(t, err)
}

func TestIndexerRegistry_Heartbeat(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	})
	require.NoError(t, err)

	err = reg.Heartbeat(context.Background(), peerID, HeartbeatRequest{CurrentTip: 999, SnapshotRanges: "[]"})
	require.NoError(t, err)

	rec, _ := st.GetByPeerID(context.Background(), peerID)
	assert.Equal(t, 999, rec.CurrentTip)
	assert.NotEmpty(t, rec.LastHeartbeat)
}

func TestIndexerRegistry_Heartbeat_NotFound(t *testing.T) {
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	err := reg.Heartbeat(context.Background(), "unknown", HeartbeatRequest{})
	assert.Error(t, err)
}

func TestIndexerRegistry_Deregister(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, _ = reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	})

	err := reg.Deregister(context.Background(), peerID)
	require.NoError(t, err)
	rec, _ := st.GetByPeerID(context.Background(), peerID)
	assert.Equal(t, store.StatusInactive, rec.Status)
}

func TestIndexerRegistry_VerifyRequest(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	})
	require.NoError(t, err)

	token := auth.GenerateToken(priv, peerID)
	rec, err := reg.VerifyRequest(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, peerID, rec.PeerID)

	otherPriv, _ := generateTestKeypair(t)
	badToken := auth.GenerateToken(otherPriv, peerID)
	_, err = reg.VerifyRequest(context.Background(), badToken)
	assert.Error(t, err)
}

func TestIndexerRegistry_UpdateReliability(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, _ = reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	})

	rec, _ := st.GetByPeerID(context.Background(), peerID)
	err := reg.UpdateReliability(context.Background(), rec.DocID, 0.75, 500, "")
	require.NoError(t, err)
}

// --- HostRegistry tests ---

func TestHostRegistry_Register(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]

	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	resp, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID:         peerID,
		DefraPK:        pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:9090",
		Multiaddr:      "/ip4/127.0.0.1/tcp/5001",
		Chain:          "eth",
		Network:        "mainnet",
	})
	require.NoError(t, err)
	assert.Equal(t, peerID, resp.PeerID)
}

func TestHostRegistry_Heartbeat(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, _ = reg.Register(context.Background(), RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:9090",
		Multiaddr:      "/ip4/127.0.0.1/tcp/5001",
		Chain:          "eth", Network: "mainnet",
	})

	before := time.Now().Truncate(time.Second)
	err := reg.Heartbeat(context.Background(), peerID)
	require.NoError(t, err)

	rec, _ := st.GetByPeerID(context.Background(), peerID)
	assert.NotEmpty(t, rec.LastHeartbeat)
	hb, _ := time.Parse(time.RFC3339, rec.LastHeartbeat)
	assert.False(t, hb.Before(before), "heartbeat should be >= %s, got %s", before, hb)
}

func TestHostRegistry_Deregister(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, _ = reg.Register(context.Background(), RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:9090",
		Multiaddr:      "/ip4/127.0.0.1/tcp/5001",
		Chain:          "eth", Network: "mainnet",
	})
	err := reg.Deregister(context.Background(), peerID)
	require.NoError(t, err)

	rec, _ := st.GetByPeerID(context.Background(), peerID)
	assert.Equal(t, store.StatusInactive, rec.Status)
}

func TestHostRegistry_VerifyRequest(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)

	_, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:9090",
		Multiaddr:      "/ip4/127.0.0.1/tcp/5001",
		Chain:          "eth", Network: "mainnet",
	})
	require.NoError(t, err)

	token := auth.GenerateToken(priv, peerID)
	rec, err := reg.VerifyRequest(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, peerID, rec.PeerID)

	otherPriv, _ := generateTestKeypair(t)
	badToken := auth.GenerateToken(otherPriv, peerID)
	_, err = reg.VerifyRequest(context.Background(), badToken)
	assert.Error(t, err)
}

// --- IndexerRegistry additional error-path tests ---

func indexerRegRequest(priv *secp256k1.PrivateKey, pubHex, peerID string) RegisterIndexerRequest {
	return RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	}
}

func TestIndexerRegistry_Register_GetError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := &mockIndexerStore{records: make(map[string]*store.IndexerRecord), err: fmt.Errorf("db down")}
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), indexerRegRequest(priv, pubHex, peerID))
	assert.Error(t, err)
}

func TestIndexerRegistry_Register_ReregistrationUpdateError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), indexerRegRequest(priv, pubHex, peerID))
	require.NoError(t, err)
	st.updateErr = fmt.Errorf("update failed")
	_, err = reg.Register(context.Background(), indexerRegRequest(priv, pubHex, peerID))
	assert.Error(t, err)
}

func TestIndexerRegistry_Register_StoreError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := &mockIndexerStore{records: make(map[string]*store.IndexerRecord), createErr: fmt.Errorf("write fail")}
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), indexerRegRequest(priv, pubHex, peerID))
	assert.Error(t, err)
}

func TestIndexerRegistry_Deregister_NotFound(t *testing.T) {
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	err := reg.Deregister(context.Background(), "unknown")
	assert.Error(t, err)
}

func TestIndexerRegistry_Deregister_GetError(t *testing.T) {
	st := &mockIndexerStore{records: make(map[string]*store.IndexerRecord), err: fmt.Errorf("db down")}
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	err := reg.Deregister(context.Background(), "peer1")
	assert.Error(t, err)
}

func TestIndexerRegistry_Deregister_UpdateError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, _ = reg.Register(context.Background(), indexerRegRequest(priv, pubHex, peerID))
	st.updateErr = fmt.Errorf("update failed")
	err := reg.Deregister(context.Background(), peerID)
	assert.Error(t, err)
}

func TestIndexerRegistry_VerifyRequest_ExtractError(t *testing.T) {
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.VerifyRequest(context.Background(), "nodots")
	assert.Error(t, err)
}

func TestIndexerRegistry_VerifyRequest_NotFound(t *testing.T) {
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.VerifyRequest(context.Background(), "unknown.ts.sig")
	assert.Error(t, err)
}

func TestIndexerRegistry_VerifyRequest_StoreError(t *testing.T) {
	st := &mockIndexerStore{records: make(map[string]*store.IndexerRecord), err: fmt.Errorf("db down")}
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.VerifyRequest(context.Background(), "peer1.ts.sig")
	assert.Error(t, err)
}

func TestIndexerRegistry_VerifyRequest_HashMismatch(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, _ = reg.Register(context.Background(), indexerRegRequest(priv, pubHex, peerID))
	// Fabricate a key with the right peerID prefix but wrong signature.
	_, err := reg.VerifyRequest(context.Background(), peerID+".ts.badsig")
	assert.Error(t, err)
}

func TestIndexerRegistry_Heartbeat_GetError(t *testing.T) {
	st := &mockIndexerStore{records: make(map[string]*store.IndexerRecord), err: fmt.Errorf("db down")}
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	err := reg.Heartbeat(context.Background(), "peer1", HeartbeatRequest{})
	assert.Error(t, err)
}

func TestIndexerRegistry_Heartbeat_UpdateError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, _ = reg.Register(context.Background(), indexerRegRequest(priv, pubHex, peerID))
	st.updateErr = fmt.Errorf("update failed")
	err := reg.Heartbeat(context.Background(), peerID, HeartbeatRequest{CurrentTip: 1})
	assert.Error(t, err)
}

func TestIndexerRegistry_verifyRegistration_InvalidPricing(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	req := indexerRegRequest(priv, pubHex, peerID)
	req.Pricing = "not-json"
	_, err := reg.Register(context.Background(), req)
	assert.Error(t, err)
}

func TestIndexerRegistry_verifyRegistration_MissingChainNetwork(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:8080", Multiaddr: "/ip4/127.0.0.1/tcp/4001",
		Chain: "", Network: "",
	})
	assert.Error(t, err)
}

func TestIndexerRegistry_verifyRegistration_MissingPeerID(t *testing.T) {
	_, pubHex := generateTestKeypair(t)
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: "", DefraPK: pubHex,
		SignedMessages: map[string]string{"aa": "bb"},
		HTTPUrl:        "http://localhost", Multiaddr: "/ip4/127.0.0.1/tcp/4001",
		Chain: "eth", Network: "mainnet",
	})
	assert.Error(t, err)
}

func TestIndexerRegistry_verifyRegistration_MissingUrl(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "", Multiaddr: "",
		Chain: "eth", Network: "mainnet",
	})
	assert.Error(t, err)
}

func TestIndexerRegistry_verifyRegistration_NoSignatures(t *testing.T) {
	_, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewIndexerRegistry(newMockIndexerStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: map[string]string{},
		HTTPUrl:        "http://localhost:8080",
		Multiaddr:      "/ip4/127.0.0.1/tcp/4001",
		Chain:          "eth", Network: "mainnet",
	})
	assert.Error(t, err)
}

// --- HostRegistry additional error-path tests ---

func hostRegRequest(priv *secp256k1.PrivateKey, pubHex, peerID string) RegisterHostRequest {
	return RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:9090",
		Multiaddr:      "/ip4/127.0.0.1/tcp/5001",
		Chain:          "eth", Network: "mainnet",
	}
}

func TestHostRegistry_Register_StoreError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := &mockHostStore{records: make(map[string]*store.HostRecord), createErr: fmt.Errorf("write fail")}
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), hostRegRequest(priv, pubHex, peerID))
	assert.Error(t, err)
}

func TestHostRegistry_Deregister_NotFound(t *testing.T) {
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	err := reg.Deregister(context.Background(), "unknown")
	assert.Error(t, err)
}

func TestHostRegistry_Deregister_GetError(t *testing.T) {
	st := &mockHostStore{records: make(map[string]*store.HostRecord), err: fmt.Errorf("db down")}
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	err := reg.Deregister(context.Background(), "peer1")
	assert.Error(t, err)
}

func TestHostRegistry_Deregister_UpdateError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, _ = reg.Register(context.Background(), hostRegRequest(priv, pubHex, peerID))
	st.updateErr = fmt.Errorf("update failed")
	err := reg.Deregister(context.Background(), peerID)
	assert.Error(t, err)
}

func TestHostRegistry_VerifyRequest_ExtractError(t *testing.T) {
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.VerifyRequest(context.Background(), "nodots")
	assert.Error(t, err)
}

func TestHostRegistry_VerifyRequest_NotFound(t *testing.T) {
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.VerifyRequest(context.Background(), "unknown.ts.sig")
	assert.Error(t, err)
}

func TestHostRegistry_VerifyRequest_StoreError(t *testing.T) {
	st := &mockHostStore{records: make(map[string]*store.HostRecord), err: fmt.Errorf("db down")}
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.VerifyRequest(context.Background(), "peer1.ts.sig")
	assert.Error(t, err)
}

func TestHostRegistry_VerifyRequest_HashMismatch(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, _ = reg.Register(context.Background(), hostRegRequest(priv, pubHex, peerID))
	_, err := reg.VerifyRequest(context.Background(), peerID+".ts.badsig")
	assert.Error(t, err)
}

func TestHostRegistry_Heartbeat_NotFound(t *testing.T) {
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	err := reg.Heartbeat(context.Background(), "unknown")
	assert.Error(t, err)
}

func TestHostRegistry_Heartbeat_GetError(t *testing.T) {
	st := &mockHostStore{records: make(map[string]*store.HostRecord), err: fmt.Errorf("db down")}
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	err := reg.Heartbeat(context.Background(), "peer1")
	assert.Error(t, err)
}

func TestHostRegistry_Heartbeat_UpdateError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, _ = reg.Register(context.Background(), hostRegRequest(priv, pubHex, peerID))
	st.updateErr = fmt.Errorf("update failed")
	err := reg.Heartbeat(context.Background(), peerID)
	assert.Error(t, err)
}

func TestHostRegistry_verifyRegistration_MissingChain(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:9090",
		Multiaddr:      "/ip4/127.0.0.1/tcp/5001",
		// Chain intentionally empty
		Network: "mainnet",
	})
	assert.Error(t, err)
}

func TestHostRegistry_verifyRegistration_MissingPeerID(t *testing.T) {
	_, pubHex := generateTestKeypair(t)
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID: "", DefraPK: pubHex, Chain: "eth", Network: "mainnet",
		SignedMessages: map[string]string{"aa": "bb"},
		HTTPUrl:        "http://localhost", Multiaddr: "/ip4/127.0.0.1/tcp/5001",
	})
	assert.Error(t, err)
}

func TestIndexerRegistry_UpdateReliability_WithStatus(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, _ = reg.Register(context.Background(), indexerRegRequest(priv, pubHex, peerID))
	rec, _ := st.GetByPeerID(context.Background(), peerID)
	err := reg.UpdateReliability(context.Background(), rec.DocID, 0.5, 10, store.StatusInactive)
	require.NoError(t, err)
	updated, _ := st.GetByPeerID(context.Background(), peerID)
	assert.Equal(t, store.StatusInactive, updated.Status)
}

func TestHostRegistry_Register_GetError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := &mockHostStore{records: make(map[string]*store.HostRecord), err: fmt.Errorf("db down")}
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), hostRegRequest(priv, pubHex, peerID))
	assert.Error(t, err)
}

func TestHostRegistry_Register_ReregistrationUpdateError(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	// First registration
	_, err := reg.Register(context.Background(), hostRegRequest(priv, pubHex, peerID))
	require.NoError(t, err)
	// Make update fail on re-registration
	st.updateErr = fmt.Errorf("update failed")
	_, err = reg.Register(context.Background(), hostRegRequest(priv, pubHex, peerID))
	assert.Error(t, err)
}

func TestHostRegistry_Register_BadSignature(t *testing.T) {
	_, pubHex := generateTestKeypair(t)
	otherPriv, _ := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(otherPriv, peerID),
		HTTPUrl:        "http://localhost:9090", Multiaddr: "/ip4/127.0.0.1/tcp/5001",
		Chain: "eth", Network: "mainnet",
	})
	assert.Error(t, err)
}

func TestHostRegistry_verifyRegistration_ChainMismatch(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: signedMessages(priv, peerID),
		HTTPUrl:        "http://localhost:9090", Multiaddr: "/ip4/127.0.0.1/tcp/5001",
		Chain: "polygon", Network: "mainnet",
	})
	assert.Error(t, err)
}

func TestHostRegistry_verifyRegistration_MissingDefraPK(t *testing.T) {
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID: "peer1", DefraPK: "",
		SignedMessages: map[string]string{"aa": "bb"},
		HTTPUrl:        "http://localhost:9090", Multiaddr: "/ip4/127.0.0.1/tcp/5001",
		Chain: "eth", Network: "mainnet",
	})
	assert.Error(t, err)
}

func TestHostRegistry_verifyRegistration_NoSignatures(t *testing.T) {
	_, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]
	reg := NewHostRegistry(newMockHostStore(), newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: map[string]string{},
		HTTPUrl:        "http://localhost:9090",
		Multiaddr:      "/ip4/127.0.0.1/tcp/5001",
		Chain:          "eth", Network: "mainnet",
	})
	assert.Error(t, err)
}

func TestIndexerRegistry_Register_MultipleSignatures_OneInvalid(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]

	validMsgHex := hex.EncodeToString([]byte(peerID))
	validSig := signMsg(priv, []byte(peerID))

	// A second message with an invalid signature.
	bogusMsg := hex.EncodeToString([]byte("other-data"))
	bogusSig := "deadbeef"

	st := newMockIndexerStore()
	reg := NewIndexerRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterIndexerRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: map[string]string{
			validMsgHex: validSig,
			bogusMsg:    bogusSig,
		},
		HTTPUrl:   "http://localhost:8080",
		Multiaddr: "/ip4/127.0.0.1/tcp/5001",
		Chain:     "eth", Network: "mainnet",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "signature invalid")
}

func TestHostRegistry_Register_MultipleSignatures_OneInvalid(t *testing.T) {
	priv, pubHex := generateTestKeypair(t)
	peerID := "Qm" + pubHex[:8]

	validMsgHex := hex.EncodeToString([]byte(peerID))
	validSig := signMsg(priv, []byte(peerID))

	bogusMsg := hex.EncodeToString([]byte("other-data"))
	bogusSig := "deadbeef"

	st := newMockHostStore()
	reg := NewHostRegistry(st, newTestVerifier(), newSugaredLogger(t), "eth", "mainnet", 30)
	_, err := reg.Register(context.Background(), RegisterHostRequest{
		PeerID: peerID, DefraPK: pubHex,
		SignedMessages: map[string]string{
			validMsgHex: validSig,
			bogusMsg:    bogusSig,
		},
		HTTPUrl:   "http://localhost:9090",
		Multiaddr: "/ip4/127.0.0.1/tcp/5001",
		Chain:     "eth", Network: "mainnet",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "signature invalid")
}
