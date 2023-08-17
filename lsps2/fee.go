package lsps2

import "fmt"

var ErrOverflow = fmt.Errorf("integer overflow detected")

func computeOpeningFee(paymentSizeMsat uint64, proportional uint32, minFeeMsat uint64) (uint64, error) {
	tmp, err := safeMultiply(paymentSizeMsat, uint64(proportional))
	if err != nil {
		return 0, err
	}

	tmp, err = safeAdd(tmp, 999_999)
	if err != nil {
		return 0, err
	}

	openingFee := tmp / 1_000_000
	if openingFee < minFeeMsat {
		openingFee = minFeeMsat
	}

	return openingFee, nil
}

func safeAdd(a uint64, b uint64) (uint64, error) {
	sum := a + b
	if sum < a {
		return 0, ErrOverflow
	}

	return sum, nil
}

func safeMultiply(a uint64, b uint64) (uint64, error) {
	prod := a * b
	if a != 0 && ((prod / a) != b) {
		return 0, ErrOverflow
	}

	return prod, nil
}
