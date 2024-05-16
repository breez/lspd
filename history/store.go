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

type RevenueForward struct {
	Identifier      string
	Nodeid          []byte
	PeeridIn        []byte
	PeeridOut       []byte
	AmtMsatIn       uint64
	AmtMsatOut      uint64
	ResolvedTime    uint64
	ChannelPointIn  wire.OutPoint
	ChannelPointOut wire.OutPoint
	SendToken       *string
	ReceiveToken    *string
	OpenChannelHtlc *OpenChannelHtlc
}

type ExportedForward struct {
	Token          string
	NodeId         []byte
	ExternalNodeId []byte
	ResolvedTime   time.Time
	// Direction is 'send' if the client associated to the token sent a payment.
	// Direction is 'receive' if the client associated to the token sent a payment.
	Direction string
	// The amount forwarded to/from the external node
	AmountMsat uint64
}

type Store interface {
	RuntimeStore
	DataStore
}

type RuntimeStore interface {
	UpdateChannels(ctx context.Context, updates []*ChannelUpdate) error
	InsertForwards(ctx context.Context, forwards []*Forward, nodeId []byte) error
	UpdateForwards(ctx context.Context, forwards []*Forward, nodeId []byte) error
	FetchClnForwardOffsets(ctx context.Context, nodeId []byte) (uint64, uint64, error)
	SetClnForwardOffsets(ctx context.Context, nodeId []byte, created uint64, updated uint64) error
	FetchLndForwardOffset(ctx context.Context, nodeId []byte) (*time.Time, error)
	AddOpenChannelHtlc(ctx context.Context, htlc *OpenChannelHtlc) error
}

type DataStore interface {
	ExportTokenForwardsForExternalNode(
		ctx context.Context,
		startNs uint64,
		endNs uint64,
		node []byte,
		externalNode []byte,
	) ([]*ExportedForward, error)

	GetOpenChannelHtlcs(
		ctx context.Context,
		startNs uint64,
		endNs uint64,
	) ([]*OpenChannelHtlc, error)

	// Gets all settled forwards in the defined time range. Ordered by nodeid, peerid_in, amt_msat_in, resolved_time
	GetForwards(
		ctx context.Context,
		startNs uint64,
		endNs uint64,
	) ([]*RevenueForward, error)
}
