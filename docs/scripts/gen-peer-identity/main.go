// gen-peer-identity generates a secp256k1 key pair and produces the registration
// payload expected by POST /v1/indexers/register and POST /v1/hosts/register.
//
// Usage:
//
//	go run ./docs/scripts/gen-peer-identity
//
// Output is JSON that can be merged with the rest of the registration body:
//
//	{"peer_id":"03...","defra_pk":"03...","private_key":"ab...","signed_messages":{"<msg_hex>":"<sig_hex>"}}
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

type identity struct {
	PeerID         string            `json:"peer_id"`
	DefraPK        string            `json:"defra_pk"`
	PrivateKey     string            `json:"private_key"`
	SignedMessages map[string]string `json:"signed_messages"`
}

func main() {
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate key: %v\n", err)
		os.Exit(1)
	}

	pubKeyBytes := privKey.PubKey().SerializeCompressed()
	peerID := hex.EncodeToString(pubKeyBytes)
	defraPK := peerID

	// Sign the raw peer ID bytes.
	// The scheduler verifies: sha256(decoded(msgHex)) signed by defraPK.
	msgBytes := []byte(peerID)
	msgHex := hex.EncodeToString(msgBytes)
	hash := sha256.Sum256(msgBytes)
	sig := ecdsa.Sign(privKey, hash[:])
	sigHex := hex.EncodeToString(sig.Serialize())

	privKeyHex := hex.EncodeToString(privKey.Serialize())

	out := identity{
		PeerID:     peerID,
		DefraPK:    defraPK,
		PrivateKey: privKeyHex,
		SignedMessages: map[string]string{
			msgHex: sigHex,
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}
}
