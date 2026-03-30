package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestVerifier() *Verifier {
	return NewVerifier()
}

func generateKeypair(t *testing.T) (*secp256k1.PrivateKey, string) {
	t.Helper()
	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	pubHex := hex.EncodeToString(priv.PubKey().SerializeCompressed())
	return priv, pubHex
}

func signMsg(priv *secp256k1.PrivateKey, msgBytes []byte) string {
	hash := sha256.Sum256(msgBytes)
	sig := ecdsa.Sign(priv, hash[:])
	return hex.EncodeToString(sig.Serialize())
}

func TestExtractPeerID(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		token := "myPeer.1234567890.deadsig"
		id, err := ExtractPeerID(token)
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

func TestVerifyRequestToken(t *testing.T) {
	v := newTestVerifier()
	priv, pubHex := generateKeypair(t)
	peerID := "QmTestPeer"

	t.Run("valid", func(t *testing.T) {
		token := GenerateToken(priv, peerID)
		got, err := v.VerifyRequestToken(pubHex, token)
		require.NoError(t, err)
		assert.Equal(t, peerID, got)
	})

	t.Run("expired timestamp", func(t *testing.T) {
		ts := strconv.FormatInt(time.Now().Unix()-120, 10)
		msg := peerID + "." + ts
		hash := sha256.Sum256([]byte(msg))
		sig := ecdsa.Sign(priv, hash[:])
		token := peerID + "." + ts + "." + hex.EncodeToString(sig.Serialize())
		_, err := v.VerifyRequestToken(pubHex, token)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expired")
	})

	t.Run("future timestamp", func(t *testing.T) {
		ts := strconv.FormatInt(time.Now().Unix()+120, 10)
		msg := peerID + "." + ts
		hash := sha256.Sum256([]byte(msg))
		sig := ecdsa.Sign(priv, hash[:])
		token := peerID + "." + ts + "." + hex.EncodeToString(sig.Serialize())
		_, err := v.VerifyRequestToken(pubHex, token)
		assert.Error(t, err)
	})

	t.Run("wrong signature", func(t *testing.T) {
		otherPriv, _ := generateKeypair(t)
		token := GenerateToken(otherPriv, peerID)
		_, err := v.VerifyRequestToken(pubHex, token)
		assert.Error(t, err)
	})

	t.Run("malformed token", func(t *testing.T) {
		_, err := v.VerifyRequestToken(pubHex, "notavalidtoken")
		assert.Error(t, err)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		_, err := v.VerifyRequestToken(pubHex, "peer.notanumber.deadsig")
		assert.Error(t, err)
	})

	t.Run("bad pubkey hex", func(t *testing.T) {
		token := GenerateToken(priv, peerID)
		_, err := v.VerifyRequestToken("not-hex", token)
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
	err := v.VerifySignature(pubHex, msgHex, hex.EncodeToString([]byte("not-a-der-sig")))
	assert.Error(t, err)
}
