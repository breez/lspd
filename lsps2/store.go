package lsps2

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/shared"
	"github.com/btcsuite/btcd/wire"
)

type SavePromises struct {
	Menu  []*shared.OpeningFeeParams
	Token string
}

type RegisterBuy struct {
	LspId            string
	PeerId           string
	Scid             lightning.ShortChannelID
	OpeningFeeParams shared.OpeningFeeParams
	PaymentSizeMsat  *uint64
	Mode             OpeningMode
}

type BuyRegistration struct {
	Id               uint64
	LspId            string
	PeerId           string // TODO: Make peerId in the registration a byte array.
	Token            string
	Scid             lightning.ShortChannelID
	OpeningFeeParams shared.OpeningFeeParams
	PaymentSizeMsat  *uint64
	Mode             OpeningMode
	ChannelPoint     *wire.OutPoint
	IsComplete       bool
}

func (b *BuyRegistration) IsExpired() bool {
	t, err := time.Parse(lsps0.TIME_FORMAT, b.OpeningFeeParams.ValidUntil)
	if err != nil {
		log.Printf("BuyRegistration.IsExpired(): time.Parse(%v, %v) error: %v", lsps0.TIME_FORMAT, b.OpeningFeeParams.ValidUntil, err)
		return true
	}

	if time.Now().UTC().After(t) {
		return true
	}

	return false
}

type ChannelOpened struct {
	RegistrationId  uint64
	Outpoint        *wire.OutPoint
	FeeMsat         uint64
	PaymentSizeMsat uint64
}

var ErrScidExists = errors.New("scid exists")
var ErrNotFound = errors.New("not found")

type Lsps2Store interface {
	SavePromises(ctx context.Context, req *SavePromises) error
	RegisterBuy(ctx context.Context, req *RegisterBuy) error
	GetBuyRegistration(ctx context.Context, scid lightning.ShortChannelID) (*BuyRegistration, error)
	SetChannelOpened(ctx context.Context, channelOpened *ChannelOpened) error
	SetCompleted(ctx context.Context, registrationId uint64) error
}
