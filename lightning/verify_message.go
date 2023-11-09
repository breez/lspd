package lightning

import (
	"crypto/sha256"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/tv42/zbase32"
)

var ErrInvalidSignature = fmt.Errorf("invalid signature")
var SignedMsgPrefix = []byte("Lightning Signed Message:")

func VerifyMessage(message []byte, signature string) (*btcec.PublicKey, error) {
	// The signature should be zbase32 encoded
	sig, err := zbase32.DecodeString(signature)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature: %v", err)
	}

	msg := append(SignedMsgPrefix, message...)
	first := sha256.Sum256(msg)
	second := sha256.Sum256(first[:])
	pubkey, wasCompressed, err := ecdsa.RecoverCompact(
		sig,
		second[:],
	)
	if err != nil {
		return nil, ErrInvalidSignature
	}

	if !wasCompressed {
		return nil, ErrInvalidSignature
	}

	return pubkey, nil
}
