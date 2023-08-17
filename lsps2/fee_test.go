package lsps2

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_FeeVariant(t *testing.T) {
	zero := uint64(0)
	tests := []struct {
		paymentSizeMsat uint64
		proportional    uint32
		minFeeMsat      uint64
		err             error
		expected        uint64
	}{
		{proportional: 0, paymentSizeMsat: 0, minFeeMsat: 0, expected: 0},
		{proportional: 0, paymentSizeMsat: 0, minFeeMsat: 1, expected: 1},
		{proportional: 1, paymentSizeMsat: 1, minFeeMsat: 0, expected: 1},
		{proportional: 1000, paymentSizeMsat: 10_000_000, minFeeMsat: 9999, expected: 10000},
		{proportional: 1000, paymentSizeMsat: 10_000_000, minFeeMsat: 10000, expected: 10000},
		{proportional: 1000, paymentSizeMsat: 10_000_000, minFeeMsat: 10001, expected: 10001},
		{proportional: 1, paymentSizeMsat: zero - 999_999, minFeeMsat: 0, err: ErrOverflow},
		{proportional: 1, paymentSizeMsat: zero - 1_000_000, minFeeMsat: 0, expected: 18446744073709},
		{proportional: 2, paymentSizeMsat: zero - 1_000_000, minFeeMsat: 0, err: ErrOverflow},
	}

	for _, tst := range tests {
		t.Run(
			fmt.Sprintf("ps%d_prop%d_min%d", tst.paymentSizeMsat, tst.proportional, tst.minFeeMsat),
			func(t *testing.T) {
				res, err := computeOpeningFee(tst.paymentSizeMsat, tst.proportional, tst.minFeeMsat)
				assert.Equal(t, tst.expected, res)
				assert.Equal(t, tst.err, err)
			},
		)

	}
}
