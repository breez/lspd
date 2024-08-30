package lntest

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type LightningNode interface {
	Stoppable
	NodeId() []byte
	Host() string
	Port() uint32
	Start()
	IsStarted() bool
	PrivateKey() *secp256k1.PrivateKey

	WaitForSync()
	GetNewAddress() string
	Fund(amountSat uint64)
	ConnectPeer(peer LightningNode)
	OpenChannel(peer LightningNode, options *OpenChannelOptions) *ChannelInfo
	WaitForChannelReady(channel *ChannelInfo) ShortChannelID
	CreateBolt11Invoice(options *CreateInvoiceOptions) *CreateInvoiceResult
	SignMessage(message []byte) []byte
	Pay(bolt11 string) *PayResult
	GetRoute(destination []byte, amountMsat uint64) *Route
	PayViaRoute(
		amountMsat uint64,
		paymentHash []byte,
		paymentSecret []byte,
		route *Route) (*PayResult, error)
	GetInvoice(paymentHash []byte) *GetInvoiceResponse
	GetPeerFeatures(peerId []byte) map[uint32]string
	GetRemoteNodeFeatures(nodeId []byte) map[uint32]string
	GetChannels() []*ChannelDetails
	SendToAddress(addr string, amountSat uint64)
	SendCustomMessage(req *CustomMsgRequest)
}

type OpenChannelOptions struct {
	IsPublic  bool
	AmountSat uint64
}

type CreateInvoiceOptions struct {
	AmountMsat      uint64
	Description     *string
	Preimage        *[]byte
	Cltv            *uint32
	IncludeHopHints bool
}

type CreateInvoiceResult struct {
	Bolt11        string
	PaymentHash   []byte
	PaymentSecret []byte
}

type PayResult struct {
	PaymentHash     []byte
	AmountMsat      uint64
	Destination     []byte
	AmountSentMsat  uint64
	PaymentPreimage []byte
}

type Route struct {
	Hops []*Hop
}

type Hop struct {
	Id         []byte
	Channel    ShortChannelID
	AmountMsat uint64
	Delay      uint16
}

type PayViaRouteResponse struct {
	PartId uint32
}

type WaitPaymentCompleteResponse struct {
	PaymentHash     []byte
	AmountMsat      uint64
	Destination     []byte
	CreatedAt       uint64
	AmountSentMsat  uint64
	PaymentPreimage []byte
}

type InvoiceStatus int32

const (
	Invoice_UNPAID  InvoiceStatus = 0
	Invoice_PAID    InvoiceStatus = 1
	Invoice_EXPIRED InvoiceStatus = 2
)

type GetInvoiceResponse struct {
	Exists             bool
	AmountMsat         uint64
	AmountReceivedMsat uint64
	Bolt11             *string
	Description        *string
	ExpiresAt          uint64
	PaidAt             *uint64
	PayerNote          *string
	PaymentHash        []byte
	PaymentPreimage    []byte
	IsPaid             bool
	IsExpired          bool
}

type ShortChannelID struct {
	BlockHeight uint32
	TxIndex     uint32
	OutputIndex uint16
}

func NewShortChanIDFromInt(chanID uint64) ShortChannelID {
	return ShortChannelID{
		BlockHeight: uint32(chanID >> 40),
		TxIndex:     uint32(chanID>>16) & 0xFFFFFF,
		OutputIndex: uint16(chanID),
	}
}

func NewShortChanIDFromString(chanID string) ShortChannelID {
	split := strings.Split(chanID, "x")
	bh, _ := strconv.ParseUint(split[0], 10, 32)
	ti, _ := strconv.ParseUint(split[1], 10, 32)
	oi, _ := strconv.ParseUint(split[2], 10, 16)

	return ShortChannelID{
		BlockHeight: uint32(bh),
		TxIndex:     uint32(ti),
		OutputIndex: uint16(oi),
	}
}

func (c ShortChannelID) ToUint64() uint64 {
	return ((uint64(c.BlockHeight) << 40) | (uint64(c.TxIndex) << 16) |
		(uint64(c.OutputIndex)))
}

func (c ShortChannelID) String() string {
	return fmt.Sprintf("%dx%dx%d", c.BlockHeight, c.TxIndex, c.OutputIndex)
}

type ChannelDetails struct {
	PeerId              []byte
	ShortChannelID      ShortChannelID
	CapacityMsat        uint64
	LocalReserveMsat    uint64
	RemoteReserveMsat   uint64
	LocalSpendableMsat  uint64
	RemoteSpendableMsat uint64
	LocalAlias          *ShortChannelID
	RemoteAlias         *ShortChannelID
}

type CustomMsgRequest struct {
	PeerId string
	Type   uint32
	Data   []byte
}
