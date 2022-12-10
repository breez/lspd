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
