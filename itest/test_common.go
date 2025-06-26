package itest

import (
	"crypto/rand"
	"log"
	"testing"

	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

var WorkingToken = "hello"

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

func calculateInnerAmountMsat(lsp LspNode, outerAmountMsat uint64, params *lspd.OpeningFeeParams) uint64 {
	var fee uint64
	log.Printf("%+v", params)
	if params == nil {
		fee = outerAmountMsat * 40 / 10_000 / 1_000 * 1_000
		if fee < 2000000 {
			fee = 2000000
		}
	} else {
		fee = outerAmountMsat * uint64(params.Proportional) / 1_000_000 / 1_000 * 1_000
		if fee < params.MinMsat {
			fee = params.MinMsat
		}
	}

	if fee > outerAmountMsat {
		lsp.Harness().Fatalf("Fee is higher than amount")
	}

	log.Printf("outer: %v, fee: %v", outerAmountMsat, fee)
	return outerAmountMsat - fee
}

var publicChanAmount uint64 = 1000183
