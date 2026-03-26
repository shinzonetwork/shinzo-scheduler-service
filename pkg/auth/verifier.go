package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// Verifier handles secp256k1 signature verification and HMAC API key management.
type Verifier struct {
	hmacSecret []byte
}

func NewVerifier(hmacSecret string) *Verifier {
	return &Verifier{hmacSecret: []byte(hmacSecret)}
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

// IssueAPIKey generates a random API key and returns (plaintext, hmacHash).
// Store the hash in DefraDB; return the plaintext to the caller once.
func (v *Verifier) IssueAPIKey(peerID string) (plaintext, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate api key: %w", err)
	}
	// Format: peerID.timestamp.random — readable but opaque enough.
	ts := time.Now().UTC().Format("20060102T150405Z")
	plaintext = fmt.Sprintf("%s.%s.%s", peerID, ts, hex.EncodeToString(buf))
	hash = v.hashAPIKey(plaintext)
	return plaintext, hash, nil
}

// VerifyAPIKey checks the presented key against the stored HMAC hash.
func (v *Verifier) VerifyAPIKey(plaintext, storedHash string) error {
	expected := v.hashAPIKey(plaintext)
	if !hmac.Equal([]byte(expected), []byte(storedHash)) {
		return errors.New("invalid API key")
	}
	return nil
}

// ExtractPeerID parses a key issued by IssueAPIKey and returns the embedded peer ID.
func ExtractPeerID(apiKey string) (string, error) {
	parts := strings.SplitN(apiKey, ".", 3)
	if len(parts) != 3 {
		return "", errors.New("malformed API key")
	}
	return parts[0], nil
}

func (v *Verifier) hashAPIKey(plaintext string) string {
	mac := hmac.New(sha256.New, v.hmacSecret)
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}
