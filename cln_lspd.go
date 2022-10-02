package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	ecies "github.com/ecies/go/v2"
	"github.com/golang/protobuf/proto"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/tlv"
	"github.com/niftynei/glightning/glightning"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type server_c struct{}

const (
	TEMPORARY_CHANNEL_FAILURE            = uint16(0x1007)
	INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS = uint16(0x4015)
)

var (
	clnPrivateKeyBytes []byte
	clnPublicKeyBytes  []byte
	clientcln          *glightning.Lightning
	wg                 sync.WaitGroup
)

//grpc clightning server

func (s *server_c) ChannelInformation(ctx context.Context, in *lspdrpc.ChannelInformationRequest) (*lspdrpc.ChannelInformationReply, error) {
	return &lspdrpc.ChannelInformationReply{
		Name:                  os.Getenv("CLN_NODE_NAME"),
		Pubkey:                os.Getenv("CLN_NODE_PUBKEY"),
		Host:                  os.Getenv("CLN_NODE_HOST"),
		ChannelCapacity:       publicChannelAmount,
		TargetConf:            targetConf,
		MinHtlcMsat:           minHtlcMsat,
		BaseFeeMsat:           baseFeeMsat,
		FeeRate:               feeRate,
		TimeLockDelta:         timeLockDelta,
		ChannelFeePermyriad:   channelFeePermyriad,
		ChannelMinimumFeeMsat: channelMinimumFeeMsat,
		LspPubkey:             clnPublicKeyBytes,
		MaxInactiveDuration:   maxInactiveDuration,
	}, nil
}

func (s *server_c) RegisterPayment(ctx context.Context, in *lspdrpc.RegisterPaymentRequest) (*lspdrpc.RegisterPaymentReply, error) {
	data, err := ecies.Decrypt(ecies.NewPrivateKeyFromBytes(clnPrivateKeyBytes), in.Blob)
	if err != nil {
		log.Printf("cies.Decrypt(%x) error: %v", in.Blob, err)
		priv, _ := btcec.PrivKeyFromBytes(btcec.S256(), clnPrivateKeyBytes)
		data, err = btcec.Decrypt(priv, in.Blob)
		if err != nil {
			log.Printf("btcec.Decrypt(%x) error: %v", in.Blob, err)
			return nil, fmt.Errorf("btcec.Decrypt(%x) error: %w", in.Blob, err)
		}
	}
	var pi lspdrpc.PaymentInformation
	err = proto.Unmarshal(data, &pi)
	if err != nil {
		log.Printf("proto.Unmarshal(%x) error: %v", data, err)
		return nil, fmt.Errorf("proto.Unmarshal(%x) error: %w", data, err)
	}
	log.Printf("RegisterPayment - Destination: %x, pi.PaymentHash: %x, pi.PaymentSecret: %x, pi.IncomingAmountMsat: %v, pi.OutgoingAmountMsat: %v",
		pi.Destination, pi.PaymentHash, pi.PaymentSecret, pi.IncomingAmountMsat, pi.OutgoingAmountMsat)
	err = checkPayment(pi.IncomingAmountMsat, pi.OutgoingAmountMsat)
	if err != nil {
		log.Printf("checkPayment(%v, %v) error: %v", pi.IncomingAmountMsat, pi.OutgoingAmountMsat, err)
		return nil, fmt.Errorf("checkPayment(%v, %v) error: %v", pi.IncomingAmountMsat, pi.OutgoingAmountMsat, err)
	}
	err = registerPayment(pi.Destination, pi.PaymentHash, pi.PaymentSecret, pi.IncomingAmountMsat, pi.OutgoingAmountMsat)
	if err != nil {
		log.Printf("RegisterPayment() error: %v", err)
		return nil, fmt.Errorf("RegisterPayment() error: %w", err)
	}
	return &lspdrpc.RegisterPaymentReply{}, nil
}

func (s *server_c) OpenChannel(ctx context.Context, in *lspdrpc.OpenChannelRequest) (*lspdrpc.OpenChannelReply, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *server_c) CheckChannels(ctx context.Context, in *lspdrpc.Encrypted) (*lspdrpc.Encrypted, error) {

	return nil, fmt.Errorf("not implemented")
}

// C-lightning plugin functions
func StartPlugin() {
	//c-lightning plugin initiate
	plugin := glightning.NewPlugin(onInit)
	plugin.RegisterHooks(&glightning.Hooks{
		HtlcAccepted: OnHtlcAccepted,
	})

	err := plugin.Start(os.Stdin, os.Stdout)
	if err != nil {
		log.Printf("start plugin error: %v", err)
	}
}
func onInit(plugin *glightning.Plugin, options map[string]glightning.Option, config *glightning.Config) {
	log.Printf("successfully init'd! %v\n", config.RpcFile)

	//lightning server
	clientcln = glightning.NewLightning()
	clientcln.SetTimeout(60)
	clientcln.StartUp(config.RpcFile, config.LightningDir)
	log.Printf("successfull clientcln.StartUp")
}

func OnHtlcAccepted(event *glightning.HtlcAcceptedEvent) (*glightning.HtlcAcceptedResponse, error) {
	log.Printf("htlc_accepted called\n")
	onion := event.Onion

	log.Printf("htlc: %v\nchanID: %v\nincoming amount: %v\noutgoing amount: %v\nincoming expiry: %v\noutgoing expiry: %v\npaymentHash: %v\nonionBlob: %v\n\n",
		event.Htlc,
		onion.ShortChannelId,
		event.Htlc.AmountMilliSatoshi, //with fees
		onion.ForwardAmount,
		event.Htlc.CltvExpiryRelative,
		event.Htlc.CltvExpiry,
		event.Htlc.PaymentHash,
		onion,
	)

	// fail htlc in case payment hash is not valid.
	paymentHashBytes, err := hex.DecodeString(event.Htlc.PaymentHash)
	if err != nil {
		return failHtlc(event, fmt.Errorf("hex.DecodeString(%v) error: %w", event.Htlc.PaymentHash, err)), nil
	}

	// fail htlc in case fetching payment information fails.
	paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, fundingTxID, fundingTxOutnum, err := paymentInfo(paymentHashBytes)
	if err != nil {
		return failHtlc(event, fmt.Errorf("paymentInfo(%v)\nfundingTxOutnum: %v\n error: %v", event.Htlc.PaymentHash, fundingTxOutnum, err)), nil
	}

	// in case payment is not registered
	if paymentSecret == nil {
		return event.Continue(), nil
	}

	// if payment hash mismatch then it is probably probing
	if hex.EncodeToString(paymentHash) != event.Htlc.PaymentHash {
		failureCode := TEMPORARY_CHANNEL_FAILURE
		if err = clnIsConnected(clientcln, hex.EncodeToString(destination)); err == nil {
			failureCode = INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS
		}
		log.Printf(" failureCode %v , Error %v", failureCode, err)
		return event.Fail(failureCode), nil
	}

	// in case we don't have a funding Tx we need to open a channel
	if fundingTxID == nil {
		fundingTxID, fundingTxOutnum, err = clnOpenChannel(clientcln, hex.EncodeToString(paymentHash), hex.EncodeToString(destination), incomingAmountMsat)
		log.Printf("openclnOpenChannelChannel(%v, %v) err: %v", destination, incomingAmountMsat, err)
		if err != nil {
			return failHtlc(event, fmt.Errorf("clnOpenChannel error: %v", err)), nil
		}

		//database entry
		err = setFundingTx(paymentHash, fundingTxID, uint32(fundingTxOutnum))
		if err != nil {
			return failHtlc(event, fmt.Errorf("setFundingTx error: %v", err)), nil
		}
	}

	// If we got here then we have a funding tx, we need to poll untill channel is opened
	// and then forward the payment
	var h chainhash.Hash
	err = h.SetBytes(fundingTxID)
	if err != nil {
		return failHtlc(event, fmt.Errorf("h.SetBytes(%v) error: %v", fundingTxID, err)), nil
	}

	channelPoint := wire.NewOutPoint(&h, fundingTxOutnum).String()
	payload, err := clnResumeOrCancel(clientcln, hex.EncodeToString(destination), hex.EncodeToString(fundingTxID), hex.EncodeToString(paymentHash), uint64(outgoingAmountMsat), 99, event, channelPoint)
	if err != nil {
		return failHtlc(event, fmt.Errorf("clnResumeOrCancel %v", err)), nil
	}
	return event.ContinueWithPayload(payload), nil
}

func clnResumeOrCancel(clientcln *glightning.Lightning,
	destination string, fundingTxID string, paymentHash string,
	outgoingAmountMsat uint64, riskfactor float32, event *glightning.HtlcAcceptedEvent,
	channelPoint string) (string, error) {

	deadline := time.Now().Add(60 * time.Second)

	dest, err := hex.DecodeString(destination)
	if err != nil {
		return "", fmt.Errorf("hex.DecodeString(destination) error: %v", err)
	}

	for {
		initialChanID, confirmedChanID, channelFound := clnGetChannel(clientcln, destination, fundingTxID)

		if channelFound {
			log.Printf("channel opended successfully alias: %v, confirmed: %v", initialChanID, confirmedChanID)

			err = insertChannel(initialChanID, confirmedChanID, channelPoint, dest, time.Now())
			if err != nil {
				log.Printf("insertChannel error: %v", err)
				return "", fmt.Errorf("insertChannel error: %v", err)
			}

			channelID := confirmedChanID
			if channelID == 0 {
				channelID = initialChanID
			}
			//decoding and encoding onion with alias in type 6 record.
			newPayload, err := encodePayloadWithNextHop(event.Onion.Payload, channelID)
			if err != nil {
				return "", fmt.Errorf("encodePayloadWithNextHop error: %v", err)
			}

			log.Printf("forwarding htlc to the destination node and a new private channel was opened")
			return newPayload, nil
		}

		log.Printf("waiting for channel to get opened.... %v\n", destination)
		if time.Now().After(deadline) {
			log.Printf("Stop retrying getChannel(%v, %v)", destination, fundingTxID)
			break
		}
		time.Sleep(1 * time.Second)
	}
	log.Printf("Error: Channel failed to opened... timed out. ")
	return "", errors.New("error: channel failed to opened... timed out")
}

func clnGetChannel(clientcln *glightning.Lightning, destination string, fundingTxID string) (initialChanID, confirmedChanID uint64, channelFound bool) {

	obj, err := clientcln.GetPeer(destination)
	if err != nil {
		log.Printf("clientcln.ListChannels(%v) error: %v", destination, err)
		return 0, 0, false
	}

	for _, c := range obj.Channels {
		log.Printf("getChannel destination: %v, Short channel id: %v, local alias: %v , FundingTxID:%v, State:%v ", destination, c.ShortChannelId, c.Alias.Local, c.FundingTxId, c.State)
		if c.State == "CHANNELD_NORMAL" && c.FundingTxId == fundingTxID {
			confirmedChanID, err = parseShortChannelID(c.ShortChannelId)
			if err != nil {
				fmt.Printf("parseShortChannelID %v: %v", c.ShortChannelId, err)
				return 0, 0, false
			}
			initialChanID, err = parseShortChannelID(c.Alias.Local)
			if err != nil {
				fmt.Printf("parseShortChannelID %v: %v", c.Alias.Local, err)
				return 0, 0, false
			}
			return initialChanID, confirmedChanID, true
		}
	}

	log.Printf("No channel found: getChannel(%v)", destination)
	return 0, 0, false
}

func parseShortChannelID(idStr string) (uint64, error) {

	if idStr == "" {
		return 0, nil
	}

	fields := strings.Split(idStr, "x")
	if len(fields) != 3 {
		return 0, fmt.Errorf("invalid short channel id %v", idStr)
	}
	var blockHeight, txIndex, txPos int64
	var err error
	if blockHeight, err = strconv.ParseInt(fields[0], 10, 64); err != nil {
		return 0, fmt.Errorf("failed to parse block height %v", fields[0])
	}
	if txIndex, err = strconv.ParseInt(fields[1], 10, 64); err != nil {
		return 0, fmt.Errorf("failed to parse block height %v", fields[1])
	}
	if txPos, err = strconv.ParseInt(fields[2], 10, 64); err != nil {
		return 0, fmt.Errorf("failed to parse block height %v", fields[2])
	}

	return lnwire.ShortChannelID{BlockHeight: uint32(blockHeight), TxIndex: uint32(txIndex), TxPosition: uint16(txPos)}.ToUint64(), nil
}

func clnOpenChannel(clientcln *glightning.Lightning, paymentHash, destination string, incomingAmountMsat int64) ([]byte, uint32, error) {
	capacity := incomingAmountMsat/1000 + additionalChannelCapacity
	if capacity == publicChannelAmount {
		capacity++
	}

	//open private channel
	minDepth := uint16(0)
	channelPoint, err := clientcln.FundChannelExt(destination, glightning.NewSat(int(capacity)), &glightning.FeeRate{
		Directive: glightning.Slow,
	}, false, nil, nil, &minDepth, glightning.NewMsat(0))
	if err != nil {
		log.Printf("clientcln.OpenChannelSync(%v, %v) error: %v", destination, capacity, err)
		return nil, 0, err
	}
	FundingTxId, err := hex.DecodeString(channelPoint.FundingTxId)
	if err != nil {
		log.Printf("hex.DecodeString(channelPoint.FundingTxId) error: %v", err)
		return nil, 0, err
	}
	return FundingTxId, uint32(channelPoint.FundingTxOutputNum), err
}

func clnIsConnected(clientcln *glightning.Lightning, destination string) error {
	pubKey := destination
	peers, err := clientcln.ListPeers()
	if err != nil {
		return fmt.Errorf("clientcln.ListPeers() error: %v", err)
	}
	for _, peer := range peers {
		if pubKey == peer.Id {
			log.Printf("destination online: %v", destination)
			return nil
		}
	}
	log.Printf("destination offline: %v", destination)
	return fmt.Errorf("destination offline")
}

func failHtlc(event *glightning.HtlcAcceptedEvent, err error) *glightning.HtlcAcceptedResponse {
	log.Printf("failing htlc: %v", err)
	return event.Fail(4103)
}

func encodePayloadWithNextHop(payloadHex string, channelId uint64) (string, error) {
	payload, err := hex.DecodeString(payloadHex)
	if err != nil {
		log.Fatalf("failed to decode types %v", err)
	}
	bufReader := bytes.NewBuffer(payload)
	var b [8]byte
	varInt, err := sphinx.ReadVarInt(bufReader, &b)
	if err != nil {
		return "", fmt.Errorf("failed to read payload length %v: %v", payloadHex, err)
	}

	innerPayload := make([]byte, varInt)
	if _, err := io.ReadFull(bufReader, innerPayload[:]); err != nil {
		return "", fmt.Errorf("failed to decode payload %x: %v", innerPayload[:], err)
	}

	s, _ := tlv.NewStream()
	tlvMap, err := s.DecodeWithParsedTypes(bytes.NewReader(innerPayload))
	if err != nil {
		return "", fmt.Errorf("DecodeWithParsedTypes failed for %x: %v", innerPayload[:], err)
	}

	tt := record.NewNextHopIDRecord(&channelId)
	buf := bytes.NewBuffer([]byte{})
	if err := tt.Encode(buf); err != nil {
		return "", fmt.Errorf("failed to encode nexthop %x: %v", innerPayload[:], err)
	}

	uTlvMap := make(map[uint64][]byte)
	for t, b := range tlvMap {
		if t == record.NextHopOnionType {
			uTlvMap[uint64(t)] = buf.Bytes()
			continue
		}
		uTlvMap[uint64(t)] = b
	}
	tlvRecords := tlv.MapToRecords(uTlvMap)
	s, err = tlv.NewStream(tlvRecords...)
	if err != nil {
		return "", fmt.Errorf("tlv.NewStream(%x) error: %v", tlvRecords, err)
	}
	var newPayloadBuf bytes.Buffer
	err = s.Encode(&newPayloadBuf)
	if err != nil {
		return "", fmt.Errorf("encode error: %v", err)
	}
	return hex.EncodeToString(newPayloadBuf.Bytes()), nil
}

func run_cln() {
	//var wg sync.WaitGroup
	//c-lightning plugin started
	wg.Add(1)
	go StartPlugin()

	err := pgConnect()
	if err != nil {
		log.Fatalf("pgConnect() error: %v", err)
	}
	clnPrivateKeyBytes, err = hex.DecodeString(os.Getenv("LSPD_PRIVATE_KEY_CLN"))
	if err != nil {
		log.Fatalf("hex.DecodeString(os.Getenv(\"LSPD_PRIVATE_KEY_CLN\")=%v) error: %v", os.Getenv("LSPD_PRIVATE_KEY_CLN"), err)
	}
	_, clnPublicKey := btcec.PrivKeyFromBytes(btcec.S256(), clnPrivateKeyBytes)
	clnPublicKeyBytes = clnPublicKey.SerializeCompressed()

	//grpc server for clightning
	address := os.Getenv("LISTEN_ADDRESS_CLN")
	var lis net.Listener

	lis, err = net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc_middleware.WithUnaryServerChain(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			if md, ok := metadata.FromIncomingContext(ctx); ok {
				for _, auth := range md.Get("authorization") {
					if auth == "Bearer "+os.Getenv("CLN_TOKEN") {
						return handler(ctx, req)
					}
				}
			}
			return nil, status.Errorf(codes.PermissionDenied, "Not authorized")
		}),
	)
	lspdrpc.RegisterChannelOpenerServer(s, &server_c{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

	wg.Wait() //wait for greenlight intercept
}
