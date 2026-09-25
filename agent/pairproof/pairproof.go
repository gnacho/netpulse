// Package pairproof is the proof a NetPulse server gives, at pairing, that
// the key it serves is the real server's: an HMAC keyed with the pairing
// token, which only the server and the admin who copied it know. The agent
// and the server both compute it here, so the two cannot drift apart.
//
// FORK: see runtime.ProveServerKey for the exchange.
package pairproof

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Label separates this MAC from any other use of the pairing token.
const Label = "netpulse-pair-v1"

// MAC is HMAC-SHA256 keyed with the pairing token over the label, the
// agent's nonce and the server's key fingerprint, in hex.
func MAC(pairingToken, nonce, serverFP string) string {
	m := hmac.New(sha256.New, []byte(pairingToken))
	fmt.Fprintf(m, "%s\n%s\n%s", Label, nonce, serverFP)
	return hex.EncodeToString(m.Sum(nil))
}
