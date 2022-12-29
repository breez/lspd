package cln_plugin

import (
	"encoding/json"
)

type Request struct {
	Id      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	JsonRpc string          `json:"jsonrpc"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	Id      json.RawMessage `json:"id"`
	JsonRpc string          `json:"jsonrpc"`
	Result  Result          `json:"result,omitempty"`
	Error   *RpcError       `json:"error,omitempty"`
}

type Result interface{}

type RpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Manifest struct {
	Options       []Option     `json:"options"`
	RpcMethods    []*RpcMethod `json:"rpcmethods"`
	Dynamic       bool         `json:"dynamic"`
	Subscriptions []string     `json:"subscriptions,omitempty"`
	Hooks         []Hook       `json:"hooks,omitempty"`
	FeatureBits   *FeatureBits `json:"featurebits,omitempty"`
	NonNumericIds bool         `json:"nonnumericids"`
}

type Option struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Default     *string `json:"default,omitempty"`
	Multi       *bool   `json:"multi,omitempty"`
	Deprecated  *bool   `json:"deprecated,omitempty"`
}

type RpcMethod struct {
	Name            string  `json:"name"`
	Usage           string  `json:"usage"`
	Description     string  `json:"description"`
	LongDescription *string `json:"long_description,omitempty"`
	Deprecated      *bool   `json:"deprecated,omitempty"`
}

type Hook struct {
	Name   string   `json:"name"`
	Before []string `json:"before,omitempty"`
}

type FeatureBits struct {
	Node    *string `json:"node,omitempty"`
	Init    *string `json:"init,omitempty"`
	Channel *string `json:"channel,omitempty"`
	Invoice *string `json:"invoice,omitempty"`
}

type InitMessage struct {
	Options       map[string]interface{} `json:"options,omitempty"`
	Configuration *InitConfiguration     `json:"configuration,omitempty"`
}

type InitConfiguration struct {
	LightningDir   string       `json:"lightning-dir"`
	RpcFile        string       `json:"rpc-file"`
	Startup        bool         `json:"startup"`
	Network        string       `json:"network"`
	FeatureSet     *FeatureBits `json:"feature_set"`
	Proxy          *Proxy       `json:"proxy"`
	TorV3Enabled   bool         `json:"torv3-enabled"`
	AlwaysUseProxy bool         `json:"always_use_proxy"`
}

type Proxy struct {
	Type    string `json:"type"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type HtlcAccepted struct {
	Onion     *Onion `json:"onion"`
	Htlc      *Htlc  `json:"htlc"`
	ForwardTo string `json:"forward_to"`
}

type Onion struct {
	Payload           string `json:"payload"`
	ShortChannelId    string `json:"short_channel_id"`
	ForwardMsat       uint64 `json:"forward_msat"`
	OutgoingCltvValue uint32 `json:"outgoing_cltv_value"`
	SharedSecret      string `json:"shared_secret"`
	NextOnion         string `json:"next_onion"`
}

type Htlc struct {
	ShortChannelId     string `json:"short_channel_id"`
	Id                 uint64 `json:"id"`
	AmountMsat         uint64 `json:"amount_msat"`
	CltvExpiry         uint32 `json:"cltv_expiry"`
	CltvExpiryRelative uint32 `json:"cltv_expiry_relative"`
	PaymentHash        string `json:"payment_hash"`
}

type LogNotification struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}
