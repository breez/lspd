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
	InitialChannelID   ShortChannelID
	ConfirmedChannelID ShortChannelID
	HtlcMinimumMsat    uint64
}

type OpenChannelRequest struct {
	Destination    []byte
	CapacitySat    uint64
	MinHtlcMsat    uint64
	IsPrivate      bool
	IsZeroConf     bool
	MinConfs       *uint32
	FeeSatPerVByte *float64
	TargetConf     *uint32
}

type Client interface {
	GetInfo() (*GetInfoResult, error)
	IsConnected(destination []byte) (bool, error)
	OpenChannel(req *OpenChannelRequest) (*wire.OutPoint, error)
	GetChannel(peerID []byte, channelPoint wire.OutPoint) (*GetChannelResult, error)
	GetPeerId(scid *ShortChannelID) ([]byte, error)
	GetNodeChannelCount(nodeID []byte) (int, error)
	GetClosedChannels(nodeID string, channelPoints map[string]uint64) (map[string]uint64, error)
	WaitOnline(peerID []byte, deadline time.Time) error
	WaitChannelActive(peerID []byte, deadline time.Time) error
}
