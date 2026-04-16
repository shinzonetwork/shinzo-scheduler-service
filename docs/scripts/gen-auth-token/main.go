// gen-auth-token generates a per-request Bearer token for the scheduler API.
//
// Usage:
//
//	go run ./docs/scripts/gen-auth-token --private-key <hex> --peer-id <id>
//
// Output is the token string: peerID.unixTs.sigHex
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func main() {
	privKeyHex := flag.String("private-key", "", "hex-encoded secp256k1 private key")
	peerID := flag.String("peer-id", "", "peer ID (typically the hex-encoded compressed public key)")
	flag.Parse()

	if *privKeyHex == "" || *peerID == "" {
		fmt.Fprintln(os.Stderr, "usage: gen-auth-token --private-key <hex> --peer-id <id>")
		os.Exit(1)
	}

	privBytes, err := hex.DecodeString(*privKeyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid private key hex: %v\n", err)
		os.Exit(1)
	}

	privKey := secp256k1.PrivKeyFromBytes(privBytes)

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := *peerID + "." + ts
	hash := sha256.Sum256([]byte(msg))
	sig := ecdsa.Sign(privKey, hash[:])

	fmt.Print(*peerID + "." + ts + "." + hex.EncodeToString(sig.Serialize()))
}
