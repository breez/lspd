package history

import (
	"context"
	"time"

	"github.com/breez/lspd/lightning"
	"github.com/btcsuite/btcd/wire"
)

type ChannelUpdate struct {
	NodeID        []byte
	PeerId        []byte
	AliasScid     *lightning.ShortChannelID
	ConfirmedScid *lightning.ShortChannelID
	ChannelPoint  *wire.OutPoint
	LastUpdate    time.Time
}

type Forward struct {
	Identifier   string
	InChannel    lightning.ShortChannelID
	OutChannel   lightning.ShortChannelID
	InMsat       uint64
	OutMsat      uint64
	ResolvedTime time.Time
}

type OpenChannelHtlc struct {
	NodeId             []byte
	PeerId             []byte
	ChannelPoint       *wire.OutPoint
	OriginalAmountMsat uint64
	ForwardAmountMsat  uint64
	IncomingAmountMsat uint64
	ForwardTime        time.Time
}

type Store interface {
	UpdateChannels(ctx context.Context, updates []*ChannelUpdate) error
	InsertForwards(ctx context.Context, forwards []*Forward, nodeId []byte) error
	UpdateForwards(ctx context.Context, forwards []*Forward, nodeId []byte) error
	FetchClnForwardOffsets(ctx context.Context, nodeId []byte) (uint64, uint64, error)
	SetClnForwardOffsets(ctx context.Context, nodeId []byte, created uint64, updated uint64) error
	FetchLndForwardOffset(ctx context.Context, nodeId []byte) (*time.Time, error)
	AddOpenChannelHtlc(ctx context.Context, htlc *OpenChannelHtlc) error
}
