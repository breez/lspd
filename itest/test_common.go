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

func calculateInnerAmountMsat(lsp LspNode, outerAmountMsat uint64) uint64 {
	if lsp.SupportsChargingFees() {
		fee := outerAmountMsat * 40 / 10_000 / 1_000 * 1_000
		if fee < 2000000 {
			fee = 2000000
		}

		return outerAmountMsat - fee
	} else {
		return outerAmountMsat
	}
}

var publicChanAmount uint64 = 1000183
