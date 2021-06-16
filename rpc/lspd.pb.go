// Code generated by protoc-gen-go. DO NOT EDIT.
// source: lspd.proto

package lspd

import (
	context "context"
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

type ChannelInformationRequest struct {
	/// The identity pubkey of the Lightning node
	Pubkey               string   `protobuf:"bytes,1,opt,name=pubkey,proto3" json:"pubkey,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ChannelInformationRequest) Reset()         { *m = ChannelInformationRequest{} }
func (m *ChannelInformationRequest) String() string { return proto.CompactTextString(m) }
func (*ChannelInformationRequest) ProtoMessage()    {}
func (*ChannelInformationRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{0}
}

func (m *ChannelInformationRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ChannelInformationRequest.Unmarshal(m, b)
}
func (m *ChannelInformationRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ChannelInformationRequest.Marshal(b, m, deterministic)
}
func (m *ChannelInformationRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ChannelInformationRequest.Merge(m, src)
}
func (m *ChannelInformationRequest) XXX_Size() int {
	return xxx_messageInfo_ChannelInformationRequest.Size(m)
}
func (m *ChannelInformationRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_ChannelInformationRequest.DiscardUnknown(m)
}

var xxx_messageInfo_ChannelInformationRequest proto.InternalMessageInfo

func (m *ChannelInformationRequest) GetPubkey() string {
	if m != nil {
		return m.Pubkey
	}
	return ""
}

type ChannelInformationReply struct {
	/// The name of of lsp
	Name string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	/// The identity pubkey of the Lightning node
	Pubkey string `protobuf:"bytes,2,opt,name=pubkey,proto3" json:"pubkey,omitempty"`
	/// The network location of the lightning node, e.g. `12.34.56.78:9012` or
	/// `localhost:10011`
	Host string `protobuf:"bytes,3,opt,name=host,proto3" json:"host,omitempty"`
	/// The channel capacity in satoshis
	ChannelCapacity int64 `protobuf:"varint,4,opt,name=channel_capacity,proto3" json:"channel_capacity,omitempty"`
	/// The target number of blocks that the funding transaction should be
	/// confirmed by.
	TargetConf int32 `protobuf:"varint,5,opt,name=target_conf,proto3" json:"target_conf,omitempty"`
	/// The base fee charged regardless of the number of milli-satoshis sent.
	BaseFeeMsat int64 `protobuf:"varint,6,opt,name=base_fee_msat,proto3" json:"base_fee_msat,omitempty"`
	/// The effective fee rate in milli-satoshis. The precision of this value goes
	/// up to 6 decimal places, so 1e-6.
	FeeRate float64 `protobuf:"fixed64,7,opt,name=fee_rate,proto3" json:"fee_rate,omitempty"`
	/// The required timelock delta for HTLCs forwarded over the channel.
	TimeLockDelta uint32 `protobuf:"varint,8,opt,name=time_lock_delta,proto3" json:"time_lock_delta,omitempty"`
	/// The minimum value in millisatoshi we will require for incoming HTLCs on
	/// the channel.
	MinHtlcMsat         int64  `protobuf:"varint,9,opt,name=min_htlc_msat,proto3" json:"min_htlc_msat,omitempty"`
	ChannelFeePermyriad int64  `protobuf:"varint,10,opt,name=channel_fee_permyriad,json=channelFeePermyriad,proto3" json:"channel_fee_permyriad,omitempty"`
	LspPubkey           []byte `protobuf:"bytes,11,opt,name=lsp_pubkey,json=lspPubkey,proto3" json:"lsp_pubkey,omitempty"`
	// The channel can be closed if not used this duration in seconds.
	MaxInactiveDuration   int64    `protobuf:"varint,12,opt,name=max_inactive_duration,json=maxInactiveDuration,proto3" json:"max_inactive_duration,omitempty"`
	ChannelMinimumFeeMsat int64    `protobuf:"varint,13,opt,name=channel_minimum_fee_msat,json=channelMinimumFeeMsat,proto3" json:"channel_minimum_fee_msat,omitempty"`
	XXX_NoUnkeyedLiteral  struct{} `json:"-"`
	XXX_unrecognized      []byte   `json:"-"`
	XXX_sizecache         int32    `json:"-"`
}

func (m *ChannelInformationReply) Reset()         { *m = ChannelInformationReply{} }
func (m *ChannelInformationReply) String() string { return proto.CompactTextString(m) }
func (*ChannelInformationReply) ProtoMessage()    {}
func (*ChannelInformationReply) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{1}
}

func (m *ChannelInformationReply) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ChannelInformationReply.Unmarshal(m, b)
}
func (m *ChannelInformationReply) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ChannelInformationReply.Marshal(b, m, deterministic)
}
func (m *ChannelInformationReply) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ChannelInformationReply.Merge(m, src)
}
func (m *ChannelInformationReply) XXX_Size() int {
	return xxx_messageInfo_ChannelInformationReply.Size(m)
}
func (m *ChannelInformationReply) XXX_DiscardUnknown() {
	xxx_messageInfo_ChannelInformationReply.DiscardUnknown(m)
}

var xxx_messageInfo_ChannelInformationReply proto.InternalMessageInfo

func (m *ChannelInformationReply) GetName() string {
	if m != nil {
		return m.Name
	}
	return ""
}

func (m *ChannelInformationReply) GetPubkey() string {
	if m != nil {
		return m.Pubkey
	}
	return ""
}

func (m *ChannelInformationReply) GetHost() string {
	if m != nil {
		return m.Host
	}
	return ""
}

func (m *ChannelInformationReply) GetChannelCapacity() int64 {
	if m != nil {
		return m.ChannelCapacity
	}
	return 0
}

func (m *ChannelInformationReply) GetTargetConf() int32 {
	if m != nil {
		return m.TargetConf
	}
	return 0
}

func (m *ChannelInformationReply) GetBaseFeeMsat() int64 {
	if m != nil {
		return m.BaseFeeMsat
	}
	return 0
}

func (m *ChannelInformationReply) GetFeeRate() float64 {
	if m != nil {
		return m.FeeRate
	}
	return 0
}

func (m *ChannelInformationReply) GetTimeLockDelta() uint32 {
	if m != nil {
		return m.TimeLockDelta
	}
	return 0
}

func (m *ChannelInformationReply) GetMinHtlcMsat() int64 {
	if m != nil {
		return m.MinHtlcMsat
	}
	return 0
}

func (m *ChannelInformationReply) GetChannelFeePermyriad() int64 {
	if m != nil {
		return m.ChannelFeePermyriad
	}
	return 0
}

func (m *ChannelInformationReply) GetLspPubkey() []byte {
	if m != nil {
		return m.LspPubkey
	}
	return nil
}

func (m *ChannelInformationReply) GetMaxInactiveDuration() int64 {
	if m != nil {
		return m.MaxInactiveDuration
	}
	return 0
}

func (m *ChannelInformationReply) GetChannelMinimumFeeMsat() int64 {
	if m != nil {
		return m.ChannelMinimumFeeMsat
	}
	return 0
}

type OpenChannelRequest struct {
	/// The identity pubkey of the Lightning node
	Pubkey               string   `protobuf:"bytes,1,opt,name=pubkey,proto3" json:"pubkey,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *OpenChannelRequest) Reset()         { *m = OpenChannelRequest{} }
func (m *OpenChannelRequest) String() string { return proto.CompactTextString(m) }
func (*OpenChannelRequest) ProtoMessage()    {}
func (*OpenChannelRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{2}
}

func (m *OpenChannelRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_OpenChannelRequest.Unmarshal(m, b)
}
func (m *OpenChannelRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_OpenChannelRequest.Marshal(b, m, deterministic)
}
func (m *OpenChannelRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_OpenChannelRequest.Merge(m, src)
}
func (m *OpenChannelRequest) XXX_Size() int {
	return xxx_messageInfo_OpenChannelRequest.Size(m)
}
func (m *OpenChannelRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_OpenChannelRequest.DiscardUnknown(m)
}

var xxx_messageInfo_OpenChannelRequest proto.InternalMessageInfo

func (m *OpenChannelRequest) GetPubkey() string {
	if m != nil {
		return m.Pubkey
	}
	return ""
}

type OpenChannelReply struct {
	/// The transaction hash
	TxHash string `protobuf:"bytes,1,opt,name=tx_hash,proto3" json:"tx_hash,omitempty"`
	/// The output index
	OutputIndex          uint32   `protobuf:"varint,2,opt,name=output_index,proto3" json:"output_index,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *OpenChannelReply) Reset()         { *m = OpenChannelReply{} }
func (m *OpenChannelReply) String() string { return proto.CompactTextString(m) }
func (*OpenChannelReply) ProtoMessage()    {}
func (*OpenChannelReply) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{3}
}

func (m *OpenChannelReply) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_OpenChannelReply.Unmarshal(m, b)
}
func (m *OpenChannelReply) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_OpenChannelReply.Marshal(b, m, deterministic)
}
func (m *OpenChannelReply) XXX_Merge(src proto.Message) {
	xxx_messageInfo_OpenChannelReply.Merge(m, src)
}
func (m *OpenChannelReply) XXX_Size() int {
	return xxx_messageInfo_OpenChannelReply.Size(m)
}
func (m *OpenChannelReply) XXX_DiscardUnknown() {
	xxx_messageInfo_OpenChannelReply.DiscardUnknown(m)
}

var xxx_messageInfo_OpenChannelReply proto.InternalMessageInfo

func (m *OpenChannelReply) GetTxHash() string {
	if m != nil {
		return m.TxHash
	}
	return ""
}

func (m *OpenChannelReply) GetOutputIndex() uint32 {
	if m != nil {
		return m.OutputIndex
	}
	return 0
}

type RegisterPaymentRequest struct {
	Blob                 []byte   `protobuf:"bytes,3,opt,name=blob,proto3" json:"blob,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *RegisterPaymentRequest) Reset()         { *m = RegisterPaymentRequest{} }
func (m *RegisterPaymentRequest) String() string { return proto.CompactTextString(m) }
func (*RegisterPaymentRequest) ProtoMessage()    {}
func (*RegisterPaymentRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{4}
}

func (m *RegisterPaymentRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_RegisterPaymentRequest.Unmarshal(m, b)
}
func (m *RegisterPaymentRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_RegisterPaymentRequest.Marshal(b, m, deterministic)
}
func (m *RegisterPaymentRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_RegisterPaymentRequest.Merge(m, src)
}
func (m *RegisterPaymentRequest) XXX_Size() int {
	return xxx_messageInfo_RegisterPaymentRequest.Size(m)
}
func (m *RegisterPaymentRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_RegisterPaymentRequest.DiscardUnknown(m)
}

var xxx_messageInfo_RegisterPaymentRequest proto.InternalMessageInfo

func (m *RegisterPaymentRequest) GetBlob() []byte {
	if m != nil {
		return m.Blob
	}
	return nil
}

type RegisterPaymentReply struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *RegisterPaymentReply) Reset()         { *m = RegisterPaymentReply{} }
func (m *RegisterPaymentReply) String() string { return proto.CompactTextString(m) }
func (*RegisterPaymentReply) ProtoMessage()    {}
func (*RegisterPaymentReply) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{5}
}

func (m *RegisterPaymentReply) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_RegisterPaymentReply.Unmarshal(m, b)
}
func (m *RegisterPaymentReply) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_RegisterPaymentReply.Marshal(b, m, deterministic)
}
func (m *RegisterPaymentReply) XXX_Merge(src proto.Message) {
	xxx_messageInfo_RegisterPaymentReply.Merge(m, src)
}
func (m *RegisterPaymentReply) XXX_Size() int {
	return xxx_messageInfo_RegisterPaymentReply.Size(m)
}
func (m *RegisterPaymentReply) XXX_DiscardUnknown() {
	xxx_messageInfo_RegisterPaymentReply.DiscardUnknown(m)
}

var xxx_messageInfo_RegisterPaymentReply proto.InternalMessageInfo

type PaymentInformation struct {
	PaymentHash          []byte   `protobuf:"bytes,1,opt,name=payment_hash,json=paymentHash,proto3" json:"payment_hash,omitempty"`
	PaymentSecret        []byte   `protobuf:"bytes,2,opt,name=payment_secret,json=paymentSecret,proto3" json:"payment_secret,omitempty"`
	Destination          []byte   `protobuf:"bytes,3,opt,name=destination,proto3" json:"destination,omitempty"`
	IncomingAmountMsat   int64    `protobuf:"varint,4,opt,name=incoming_amount_msat,json=incomingAmountMsat,proto3" json:"incoming_amount_msat,omitempty"`
	OutgoingAmountMsat   int64    `protobuf:"varint,5,opt,name=outgoing_amount_msat,json=outgoingAmountMsat,proto3" json:"outgoing_amount_msat,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *PaymentInformation) Reset()         { *m = PaymentInformation{} }
func (m *PaymentInformation) String() string { return proto.CompactTextString(m) }
func (*PaymentInformation) ProtoMessage()    {}
func (*PaymentInformation) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{6}
}

func (m *PaymentInformation) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PaymentInformation.Unmarshal(m, b)
}
func (m *PaymentInformation) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PaymentInformation.Marshal(b, m, deterministic)
}
func (m *PaymentInformation) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PaymentInformation.Merge(m, src)
}
func (m *PaymentInformation) XXX_Size() int {
	return xxx_messageInfo_PaymentInformation.Size(m)
}
func (m *PaymentInformation) XXX_DiscardUnknown() {
	xxx_messageInfo_PaymentInformation.DiscardUnknown(m)
}

var xxx_messageInfo_PaymentInformation proto.InternalMessageInfo

func (m *PaymentInformation) GetPaymentHash() []byte {
	if m != nil {
		return m.PaymentHash
	}
	return nil
}

func (m *PaymentInformation) GetPaymentSecret() []byte {
	if m != nil {
		return m.PaymentSecret
	}
	return nil
}

func (m *PaymentInformation) GetDestination() []byte {
	if m != nil {
		return m.Destination
	}
	return nil
}

func (m *PaymentInformation) GetIncomingAmountMsat() int64 {
	if m != nil {
		return m.IncomingAmountMsat
	}
	return 0
}

func (m *PaymentInformation) GetOutgoingAmountMsat() int64 {
	if m != nil {
		return m.OutgoingAmountMsat
	}
	return 0
}

type Encrypted struct {
	Data                 []byte   `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *Encrypted) Reset()         { *m = Encrypted{} }
func (m *Encrypted) String() string { return proto.CompactTextString(m) }
func (*Encrypted) ProtoMessage()    {}
func (*Encrypted) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{7}
}

func (m *Encrypted) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Encrypted.Unmarshal(m, b)
}
func (m *Encrypted) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Encrypted.Marshal(b, m, deterministic)
}
func (m *Encrypted) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Encrypted.Merge(m, src)
}
func (m *Encrypted) XXX_Size() int {
	return xxx_messageInfo_Encrypted.Size(m)
}
func (m *Encrypted) XXX_DiscardUnknown() {
	xxx_messageInfo_Encrypted.DiscardUnknown(m)
}

var xxx_messageInfo_Encrypted proto.InternalMessageInfo

func (m *Encrypted) GetData() []byte {
	if m != nil {
		return m.Data
	}
	return nil
}

type Signed struct {
	Data                 []byte   `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
	Pubkey               []byte   `protobuf:"bytes,2,opt,name=pubkey,proto3" json:"pubkey,omitempty"`
	Signature            []byte   `protobuf:"bytes,3,opt,name=signature,proto3" json:"signature,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *Signed) Reset()         { *m = Signed{} }
func (m *Signed) String() string { return proto.CompactTextString(m) }
func (*Signed) ProtoMessage()    {}
func (*Signed) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{8}
}

func (m *Signed) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Signed.Unmarshal(m, b)
}
func (m *Signed) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Signed.Marshal(b, m, deterministic)
}
func (m *Signed) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Signed.Merge(m, src)
}
func (m *Signed) XXX_Size() int {
	return xxx_messageInfo_Signed.Size(m)
}
func (m *Signed) XXX_DiscardUnknown() {
	xxx_messageInfo_Signed.DiscardUnknown(m)
}

var xxx_messageInfo_Signed proto.InternalMessageInfo

func (m *Signed) GetData() []byte {
	if m != nil {
		return m.Data
	}
	return nil
}

func (m *Signed) GetPubkey() []byte {
	if m != nil {
		return m.Pubkey
	}
	return nil
}

func (m *Signed) GetSignature() []byte {
	if m != nil {
		return m.Signature
	}
	return nil
}

type CheckChannelsRequest struct {
	EncryptPubkey        []byte            `protobuf:"bytes,1,opt,name=encrypt_pubkey,json=encryptPubkey,proto3" json:"encrypt_pubkey,omitempty"`
	FakeChannels         map[string]uint64 `protobuf:"bytes,2,rep,name=fake_channels,json=fakeChannels,proto3" json:"fake_channels,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"varint,2,opt,name=value,proto3"`
	WaitingCloseChannels map[string]uint64 `protobuf:"bytes,3,rep,name=waiting_close_channels,json=waitingCloseChannels,proto3" json:"waiting_close_channels,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"varint,2,opt,name=value,proto3"`
	XXX_NoUnkeyedLiteral struct{}          `json:"-"`
	XXX_unrecognized     []byte            `json:"-"`
	XXX_sizecache        int32             `json:"-"`
}

func (m *CheckChannelsRequest) Reset()         { *m = CheckChannelsRequest{} }
func (m *CheckChannelsRequest) String() string { return proto.CompactTextString(m) }
func (*CheckChannelsRequest) ProtoMessage()    {}
func (*CheckChannelsRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{9}
}

func (m *CheckChannelsRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_CheckChannelsRequest.Unmarshal(m, b)
}
func (m *CheckChannelsRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_CheckChannelsRequest.Marshal(b, m, deterministic)
}
func (m *CheckChannelsRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_CheckChannelsRequest.Merge(m, src)
}
func (m *CheckChannelsRequest) XXX_Size() int {
	return xxx_messageInfo_CheckChannelsRequest.Size(m)
}
func (m *CheckChannelsRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_CheckChannelsRequest.DiscardUnknown(m)
}

var xxx_messageInfo_CheckChannelsRequest proto.InternalMessageInfo

func (m *CheckChannelsRequest) GetEncryptPubkey() []byte {
	if m != nil {
		return m.EncryptPubkey
	}
	return nil
}

func (m *CheckChannelsRequest) GetFakeChannels() map[string]uint64 {
	if m != nil {
		return m.FakeChannels
	}
	return nil
}

func (m *CheckChannelsRequest) GetWaitingCloseChannels() map[string]uint64 {
	if m != nil {
		return m.WaitingCloseChannels
	}
	return nil
}

type CheckChannelsReply struct {
	NotFakeChannels      map[string]uint64 `protobuf:"bytes,1,rep,name=not_fake_channels,json=notFakeChannels,proto3" json:"not_fake_channels,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"varint,2,opt,name=value,proto3"`
	ClosedChannels       map[string]uint64 `protobuf:"bytes,2,rep,name=closed_channels,json=closedChannels,proto3" json:"closed_channels,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"varint,2,opt,name=value,proto3"`
	XXX_NoUnkeyedLiteral struct{}          `json:"-"`
	XXX_unrecognized     []byte            `json:"-"`
	XXX_sizecache        int32             `json:"-"`
}

func (m *CheckChannelsReply) Reset()         { *m = CheckChannelsReply{} }
func (m *CheckChannelsReply) String() string { return proto.CompactTextString(m) }
func (*CheckChannelsReply) ProtoMessage()    {}
func (*CheckChannelsReply) Descriptor() ([]byte, []int) {
	return fileDescriptor_c69a0f5a734bca26, []int{10}
}

func (m *CheckChannelsReply) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_CheckChannelsReply.Unmarshal(m, b)
}
func (m *CheckChannelsReply) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_CheckChannelsReply.Marshal(b, m, deterministic)
}
func (m *CheckChannelsReply) XXX_Merge(src proto.Message) {
	xxx_messageInfo_CheckChannelsReply.Merge(m, src)
}
func (m *CheckChannelsReply) XXX_Size() int {
	return xxx_messageInfo_CheckChannelsReply.Size(m)
}
func (m *CheckChannelsReply) XXX_DiscardUnknown() {
	xxx_messageInfo_CheckChannelsReply.DiscardUnknown(m)
}

var xxx_messageInfo_CheckChannelsReply proto.InternalMessageInfo

func (m *CheckChannelsReply) GetNotFakeChannels() map[string]uint64 {
	if m != nil {
		return m.NotFakeChannels
	}
	return nil
}

func (m *CheckChannelsReply) GetClosedChannels() map[string]uint64 {
	if m != nil {
		return m.ClosedChannels
	}
	return nil
}

func init() {
	proto.RegisterType((*ChannelInformationRequest)(nil), "lspd.ChannelInformationRequest")
	proto.RegisterType((*ChannelInformationReply)(nil), "lspd.ChannelInformationReply")
	proto.RegisterType((*OpenChannelRequest)(nil), "lspd.OpenChannelRequest")
	proto.RegisterType((*OpenChannelReply)(nil), "lspd.OpenChannelReply")
	proto.RegisterType((*RegisterPaymentRequest)(nil), "lspd.RegisterPaymentRequest")
	proto.RegisterType((*RegisterPaymentReply)(nil), "lspd.RegisterPaymentReply")
	proto.RegisterType((*PaymentInformation)(nil), "lspd.PaymentInformation")
	proto.RegisterType((*Encrypted)(nil), "lspd.Encrypted")
	proto.RegisterType((*Signed)(nil), "lspd.Signed")
	proto.RegisterType((*CheckChannelsRequest)(nil), "lspd.CheckChannelsRequest")
	proto.RegisterMapType((map[string]uint64)(nil), "lspd.CheckChannelsRequest.FakeChannelsEntry")
	proto.RegisterMapType((map[string]uint64)(nil), "lspd.CheckChannelsRequest.WaitingCloseChannelsEntry")
	proto.RegisterType((*CheckChannelsReply)(nil), "lspd.CheckChannelsReply")
	proto.RegisterMapType((map[string]uint64)(nil), "lspd.CheckChannelsReply.ClosedChannelsEntry")
	proto.RegisterMapType((map[string]uint64)(nil), "lspd.CheckChannelsReply.NotFakeChannelsEntry")
}

func init() {
	proto.RegisterFile("lspd.proto", fileDescriptor_c69a0f5a734bca26)
}

var fileDescriptor_c69a0f5a734bca26 = []byte{
	// 899 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x94, 0x56, 0xef, 0x6e, 0xe3, 0x44,
	0x10, 0x3f, 0x37, 0x69, 0xef, 0x32, 0x71, 0x9a, 0xde, 0x5e, 0x2e, 0xf8, 0xa2, 0x3b, 0x5d, 0x30,
	0x9c, 0x14, 0xa1, 0x12, 0xa1, 0x16, 0x09, 0xc4, 0x17, 0xd4, 0x96, 0x2b, 0x9c, 0x44, 0x21, 0xf8,
	0x04, 0x88, 0x4f, 0xd6, 0xc6, 0x9e, 0x24, 0x4b, 0xec, 0xb5, 0xb1, 0xd7, 0xbd, 0xe6, 0x05, 0x78,
	0x1c, 0xde, 0x84, 0xb7, 0xe0, 0x25, 0xf8, 0x86, 0xf6, 0x8f, 0x1b, 0x27, 0x71, 0x81, 0x7e, 0x5b,
	0xff, 0x66, 0xe6, 0x37, 0xb3, 0xbf, 0x99, 0x9d, 0x04, 0x20, 0xca, 0xd3, 0x70, 0x9c, 0x66, 0x89,
	0x48, 0x48, 0x53, 0x9e, 0xdd, 0x53, 0x78, 0x76, 0xb1, 0xa0, 0x9c, 0x63, 0xf4, 0x86, 0xcf, 0x92,
	0x2c, 0xa6, 0x82, 0x25, 0xdc, 0xc3, 0xdf, 0x0a, 0xcc, 0x05, 0xe9, 0xc3, 0x41, 0x5a, 0x4c, 0x97,
	0xb8, 0x72, 0xac, 0xa1, 0x35, 0x6a, 0x79, 0xe6, 0xcb, 0xfd, 0xbb, 0x01, 0xef, 0xd5, 0x45, 0xa5,
	0xd1, 0x8a, 0x10, 0x68, 0x72, 0x1a, 0xa3, 0x89, 0x50, 0xe7, 0x0a, 0xcf, 0x5e, 0x95, 0x47, 0xfa,
	0x2e, 0x92, 0x5c, 0x38, 0x0d, 0xed, 0x2b, 0xcf, 0xe4, 0x23, 0x38, 0x0a, 0x34, 0xb5, 0x1f, 0xd0,
	0x94, 0x06, 0x4c, 0xac, 0x9c, 0xe6, 0xd0, 0x1a, 0x35, 0xbc, 0x1d, 0x9c, 0x0c, 0xa1, 0x2d, 0x68,
	0x36, 0x47, 0xe1, 0x07, 0x09, 0x9f, 0x39, 0xfb, 0x43, 0x6b, 0xb4, 0xef, 0x55, 0x21, 0xf2, 0x21,
	0x74, 0xa6, 0x34, 0x47, 0x7f, 0x86, 0xe8, 0xc7, 0x39, 0x15, 0xce, 0x81, 0xa2, 0xda, 0x04, 0xc9,
	0x00, 0x1e, 0xc9, 0x73, 0x46, 0x05, 0x3a, 0x0f, 0x87, 0xd6, 0xc8, 0xf2, 0x6e, 0xbf, 0xc9, 0x08,
	0xba, 0x82, 0xc5, 0xe8, 0x47, 0x49, 0xb0, 0xf4, 0x43, 0x8c, 0x04, 0x75, 0x1e, 0x0d, 0xad, 0x51,
	0xc7, 0xdb, 0x86, 0x65, 0xae, 0x98, 0x71, 0x7f, 0x21, 0xa2, 0x40, 0xe7, 0x6a, 0xe9, 0x5c, 0x1b,
	0x20, 0x39, 0x81, 0xa7, 0xe5, 0x3d, 0x64, 0x8e, 0x14, 0xb3, 0x78, 0x95, 0x31, 0x1a, 0x3a, 0xa0,
	0xbc, 0x9f, 0x18, 0xe3, 0x25, 0xe2, 0xa4, 0x34, 0x91, 0x17, 0xaa, 0x71, 0xbe, 0xd1, 0xb0, 0x3d,
	0xb4, 0x46, 0xb6, 0xd7, 0x8a, 0xf2, 0x74, 0xa2, 0x65, 0x3c, 0x81, 0xa7, 0x31, 0xbd, 0xf1, 0x19,
	0xa7, 0x81, 0x60, 0xd7, 0xe8, 0x87, 0x45, 0xa6, 0x1a, 0xe2, 0xd8, 0x9a, 0x32, 0xa6, 0x37, 0x6f,
	0x8c, 0xed, 0x2b, 0x63, 0x22, 0x9f, 0x81, 0x53, 0x96, 0x11, 0x33, 0xce, 0xe2, 0x22, 0x5e, 0x6b,
	0xd4, 0x51, 0x61, 0x65, 0x99, 0x57, 0xda, 0x7c, 0x89, 0x78, 0x95, 0x53, 0xe1, 0x1e, 0x03, 0xf9,
	0x3e, 0x45, 0x6e, 0xda, 0xff, 0x5f, 0x93, 0x32, 0x81, 0xa3, 0x0d, 0x6f, 0x39, 0x21, 0x0e, 0x3c,
	0x14, 0x37, 0xfe, 0x82, 0xe6, 0x0b, 0xe3, 0x5c, 0x7e, 0x12, 0x17, 0xec, 0xa4, 0x10, 0x69, 0x21,
	0x7c, 0xc6, 0x43, 0xbc, 0x51, 0xd3, 0xd2, 0xf1, 0x36, 0x30, 0xf7, 0x18, 0xfa, 0x1e, 0xce, 0x59,
	0x2e, 0x30, 0x9b, 0xd0, 0x55, 0x8c, 0x5c, 0x94, 0x35, 0x10, 0x68, 0x4e, 0xa3, 0x64, 0xaa, 0xa6,
	0xc9, 0xf6, 0xd4, 0xd9, 0xed, 0x43, 0x6f, 0xc7, 0x3b, 0x8d, 0x56, 0xee, 0x5f, 0x16, 0x10, 0x03,
	0x54, 0x26, 0x98, 0xbc, 0x0f, 0x76, 0xaa, 0xd1, 0x75, 0x7d, 0xb6, 0xd7, 0x36, 0xd8, 0x37, 0xb2,
	0xc6, 0x57, 0x70, 0x58, 0xba, 0xe4, 0x18, 0x64, 0x28, 0x54, 0x95, 0xb6, 0xd7, 0x31, 0xe8, 0x5b,
	0x05, 0xca, 0xd1, 0x0c, 0x31, 0x17, 0x8c, 0xeb, 0x4e, 0xe8, 0x9a, 0xaa, 0x10, 0xf9, 0x04, 0x7a,
	0x8c, 0x07, 0x49, 0xcc, 0xf8, 0xdc, 0xa7, 0x71, 0x52, 0x70, 0xa1, 0xd5, 0xd7, 0xc3, 0x4e, 0x4a,
	0xdb, 0x99, 0x32, 0x49, 0xe9, 0x65, 0x44, 0x52, 0x88, 0x79, 0xb2, 0x1d, 0xb1, 0xaf, 0x23, 0x4a,
	0xdb, 0x3a, 0xc2, 0x7d, 0x09, 0xad, 0xd7, 0x3c, 0xc8, 0x56, 0xa9, 0xc0, 0x50, 0xea, 0x13, 0x52,
	0x41, 0xcd, 0xa5, 0xd4, 0xd9, 0xf5, 0xe0, 0xe0, 0x2d, 0x9b, 0xf3, 0x7a, 0xeb, 0xd6, 0xbb, 0xb5,
	0x6f, 0xdf, 0xed, 0x73, 0x68, 0xe5, 0x6c, 0xce, 0xa9, 0x28, 0x32, 0x34, 0x57, 0x5b, 0x03, 0xee,
	0xef, 0x0d, 0xe8, 0x5d, 0x2c, 0x30, 0x58, 0x9a, 0xae, 0xe7, 0x65, 0x83, 0x5e, 0xc1, 0x21, 0xea,
	0x6a, 0xfc, 0xca, 0xb0, 0xd8, 0x5e, 0xc7, 0xa0, 0x66, 0x9c, 0x7f, 0x80, 0xce, 0x8c, 0x2e, 0xd1,
	0x37, 0xf3, 0x97, 0x3b, 0x7b, 0xc3, 0xc6, 0xa8, 0x7d, 0x72, 0x3c, 0x56, 0xcb, 0xab, 0x8e, 0x79,
	0x7c, 0x49, 0x97, 0x58, 0x62, 0xaf, 0xb9, 0xc8, 0x56, 0x9e, 0x3d, 0xab, 0x40, 0xe4, 0x57, 0xe8,
	0xbf, 0xa3, 0x4c, 0x48, 0xe1, 0x82, 0x28, 0xc9, 0x2b, 0xdc, 0x0d, 0xc5, 0xfd, 0xe9, 0xbf, 0x70,
	0xff, 0xac, 0x03, 0x2f, 0x64, 0xdc, 0x66, 0x8e, 0xde, 0xbb, 0x1a, 0xd3, 0xe0, 0x4b, 0x78, 0xbc,
	0x53, 0x0e, 0x39, 0x82, 0xc6, 0xfa, 0x71, 0xc8, 0x23, 0xe9, 0xc1, 0xfe, 0x35, 0x8d, 0x0a, 0x54,
	0xd2, 0x36, 0x3d, 0xfd, 0xf1, 0xc5, 0xde, 0xe7, 0xd6, 0xe0, 0x6b, 0x78, 0x76, 0x67, 0xce, 0xfb,
	0x10, 0xb9, 0x7f, 0xee, 0x01, 0xd9, 0xba, 0x92, 0x7c, 0x7f, 0xbf, 0xc0, 0x63, 0x9e, 0x08, 0x7f,
	0x53, 0x63, 0x4b, 0xe9, 0xf0, 0x71, 0xad, 0x0e, 0x69, 0xb4, 0x1a, 0x7f, 0x97, 0x88, 0x5d, 0x91,
	0xbb, 0x7c, 0x13, 0x25, 0x3f, 0x42, 0x57, 0xe9, 0x1b, 0xfe, 0xbf, 0xe6, 0x49, 0x62, 0x75, 0xc7,
	0x70, 0x93, 0xf7, 0x30, 0xd8, 0x00, 0x07, 0xe7, 0xd0, 0xab, 0xcb, 0x7f, 0x2f, 0x55, 0xcf, 0xe0,
	0x49, 0x4d, 0xaa, 0xfb, 0x50, 0x9c, 0xfc, 0xb1, 0x07, 0x1d, 0x13, 0x2d, 0x97, 0x1a, 0x66, 0xe4,
	0x27, 0x29, 0xf0, 0xf6, 0xef, 0x20, 0x79, 0x59, 0x5e, 0xf6, 0x8e, 0xdf, 0xd5, 0xc1, 0x8b, 0xbb,
	0x1d, 0xe4, 0x72, 0x7a, 0x40, 0xce, 0xa0, 0x5d, 0x59, 0x9b, 0xc4, 0xd1, 0xfe, 0xbb, 0x7b, 0x77,
	0xd0, 0xaf, 0xb1, 0x68, 0x8a, 0x2b, 0xe8, 0x6e, 0x6d, 0x3e, 0xf2, 0x5c, 0x3b, 0xd7, 0xaf, 0xcf,
	0xc1, 0xe0, 0x0e, 0xab, 0xa6, 0x3b, 0x95, 0x57, 0xaf, 0x34, 0x8f, 0x74, 0xb5, 0xfb, 0xed, 0x7a,
	0x19, 0x6c, 0x03, 0xee, 0x83, 0xf3, 0x0f, 0xa0, 0xc7, 0x92, 0xf1, 0x3c, 0x4b, 0x03, 0x6d, 0xcb,
	0x31, 0xbb, 0x66, 0x01, 0x9e, 0xb7, 0xbe, 0xcd, 0xd3, 0x70, 0x22, 0xff, 0x85, 0x4c, 0xac, 0xe9,
	0x81, 0xfa, 0x3b, 0x72, 0xfa, 0x4f, 0x00, 0x00, 0x00, 0xff, 0xff, 0x6e, 0x2c, 0x67, 0xf7, 0x9c,
	0x08, 0x00, 0x00,
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConnInterface

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion6

// ChannelOpenerClient is the client API for ChannelOpener service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type ChannelOpenerClient interface {
	ChannelInformation(ctx context.Context, in *ChannelInformationRequest, opts ...grpc.CallOption) (*ChannelInformationReply, error)
	OpenChannel(ctx context.Context, in *OpenChannelRequest, opts ...grpc.CallOption) (*OpenChannelReply, error)
	RegisterPayment(ctx context.Context, in *RegisterPaymentRequest, opts ...grpc.CallOption) (*RegisterPaymentReply, error)
	CheckChannels(ctx context.Context, in *Encrypted, opts ...grpc.CallOption) (*Encrypted, error)
}

type channelOpenerClient struct {
	cc grpc.ClientConnInterface
}

func NewChannelOpenerClient(cc grpc.ClientConnInterface) ChannelOpenerClient {
	return &channelOpenerClient{cc}
}

func (c *channelOpenerClient) ChannelInformation(ctx context.Context, in *ChannelInformationRequest, opts ...grpc.CallOption) (*ChannelInformationReply, error) {
	out := new(ChannelInformationReply)
	err := c.cc.Invoke(ctx, "/lspd.ChannelOpener/ChannelInformation", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelOpenerClient) OpenChannel(ctx context.Context, in *OpenChannelRequest, opts ...grpc.CallOption) (*OpenChannelReply, error) {
	out := new(OpenChannelReply)
	err := c.cc.Invoke(ctx, "/lspd.ChannelOpener/OpenChannel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelOpenerClient) RegisterPayment(ctx context.Context, in *RegisterPaymentRequest, opts ...grpc.CallOption) (*RegisterPaymentReply, error) {
	out := new(RegisterPaymentReply)
	err := c.cc.Invoke(ctx, "/lspd.ChannelOpener/RegisterPayment", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelOpenerClient) CheckChannels(ctx context.Context, in *Encrypted, opts ...grpc.CallOption) (*Encrypted, error) {
	out := new(Encrypted)
	err := c.cc.Invoke(ctx, "/lspd.ChannelOpener/CheckChannels", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ChannelOpenerServer is the server API for ChannelOpener service.
type ChannelOpenerServer interface {
	ChannelInformation(context.Context, *ChannelInformationRequest) (*ChannelInformationReply, error)
	OpenChannel(context.Context, *OpenChannelRequest) (*OpenChannelReply, error)
	RegisterPayment(context.Context, *RegisterPaymentRequest) (*RegisterPaymentReply, error)
	CheckChannels(context.Context, *Encrypted) (*Encrypted, error)
}

// UnimplementedChannelOpenerServer can be embedded to have forward compatible implementations.
type UnimplementedChannelOpenerServer struct {
}

func (*UnimplementedChannelOpenerServer) ChannelInformation(ctx context.Context, req *ChannelInformationRequest) (*ChannelInformationReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ChannelInformation not implemented")
}
func (*UnimplementedChannelOpenerServer) OpenChannel(ctx context.Context, req *OpenChannelRequest) (*OpenChannelReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method OpenChannel not implemented")
}
func (*UnimplementedChannelOpenerServer) RegisterPayment(ctx context.Context, req *RegisterPaymentRequest) (*RegisterPaymentReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RegisterPayment not implemented")
}
func (*UnimplementedChannelOpenerServer) CheckChannels(ctx context.Context, req *Encrypted) (*Encrypted, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CheckChannels not implemented")
}

func RegisterChannelOpenerServer(s *grpc.Server, srv ChannelOpenerServer) {
	s.RegisterService(&_ChannelOpener_serviceDesc, srv)
}

func _ChannelOpener_ChannelInformation_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ChannelInformationRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelOpenerServer).ChannelInformation(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lspd.ChannelOpener/ChannelInformation",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelOpenerServer).ChannelInformation(ctx, req.(*ChannelInformationRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ChannelOpener_OpenChannel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(OpenChannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelOpenerServer).OpenChannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lspd.ChannelOpener/OpenChannel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelOpenerServer).OpenChannel(ctx, req.(*OpenChannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ChannelOpener_RegisterPayment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RegisterPaymentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelOpenerServer).RegisterPayment(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lspd.ChannelOpener/RegisterPayment",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelOpenerServer).RegisterPayment(ctx, req.(*RegisterPaymentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ChannelOpener_CheckChannels_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Encrypted)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelOpenerServer).CheckChannels(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lspd.ChannelOpener/CheckChannels",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelOpenerServer).CheckChannels(ctx, req.(*Encrypted))
	}
	return interceptor(ctx, in, info, handler)
}

var _ChannelOpener_serviceDesc = grpc.ServiceDesc{
	ServiceName: "lspd.ChannelOpener",
	HandlerType: (*ChannelOpenerServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ChannelInformation",
			Handler:    _ChannelOpener_ChannelInformation_Handler,
		},
		{
			MethodName: "OpenChannel",
			Handler:    _ChannelOpener_OpenChannel_Handler,
		},
		{
			MethodName: "RegisterPayment",
			Handler:    _ChannelOpener_RegisterPayment_Handler,
		},
		{
			MethodName: "CheckChannels",
			Handler:    _ChannelOpener_CheckChannels_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "lspd.proto",
}
