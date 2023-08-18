package lsps2

import (
	"context"
	"errors"

	"github.com/breez/lspd/basetypes"
	"github.com/breez/lspd/shared"
)

type RegisterBuy struct {
	LspId            string
	PeerId           string
	Scid             basetypes.ShortChannelID
	OpeningFeeParams shared.OpeningFeeParams
	PaymentSizeMsat  *uint64
	Mode             OpeningMode
}

var ErrScidExists = errors.New("scid exists")

type Lsps2Store interface {
	RegisterBuy(ctx context.Context, req *RegisterBuy) error
}
