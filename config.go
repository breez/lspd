package main

type NodeConfig struct {
	LspdPrivateKey            string     `json:"lspdPrivateKey"`
	Token                     string     `json:"token"`
	Host                      string     `json:"host"`
	PublicChannelAmount       int64      `json:"publicChannelAmount,string"`
	ChannelAmount             uint64     `json:"channelAmount,string"`
	ChannelPrivate            bool       `json:"channelPrivate"`
	TargetConf                uint32     `json:"targetConf,string"`
	MinHtlcMsat               uint64     `json:"minHtlcMsat,string"`
	BaseFeeMsat               uint64     `json:"baseFeeMsat,string"`
	FeeRate                   float64    `json:"feeRate,string"`
	TimeLockDelta             uint32     `json:"timeLockDelta,string"`
	ChannelFeePermyriad       int64      `json:"channelFeePermyriad,string"`
	ChannelMinimumFeeMsat     int64      `json:"channelMinimumFeeMsat,string"`
	AdditionalChannelCapacity int64      `json:"additionalChannelCapacity,string"`
	MaxInactiveDuration       uint64     `json:"maxInactiveDuration,string"`
	Lnd                       *LndConfig `json:"lnd,omitempty"`
	Cln                       *ClnConfig `json:"cln,omitempty"`
}

type LndConfig struct {
	Address  string `json:"address"`
	Cert     string `json:"cert"`
	Macaroon string `json:"macaroon"`
}

type ClnConfig struct {
	PluginAddress string `json:"pluginAddress"`
	SocketPath    string `json:"socketPath"`
}
