package config

type NodeConfig struct {
	// Name of the LSP. If empty, the node's alias will be taken instead.
	Name string `json:name,omitempty`

	// The public key of the lightning node.
	NodePubkey string `json:nodePubkey,omitempty`

	// Hex encoded private key of the LSP. This is used to decrypt traffic from
	// clients.
	LspdPrivateKey string `json:"lspdPrivateKey"`

	// Tokens used to authenticate to lspd. These tokens must be unique for each
	// configured node, so it's obvious which node an rpc call is meant for.
	Tokens []string `json:"tokens"`

	// The network location of the lightning node, e.g. `12.34.56.78:9012` or
	// `localhost:10011`
	Host string `json:"host"`

	// Public channel amount is a reserved amount for public channels. If a
	// zero conf channel is opened, it will never have this exact amount.
	PublicChannelAmount int64 `json:"publicChannelAmount,string"`

	// The capacity of opened channels through the OpenChannel rpc.
	ChannelAmount uint64 `json:"channelAmount,string"`

	// Value indicating whether channels opened through the OpenChannel rpc
	// should be private.
	ChannelPrivate bool `json:"channelPrivate"`

	// Number of blocks after which an opened channel is considered confirmed.
	TargetConf uint32 `json:"targetConf,string"`

	// Minimum number of confirmations inputs for zero conf channel opens should
	// have.
	MinConfs *uint32 `json:"minConfs,string"`

	// Smallest htlc amount routed over channels opened with the OpenChannel
	// rpc call.
	MinHtlcMsat uint64 `json:"minHtlcMsat,string"`

	// The base fee for routing payments over the channel. It is configured on
	// the node itself, but this value is returned in the ChannelInformation rpc.
	BaseFeeMsat uint64 `json:"baseFeeMsat,string"`

	// The fee rate for routing payments over the channel. It is configured on
	// the node itself, but this value is returned in the ChannelInformation rpc.
	FeeRate float64 `json:"feeRate,string"`

	// Minimum timelock delta required for opening a zero conf channel.
	TimeLockDelta uint32 `json:"timeLockDelta,string"`

	// Fee for opening a zero conf channel in satoshi per 10000 satoshi based
	// on the incoming payment amount.
	ChannelFeePermyriad int64 `json:"channelFeePermyriad,string"`

	// Minimum fee for opening a zero conf channel in millisatoshi.
	ChannelMinimumFeeMsat int64 `json:"channelMinimumFeeMsat,string"`

	// Channel capacity that is added on top of the incoming payment amount
	// when a new zero conf channel is opened. In satoshi.
	AdditionalChannelCapacity int64 `json:"additionalChannelCapacity,string"`

	// The channel can be closed if not used this duration in seconds.
	MaxInactiveDuration uint64 `json:"maxInactiveDuration,string"`

	// The maximum time to hold a htlc after sending a notification when the
	// peer is offline.
	NotificationTimeout string `json:"notificationTimeout,string"`

	MinPaymentSizeMsat uint64 `json:"minPaymentSizeMsat,string"`
	MaxPaymentSizeMsat uint64 `json:"maxPaymentSizeMsat,string"`

	// Set this field to connect to an LND node.
	Lnd *LndConfig `json:"lnd,omitempty"`

	// Set this field to connect to a CLN node.
	Cln *ClnConfig `json:"cln,omitempty"`
}

type LndConfig struct {
	// Address to the grpc api.
	Address string `json:"address"`

	// tls cert for the grpc api.
	Cert string `json:"cert"`

	// macaroon to use.
	Macaroon string `json:"macaroon"`
}

type ClnConfig struct {
	// The address to the cln htlc acceptor grpc api shipped with lspd.
	PluginAddress string `json:"pluginAddress"`

	// File path to the cln lightning-roc socket file. Find the path in
	// cln-dir/mainnet/lightning-rpc
	SocketPath string `json:"socketPath"`
}
