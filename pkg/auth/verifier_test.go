package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestVerifier() *Verifier {
	return NewVerifier("test-hmac-secret-32-bytes-long!!")
}

// generateKeypair creates a fresh secp256k1 keypair for use in tests.
func generateKeypair(t *testing.T) (*secp256k1.PrivateKey, string) {
	t.Helper()
	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	pubHex := hex.EncodeToString(priv.PubKey().SerializeCompressed())
	return priv, pubHex
}

// signMsg returns a DER-encoded signature over SHA256(msgBytes) as a hex string.
func signMsg(priv *secp256k1.PrivateKey, msgBytes []byte) string {
	hash := sha256.Sum256(msgBytes)
	sig := ecdsa.Sign(priv, hash[:])
	return hex.EncodeToString(sig.Serialize())
}

func TestIssueAPIKey(t *testing.T) {
	v := newTestVerifier()

	t.Run("format", func(t *testing.T) {
		key, hash, err := v.IssueAPIKey("peer1")
		require.NoError(t, err)
		parts := strings.SplitN(key, ".", 3)
		assert.Len(t, parts, 3, "key must have peerID.timestamp.random structure")
		assert.Equal(t, "peer1", parts[0])
		assert.NotEmpty(t, hash)
	})

	t.Run("uniqueness", func(t *testing.T) {
		k1, h1, _ := v.IssueAPIKey("peer1")
		k2, h2, _ := v.IssueAPIKey("peer1")
		assert.NotEqual(t, k1, k2)
		assert.NotEqual(t, h1, h2)
	})
}

func TestVerifyAPIKey(t *testing.T) {
	v := newTestVerifier()
	plain, hash, err := v.IssueAPIKey("peer1")
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		assert.NoError(t, v.VerifyAPIKey(plain, hash))
	})

	t.Run("wrong key", func(t *testing.T) {
		assert.Error(t, v.VerifyAPIKey("wrong.key.value", hash))
	})

	t.Run("tampered", func(t *testing.T) {
		tampered := plain + "x"
		assert.Error(t, v.VerifyAPIKey(tampered, hash))
	})

	t.Run("different secret", func(t *testing.T) {
		other := NewVerifier("other-secret")
		assert.Error(t, other.VerifyAPIKey(plain, hash))
	})
}

func TestExtractPeerID(t *testing.T) {
	v := newTestVerifier()

	t.Run("happy path", func(t *testing.T) {
		plain, _, err := v.IssueAPIKey("myPeer")
		require.NoError(t, err)
		id, err := ExtractPeerID(plain)
		require.NoError(t, err)
		assert.Equal(t, "myPeer", id)
	})

	t.Run("no dots", func(t *testing.T) {
		_, err := ExtractPeerID("nodots")
		assert.Error(t, err)
	})

	t.Run("one dot", func(t *testing.T) {
		_, err := ExtractPeerID("only.one")
		assert.Error(t, err)
	})
}

func TestVerifySignature(t *testing.T) {
	v := newTestVerifier()
	priv, pubHex := generateKeypair(t)
	msg := []byte("hello shinzo")
	msgHex := hex.EncodeToString(msg)
	sigHex := signMsg(priv, msg)

	t.Run("valid", func(t *testing.T) {
		assert.NoError(t, v.VerifySignature(pubHex, msgHex, sigHex))
	})

	t.Run("wrong message", func(t *testing.T) {
		wrongMsg := hex.EncodeToString([]byte("different"))
		assert.Error(t, v.VerifySignature(pubHex, wrongMsg, sigHex))
	})

	t.Run("wrong key", func(t *testing.T) {
		_, otherPub := generateKeypair(t)
		assert.Error(t, v.VerifySignature(otherPub, msgHex, sigHex))
	})

	t.Run("bad pubkey hex", func(t *testing.T) {
		err := v.VerifySignature("not-hex", msgHex, sigHex)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid public key hex")
	})

	t.Run("bad msg hex", func(t *testing.T) {
		err := v.VerifySignature(pubHex, "not-hex", sigHex)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid message hex")
	})

	t.Run("bad sig hex", func(t *testing.T) {
		err := v.VerifySignature(pubHex, msgHex, "not-hex")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid signature hex")
	})
}

func TestVerifySignature_InvalidPubKeyBytes(t *testing.T) {
	v := newTestVerifier()
	// Valid hex but not a valid secp256k1 pubkey (all zeros).
	msg := []byte("hello")
	msgHex := hex.EncodeToString(msg)
	sigHex := signMsg(func() *secp256k1.PrivateKey {
		priv, _ := secp256k1.GeneratePrivateKey()
		return priv
	}(), msg)
	err := v.VerifySignature("0000000000000000000000000000000000000000000000000000000000000000", msgHex, sigHex)
	assert.Error(t, err)
}

func TestVerifySignature_InvalidDERSignature(t *testing.T) {
	v := newTestVerifier()
	_, pubHex := generateKeypair(t)
	msg := []byte("hello")
	msgHex := hex.EncodeToString(msg)
	// Valid hex but not valid DER signature.
	err := v.VerifySignature(pubHex, msgHex, hex.EncodeToString([]byte("not-a-der-sig")))
	assert.Error(t, err)
}
