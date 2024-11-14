package lightning

import (
	"time"

	"github.com/btcsuite/btcd/wire"
)

type GetInfoResult struct {
	Alias  string
	Pubkey string
}

type GetChannelResult struct {
	AliasScid       *ShortChannelID
	ConfirmedScid   *ShortChannelID
	HtlcMinimumMsat uint64
}

type RoutingPolicy struct {
	Ppm      uint32
	BaseMsat uint64
}

type OpenChannelRequest struct {
	Destination    []byte
	CapacitySat    uint64
	MinConfs       *uint32
	FeeSatPerVByte *float64
	TargetConf     *uint32
	RoutingPolicy  *RoutingPolicy
}

type Channel struct {
	AliasScid     *ShortChannelID
	ConfirmedScid *ShortChannelID
	ChannelPoint  *wire.OutPoint
	PeerId        []byte
}

type Client interface {
	GetInfo() (*GetInfoResult, error)
	IsConnected(destination []byte) (bool, error)
	OpenChannel(req *OpenChannelRequest) (*wire.OutPoint, error)
	GetChannel(peerID []byte, channelPoint wire.OutPoint) (*GetChannelResult, error)
	GetPeerId(scid *ShortChannelID) ([]byte, error)
	GetClosedChannels(nodeID string, channelPoints map[string]uint64) (map[string]uint64, error)
	WaitOnline(peerID []byte, deadline time.Time) error
	WaitChannelActive(peerID []byte, deadline time.Time) error
	ListChannels() ([]*Channel, error)
}
