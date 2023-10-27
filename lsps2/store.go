package lsps2

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/GoWebProd/uuid7"
	"github.com/breez/lspd/common"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0"
	"github.com/btcsuite/btcd/wire"
)

type SavePromises struct {
	Menu  []*common.OpeningFeeParams
	Token string
}

type RegisterBuy struct {
	LspId            string
	PeerId           string
	Scid             lightning.ShortChannelID
	OpeningFeeParams common.OpeningFeeParams
	PaymentSizeMsat  *uint64
	Mode             OpeningMode
}

type BuyRegistration struct {
	Id               uuid7.UUID
	LspId            string
	PeerId           string // TODO: Make peerId in the registration a byte array.
	Token            string
	Scid             lightning.ShortChannelID
	OpeningFeeParams common.OpeningFeeParams
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
	RegistrationId  uuid7.UUID
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
	SetCompleted(ctx context.Context, registrationId uuid7.UUID) error
	RemoveUnusedExpired(ctx context.Context, before time.Time) error
}
