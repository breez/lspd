package config

type NodeConfig struct {
	// Name of the LSP. If empty, the node's alias will be taken instead. The
	// name is returned in the ChannelInformation api endpoint as a friendly
	// name.
	Name string `json:name,omitempty`

	// The public key of the lightning node. If empty, the node's pubkey will be
	// taken instead. The pubkey is returned in the ChannelInformation api
	// endpoint, s
	NodePubkey string `json:nodePubkey,omitempty`

	// Hex encoded private key of the LSP. This is used to decrypt traffic from
	// clients. Note this is not the private key of the node.
	LspdPrivateKey string `json:"lspdPrivateKey"`

	// Tokens used to authenticate to lspd. These tokens must be unique for each
	// configured node, so it's obvious which node an rpc call is meant for.
	Tokens []string `json:"tokens"`

	// If the used token is in the LegacyOnionTokens array, the forwarded htlc
	// will have the legacy onion format. As per the time of writing breezmobile
	// requires the legacy onion format.
	LegacyOnionTokens []string `json:"legacyOnionTokens"`

	// The network location of the lightning node, e.g. `12.34.56.78:9012` or
	// `localhost:10011`. The host is returned in the ChannelInformation api
	// endpoint so clients can connect to the node.
	Host string `json:"host"`

	// Public channel amount is a reserved amount for public channels. If a
	// zero conf channel is opened, it will never have this exact amount. This
	// configuration option is Breez specific and can be ignored by others
	// running lspd.
	PublicChannelAmount int64 `json:"publicChannelAmount,string,omitempty"`

	// Number of blocks after which an opened channel is considered confirmed.
	TargetConf uint32 `json:"targetConf,string"`

	// Minimum number of confirmations inputs for zero conf channel opens should
	// have.
	MinConfs *uint32 `json:"minConfs,string"`

	// Smallest htlc amount routed over channels a channel. It is configured on
	// the node itself, but this value is returned in the ChannelInformation rpc.
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
	// This field is for legacy support for clients that don't support
	// OpeningFeeParams.
	ChannelFeePermyriad int64 `json:"channelFeePermyriad,string"`

	// Minimum fee for opening a zero conf channel in millisatoshi. If the fee
	// using ChannelFeePermyriad is less than this amount, this amount is the
	// actual fee to be paid.
	// This field is for legacy support for clients that don't support
	// OpeningFeeParams.
	ChannelMinimumFeeMsat int64 `json:"channelMinimumFeeMsat,string"`

	// Channel capacity that is added on top of the incoming payment amount
	// when a new zero conf channel is opened. In satoshi.
	AdditionalChannelCapacity int64 `json:"additionalChannelCapacity,string"`

	// The channel can be closed if not used this duration in seconds. This is
	// not enforced by lspd, but gives an indication to clients.
	// This field is for legacy support for clients that don't support
	// OpeningFeeParams.
	MaxInactiveDuration uint64 `json:"maxInactiveDuration,string"`

	// The maximum time to hold a htlc after sending a notification when the
	// peer is offline. Defaults to 1m.
	NotificationTimeout string `json:"notificationTimeout,string"`

	// The minimum payment size accepted in LSPS2 forwards that need a channel
	// open.
	MinPaymentSizeMsat uint64 `json:"minPaymentSizeMsat,string"`

	// The maximum payment size accepted in LSPS2 forwards that need a channel
	// open.
	MaxPaymentSizeMsat uint64 `json:"maxPaymentSizeMsat,string"`

	// Set this field to connect to an LND node.
	Lnd *LndConfig `json:"lnd,omitempty"`

	// Set this field to connect to a CLN node.
	Cln *ClnConfig `json:"cln,omitempty"`
}

type LndConfig struct {
	// Address to the grpc api.
	Address string `json:"address"`

	// tls cert for the grpc api. Can either be a file path or the cert
	// contents. Typically stored in `lnd-dir/tls.cert`.
	Cert string `json:"cert"`

	// macaroon to use. Can either be a file path or the macaroon contents.
	Macaroon string `json:"macaroon"`
}

type ClnConfig struct {
	// The address to the cln htlc acceptor grpc api shipped with lspd.
	PluginAddress string `json:"pluginAddress"`

	// The address to the cln grpc api.
	GrpcAddress string `json:"grpcAddress"`

	// CA cert for grpc access. Can either be a file path or the cert contents.
	// Typically stored in `lightningd-dir/mainnet/ca.pem`.
	CaCert string `json:"caCert"`

	// Client cert for grpc access. Can either be a file path or the cert
	// contents. Typically stored in `lightningd-dir/{network}/client.pem`.
	ClientCert string `json:"clientCert"`

	// Client key for grpc access. Can either be a file path or the key
	// contents. Typically stored in `lightningd-dir/{network}/client-key.pem`.
	ClientKey string `json:"clientKey"`
}
