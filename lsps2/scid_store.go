package lsps2

import (
	"context"
	"fmt"
	"time"

	"github.com/breez/lspd/basetypes"
)

var ErrScidExists = fmt.Errorf("scid already exists")

type ScidStore interface {
	AddScid(
		ctx context.Context,
		lspId []byte,
		scid basetypes.ShortChannelID,
		expiry time.Time,
	) error
	RemoveScid(
		ctx context.Context,
		lspId []byte,
		scid basetypes.ShortChannelID,
	) (bool, error)
	RemoveExpired(ctx context.Context, before time.Time) error
}
