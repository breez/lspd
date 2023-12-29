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

type ExternalTokenForward struct {
	Token          string
	NodeId         []byte
	ExternalNodeId []byte
	ResolvedTime   time.Time
	Direction      string
	// The amount forwarded to/from the lsp
	AmountMsat uint64
}

type Forward struct {
	Identifier   string
	InChannel    lightning.ShortChannelID
	OutChannel   lightning.ShortChannelID
	InMsat       uint64
	OutMsat      uint64
	ResolvedTime time.Time
}

type Store interface {
	UpdateChannels(ctx context.Context, updates []*ChannelUpdate) error
	InsertForwards(ctx context.Context, forwards []*Forward, nodeId []byte) error
	UpdateForwards(ctx context.Context, forwards []*Forward, nodeId []byte) error
	FetchClnForwardOffsets(ctx context.Context, nodeId []byte) (uint64, uint64, error)
	SetClnForwardOffsets(ctx context.Context, nodeId []byte, created uint64, updated uint64) error
	FetchLndForwardOffset(ctx context.Context, nodeId []byte) (*time.Time, error)
	MatchForwardsAndChannels(ctx context.Context) error
	ExportTokenForwardsForExternalNode(ctx context.Context, start time.Time, end time.Time, node []byte, externalNode []byte) ([]*ExternalTokenForward, error)
	ImportTokenForwards(ctx context.Context, forwards []*ExternalTokenForward) error
	MatchInternalForwards(ctx context.Context, start time.Time, end time.Time) error
	MatchExternalForwards(ctx context.Context, start time.Time, end time.Time) error
	GetFirstAndLastMatchedForwardTimes(ctx context.Context, internal bool) (*time.Time, *time.Time, error)
	GetFirstForwardTime(ctx context.Context) (*time.Time, error)
	GetForwardsWithoutChannelCount(ctx context.Context) (int64, error)
}
