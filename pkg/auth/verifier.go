package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

type Verifier struct{}

func NewVerifier() *Verifier {
	return &Verifier{}
}

// VerifySignature checks that sig (hex-encoded DER) was produced by the private key
// corresponding to pubKeyHex (compressed secp256k1, hex-encoded) over msgHash (raw bytes).
func (v *Verifier) VerifySignature(pubKeyHex, msgHex, sigHex string) error {
	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return fmt.Errorf("invalid public key hex: %w", err)
	}
	pubKey, err := secp256k1.ParsePubKey(pubBytes)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	msgBytes, err := hex.DecodeString(msgHex)
	if err != nil {
		return fmt.Errorf("invalid message hex: %w", err)
	}

	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}
	sig, err := ecdsa.ParseDERSignature(sigBytes)
	if err != nil {
		return fmt.Errorf("parse signature: %w", err)
	}

	hash := sha256.Sum256(msgBytes)
	if !sig.Verify(hash[:], pubKey) {
		return errors.New("signature verification failed")
	}
	return nil
}

// VerifyRequestToken validates a per-request Bearer token of the form
// "<peerID>.<unix_ts>.<sig_hex>" where sig is a secp256k1 DER signature over
// SHA256(peerID + "." + unix_ts). Returns the peerID on success.
// Rejects tokens whose timestamp is outside a ±60-second window.
func (v *Verifier) VerifyRequestToken(pubKeyHex, token string) (string, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return "", errors.New("malformed token: expected peerID.timestamp.sig")
	}
	peerID := parts[0]
	tsStr := parts[1]
	sigHex := parts[2]

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp: %w", err)
	}

	diff := time.Now().Unix() - ts
	if diff < -60 || diff > 60 {
		return "", errors.New("token expired or timestamp out of window")
	}

	msg := peerID + "." + tsStr
	hash := sha256.Sum256([]byte(msg))

	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid public key hex: %w", err)
	}
	pubKey, err := secp256k1.ParsePubKey(pubBytes)
	if err != nil {
		return "", fmt.Errorf("parse public key: %w", err)
	}

	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return "", fmt.Errorf("invalid signature hex: %w", err)
	}
	sig, err := ecdsa.ParseDERSignature(sigBytes)
	if err != nil {
		return "", fmt.Errorf("parse signature: %w", err)
	}

	if !sig.Verify(hash[:], pubKey) {
		return "", errors.New("signature verification failed")
	}
	return peerID, nil
}

// ExtractPeerID returns the peerID component from a token (first segment before ".").
func ExtractPeerID(token string) (string, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return "", errors.New("malformed token")
	}
	return parts[0], nil
}

// GenerateToken constructs a per-request auth token for testing and client use.
func GenerateToken(priv *secp256k1.PrivateKey, peerID string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := peerID + "." + ts
	hash := sha256.Sum256([]byte(msg))
	sig := ecdsa.Sign(priv, hash[:])
	return peerID + "." + ts + "." + hex.EncodeToString(sig.Serialize())
}
