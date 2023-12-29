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

type Store interface {
	UpdateChannels(ctx context.Context, updates []*ChannelUpdate) error
}
