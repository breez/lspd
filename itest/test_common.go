package itest

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}

func AssertChannelCapacity(
	t *testing.T,
	outerAmountMsat uint64,
	capacityMsat uint64,
) {
	assert.Equal(t, ((outerAmountMsat/1000)+100000)*1000, capacityMsat)
}

func calculateInnerAmountMsat(lsp LspNode, outerAmountMsat uint64) (uint64, uint64) {
	fee := outerAmountMsat * 40 / 10_000 / 1_000 * 1_000
	if fee < 2000000 {
		fee = 2000000
	}

	inner := outerAmountMsat - fee

	// NOTE: If the LSP does not support charging fees (the CLN version doesn't)
	// We have to pretend in the registerpayment call that the LSP WILL charge
	// fees. If we update the CLN lsp to charge fees, this check can be removed.
	if lsp.SupportsChargingFees() {
		return inner, inner
	} else {
		return outerAmountMsat, inner
	}
}

var publicChanAmount uint64 = 1000183
