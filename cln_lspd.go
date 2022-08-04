package main

import (
	"context"
	"encoding/hex"
	"fmt"
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
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/niftynei/glightning/glightning"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type server_c struct{}

var (
	clnPrivateKeyBytes []byte
	clnPublicKeyBytes  []byte
	minConf            uint16
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

//C-lightning plugin functions
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
	clientcln.StartUp(config.RpcFile, config.LightningDir)
	log.Printf("successfull clientcln.StartUp")
}

func clnOpenChannel(clientcln *glightning.Lightning, paymentHash, destination string, incomingAmountMsat int64) ([]byte, uint32, error) {

	capacity := incomingAmountMsat/1000 + additionalChannelCapacity
	if capacity == publicChannelAmount {
		capacity++
	}
	minConf = 0 //need to be updated with mindepth for zeroconf

	//open private channel
	//todo-glightning need to update the code with accepting parameter for zero-conf
	channelPoint, err := clientcln.FundChannelExt(destination, glightning.NewSat(int(capacity)), nil, false, &minConf, nil)
	if err != nil {
		log.Printf("clientcln.OpenChannelSync(%v, %v) error: %v", destination, capacity, err)
		return nil, 0, err
	}
	FundingTxId, err := hex.DecodeString(channelPoint.FundingTxId)
	if err != nil {
		log.Printf("hex.DecodeString(channelPoint.FundingTxId) error: %v", err)
		return nil, 0, err
	}
	return FundingTxId, uint32(0), err
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

	paymentHashBytes, err := hex.DecodeString(event.Htlc.PaymentHash)
	if err != nil {
		log.Printf("hex.DecodeString(%v) error: %v", event.Htlc.PaymentHash, err)
		return event.Continue(), fmt.Errorf("hex.DecodeString(%v) error: %w", event.Htlc.PaymentHash, err)
	}

	paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, fundingTxID, fundingTxOutnum, err := paymentInfo(paymentHashBytes)
	if err != nil {
		log.Printf("paymentInfo(%v)\nfundingTxOutnum: %v\n error: %v", event.Htlc.PaymentHash, fundingTxOutnum, err)
	}

	if paymentSecret != nil {
		if fundingTxID == nil {
			if hex.EncodeToString(paymentHash) == event.Htlc.PaymentHash {

				fundingTxID, fundingTxOutnum, err = clnOpenChannel(clientcln, hex.EncodeToString(paymentHash), hex.EncodeToString(destination), incomingAmountMsat)
				log.Printf("openclnOpenChannelChannel(%v, %v) err: %v", destination, incomingAmountMsat, err)
				if err != nil {
					log.Printf(" clnOpenChannel error: %v", err)
				}

			} else { //probing
				failureCode := "TEMPORARY_CHANNEL_FAILURE"
				if err = clnIsConnected(clientcln, hex.EncodeToString(destination)); err == nil {
					failureCode = "INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS"
				}
				log.Printf(" failureCode %v , Error %v", failureCode, err)
				return event.Continue(), err
			}

		}
		var h chainhash.Hash
		err = h.SetBytes(fundingTxID)
		if err != nil {
			log.Printf("h.SetBytes(%v) error: %v", fundingTxID, err)
		}
		channelPoint := wire.NewOutPoint(&h, fundingTxOutnum).String()
		return clnResumeOrCancel(clientcln, hex.EncodeToString(destination), hex.EncodeToString(fundingTxID), hex.EncodeToString(paymentHash), uint64(outgoingAmountMsat), 99, event, channelPoint)

	}

	return event.Continue(), err
}

func clnGetChannel(clientcln *glightning.Lightning, destination string, fundingTxID string) string {
	obj, err := clientcln.ListFunds()
	if err != nil {
		log.Printf("clientcln.ListChannels(%v) error: %v", destination, err)
		return ""
	}
	for _, c := range obj.Channels {
		log.Printf("getChannel(%v): %v", destination, c.ShortChannelId)
		if c.FundingTxId == fundingTxID && c.Connected {
			chanid := c.ShortChannelId
			return chanid
		}
	}
	log.Printf("No channel found: getChannel(%v)", destination)
	return ""
}
func clnResumeOrCancel(clientcln *glightning.Lightning, destination string, fundingTxID string, paymentHash string, outgoingAmountMsat uint64, riskfactor float32, event *glightning.HtlcAcceptedEvent, channelPoint string) (*glightning.HtlcAcceptedResponse, error) {
	deadline := time.Now().Add(10 * time.Second)

	for {
		shortChanID := clnGetChannel(clientcln, destination, fundingTxID)

		if shortChanID != "" {
			log.Printf("channel opended successfully chanID: %v)", shortChanID)
			dest, err := hex.DecodeString(destination)
			if err != nil {
				log.Printf("hex.DecodeString(destination) error: %v", err)
				break
			}
			fields := strings.Split(shortChanID, "x")
			var chanID lnwire.ShortChannelID
			var temp []int64
			for i, field := range fields {
				temp[i], _ = strconv.ParseInt(field, 10, 64)
			}
			chanID = lnwire.ShortChannelID{BlockHeight: uint32(temp[0]), TxIndex: uint32(temp[1]), TxPosition: uint16(temp[2])}
			// we don't get short channel id until 1 conf
			err = insertChannel(chanID.ToUint64(), channelPoint, dest, time.Now())
			if err != nil {
				log.Printf("insertChannel error: %v", err)
			}

			break
		}

		log.Printf("waiting for channel to get opened.... %v\n", destination)
		if time.Now().After(deadline) {
			log.Printf("Stop retrying getChannel(%v, %v)", destination, fundingTxID)
			break
		}
		time.Sleep(10 * time.Second)
	}
	log.Printf("forwarding htlc to the destination node and a new private channel was opened")
	return event.Continue(), nil
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
