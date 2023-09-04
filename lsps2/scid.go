package lsps2

import (
	"crypto/rand"
	"math/big"

	"github.com/breez/lspd/lightning"
)

var one = big.NewInt(1)
var two = big.NewInt(2)
var sixtyfour = big.NewInt(64)
var twoPowSixtyfour = two.Exp(two, sixtyfour, nil)
var maxUint64 = twoPowSixtyfour.Sub(twoPowSixtyfour, one)

func newScid() (*lightning.ShortChannelID, error) {
	s, err := rand.Int(rand.Reader, maxUint64)
	if err != nil {
		return nil, err
	}

	scid := lightning.ShortChannelID(s.Uint64())
	return &scid, nil
}
