package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/golang/protobuf/proto"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/niftynei/glightning/glightning"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type server_c struct{}

var (
	cln_privateKey *btcec.PrivateKey
	cln_publicKey  *btcec.PublicKey
	minConf        uint16
	clientcln      *glightning.Lightning
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
		LspPubkey:             cln_publicKey.SerializeCompressed(),
		MaxInactiveDuration:   maxInactiveDuration,
	}, nil
}

func (s *server_c) RegisterPayment(ctx context.Context, in *lspdrpc.RegisterPaymentRequest) (*lspdrpc.RegisterPaymentReply, error) {
	data, err := btcec.Decrypt(cln_privateKey, in.Blob)
	if err != nil {
		log.Printf("btcec.Decrypt(%x) error: %v", in.Blob, err)
		return nil, fmt.Errorf("btcec.Decrypt(%x) error: %w", in.Blob, err)
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

func clnOpenChannel(clientcln *glightning.Lightning, paymentHash, destination string, incomingAmountMsat int64) (string, uint32, error) {

	//log.Printf("channel open entered")
	capacity := incomingAmountMsat/1000 + additionalChannelCapacity
	if capacity == publicChannelAmount {
		capacity++
	}
	//log.Printf("capacity-------%v", capacity)
	minConf = 1
	channelPoint, err := clientcln.FundChannelExt(destination, glightning.NewSat(int(capacity)), nil, true, &minConf, nil)
	//log.Printf("clientcln.FundChannelExt(destination %v, glightning.NewSat(int(capacity)) %v, nil, false, &minConf %v, nil)", destination, glightning.NewSat(int(capacity)), &minConf)
	if err != nil {
		log.Printf("clientcln.OpenChannelSync(%v, %v) error: %v", destination, capacity, err)
		return "", 0, err
	}
	//log.Printf("channel opened without errr")
	outputin, err := strconv.Atoi(channelPoint.FundingTxId)
	if err != nil {
		log.Printf("strconv.Atoi(channelPoint.FundingTxId) error: %v", err)
	}
	//database entry
	pyhash, _ := hex.DecodeString(paymentHash)
	fTxId, _ := hex.DecodeString(channelPoint.FundingTxId)
	err = setFundingTx(pyhash, fTxId, outputin)
	//log.Printf("channel open exited")
	return channelPoint.FundingTxId, uint32(outputin), err
}

func clnIsConnected(clientcln *glightning.Lightning, destination string) error {
	pubKey := destination
	peers, err := clientcln.ListPeers()
	if err != nil {
		//log.Printf("clientcln.ListPeers() error: %v", err)
		return fmt.Errorf("clientcln.ListPeers() error: %w", err)
	}
	for _, peer := range peers {
		if pubKey == peer.Id {
			log.Printf("destination online: %x", destination)
			return nil
		}
	}
	log.Printf("destination offline: %x", destination)
	return fmt.Errorf("destination offline")
}
func OnHtlcAccepted(event *glightning.HtlcAcceptedEvent) (*glightning.HtlcAcceptedResponse, error) {
	//log.Printf("htlc_accepted called\n")

	onion := event.Onion
	log.Printf("has perhop? %t", onion.PerHop != nil)
	log.Printf("type is %s", onion.Type)

	var on string
	if onion.PaymentSecret == "" {
		on = ""
	} else {
		on = "not "
	}
	log.Printf("payment secret is %sempty", on)

	if onion.TotalMilliSatoshi == "" {
		on = "empty"
	} else {
		on = onion.TotalMilliSatoshi
	}
	log.Printf("amount is %s", on)

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

	//paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, fundingTxID, fundingTxOutnum, err := paymentInfo([]byte(event.Htlc.PaymentHash))
	//if err != nil {
	//	log.Printf("paymentInfo(%x)\nfundingTxOutnum: %v\n error: %v", event.Htlc.PaymentHash, fundingTxOutnum, err)
	//}

	//testing code...
	milliSatoshiOutAmount := onion.ForwardAmount[:len(onion.ForwardAmount)-4]
	outgoingAmountMsat, _ := strconv.Atoi(milliSatoshiOutAmount)

	milliSatoshiTotalAmount := event.Htlc.AmountMilliSatoshi[:len(event.Htlc.AmountMilliSatoshi)-4]
	vr, err := strconv.Atoi(milliSatoshiTotalAmount)
	incomingAmountMsat := int64(vr)

	paymentHash := event.Htlc.PaymentHash
	paymentSecret := onion.PaymentSecret
	destination := "02c3144f582c9d170c7d8dfddafac6241a12b5af34a9f7317dce6978a663ff82c1"
	//incomingAmountMsat, err := strconv.ParseInt(onion.TotalMilliSatoshi, 10, 64)
	if err != nil {
		log.Printf("incomingAmountMsat error %v", err)
	}
	//outgoingAmountMsat := onion.ForwardAmount
	var fundingTxID string
	var fundingTxOutnum uint32
	//log.Printf("paymentHash:%x\npaymentSecret:%x\ndestination:%x\nincomingAmountMsat:%v\noutgoingAmountMsat:%v\n\n",
	//	paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat)
	log.Printf("paymentHash%v\n, paymentSecret%v\n, destination%v\n, incomingAmountMsat%v\n, outgoingAmountMsat%v\n, fundingTxID%v\n, fundingTxOutnum%v\n", paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, fundingTxID, fundingTxOutnum)

	/////////////////////testing code...end

	if paymentSecret != "" {

		if fundingTxID == "" {
			if paymentHash == event.Htlc.PaymentHash {

				fundingTxID, fundingTxOutnum, err = clnOpenChannel(clientcln, paymentHash, destination, incomingAmountMsat)
				log.Printf("openclnOpenChannelChannel(%x, %v) err: %v", destination, incomingAmountMsat, err)
				if err != nil {
					log.Printf(" clnOpenChannel error: %v", err)
				}
			} else { //probing
				failureCode := "TEMPORARY_CHANNEL_FAILURE"
				if err := clnIsConnected(clientcln, destination); err == nil {
					failureCode = "INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS"
				}
				log.Printf(" failureCode %v , Error %v", failureCode, err)
			}

			//var h chainhash.Hash
			//err = h.SetBytes(fundingTxID)
			//if err != nil {
			//	log.Printf("h.SetBytes(%x) error: %v", fundingTxID, err)
			//}
			//channelPoint := wire.NewOutPoint(&h, fundingTxOutnum).String()

			go clnResumeOrCancel(clientcln, destination, fundingTxID)

		}

	}

	return event.Continue(), nil
}

func clnGetChannel(clientcln *glightning.Lightning, destination string, fundingTxID string) string {
	obj, err := clientcln.ListFunds()
	if err != nil {
		log.Printf("clientcln.ListChannels(%x) error: %v", destination, err)
		return ""
	}
	for _, c := range obj.Channels {
		log.Printf("getChannel(%x): %v", destination, c.ShortChannelId)
		if c.FundingTxId == fundingTxID && c.Connected {
			chanid := c.ShortChannelId
			if err != nil {
				log.Printf("strconv.ParseUint(c.ShortChannelId, 10, 64) error: %v", err)
			}
			return chanid
		}
	}
	log.Printf("No channel found: getChannel(%x)", destination)
	return ""
}
func clnResumeOrCancel(clientcln *glightning.Lightning, destination string, fundingTxID string) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		chanID := clnGetChannel(clientcln, destination, fundingTxID)
		if chanID != "" {
			log.Printf("channel opended successfully chanID: %v)", chanID)
			//err := insertChannel(chanID, hex.EncodeToString(fundingTxID), destination, time.Now())
			//if err != nil {
			//		log.Printf("insertChannel error: %v", err)
			//	}
			return
		}
		log.Printf("getChannel(%x, %v) returns 0", destination, fundingTxID)
		if time.Now().After(deadline) {
			log.Printf("Stop retrying getChannel(%x, %v)", destination, fundingTxID)
			break
		}
		time.Sleep(1 * time.Second)
	}

}
func run_cln() {
	var wg sync.WaitGroup
	//c-lightning plugin started
	wg.Add(1)
	go StartPlugin()

	err := pgConnect()
	if err != nil {
		log.Fatalf("pgConnect() error: %v", err)
	}
	privateKeyBytes, err := hex.DecodeString(os.Getenv("LSPD_PRIVATE_KEY_CLN"))
	if err != nil {
		log.Fatalf("hex.DecodeString(os.Getenv(\"LSPD_PRIVATE_KEY_CLN\")=%v) error: %v", os.Getenv("LSPD_PRIVATE_KEY_CLN"), err)
	}
	cln_privateKey, cln_publicKey = btcec.PrivKeyFromBytes(btcec.S256(), privateKeyBytes)

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
