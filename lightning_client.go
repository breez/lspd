package main

import (
	"github.com/breez/lspd/basetypes"
	"github.com/btcsuite/btcd/wire"
)

type GetInfoResult struct {
	Alias  string
	Pubkey string
}

type GetChannelResult struct {
	InitialChannelID   basetypes.ShortChannelID
	ConfirmedChannelID basetypes.ShortChannelID
}

type OpenChannelRequest struct {
	Destination []byte
	CapacitySat uint64
	MinHtlcMsat uint64
	TargetConf  uint32
	IsPrivate   bool
	IsZeroConf  bool
}

type LightningClient interface {
	GetInfo() (*GetInfoResult, error)
	IsConnected(destination []byte) (bool, error)
	OpenChannel(req *OpenChannelRequest) (*wire.OutPoint, error)
	GetChannel(peerID []byte, channelPoint wire.OutPoint) (*GetChannelResult, error)
	GetNodeChannelCount(nodeID []byte) (int, error)
	GetClosedChannels(nodeID string, channelPoints map[string]uint64) (map[string]uint64, error)
}
