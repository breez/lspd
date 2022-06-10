package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/golang/protobuf/proto"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/caddyserver/certmagic"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/niftynei/glightning/glightning"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	publicChannelAmount       = 1_000_183
	targetConf                = 6
	minHtlcMsat               = 600
	baseFeeMsat               = 1000
	feeRate                   = 0.000001
	timeLockDelta             = 144
	channelFeePermyriad       = 40
	channelMinimumFeeMsat     = 2_000_000
	additionalChannelCapacity = 100_000
	maxInactiveDuration       = 45 * 24 * 3600
)

type server struct{}

var (
	client              lnrpc.LightningClient
	routerClient        routerrpc.RouterClient
	chainNotifierClient chainrpc.ChainNotifierClient
	openChannelReqGroup singleflight.Group
	privateKey          *btcec.PrivateKey
	publicKey           *btcec.PublicKey
	nodeName            = os.Getenv("NODE_NAME")
	nodePubkey          = os.Getenv("NODE_PUBKEY")
	lis                 net.Listener
	conn                *grpc.ClientConn
)

func (s *server) ChannelInformation(ctx context.Context, in *lspdrpc.ChannelInformationRequest) (*lspdrpc.ChannelInformationReply, error) {
	return &lspdrpc.ChannelInformationReply{
		Name:                  nodeName,
		Pubkey:                nodePubkey,
		Host:                  os.Getenv("NODE_HOST"),
		ChannelCapacity:       publicChannelAmount,
		TargetConf:            targetConf,
		MinHtlcMsat:           minHtlcMsat,
		BaseFeeMsat:           baseFeeMsat,
		FeeRate:               feeRate,
		TimeLockDelta:         timeLockDelta,
		ChannelFeePermyriad:   channelFeePermyriad,
		ChannelMinimumFeeMsat: channelMinimumFeeMsat,
		LspPubkey:             publicKey.SerializeCompressed(),
		MaxInactiveDuration:   maxInactiveDuration,
	}, nil
}

func (s *server) RegisterPayment(ctx context.Context, in *lspdrpc.RegisterPaymentRequest) (*lspdrpc.RegisterPaymentReply, error) {
	data, err := btcec.Decrypt(privateKey, in.Blob)
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

func (s *server) OpenChannel(ctx context.Context, in *lspdrpc.OpenChannelRequest) (*lspdrpc.OpenChannelReply, error) {
	r, err, _ := openChannelReqGroup.Do(in.Pubkey, func() (interface{}, error) {
		clientCtx := metadata.AppendToOutgoingContext(context.Background(), "macaroon", os.Getenv("LND_MACAROON_HEX"))
		nodeChannels, err := getNodeChannels(in.Pubkey)
		if err != nil {
			return nil, err
		}
		pendingChannels, err := getPendingNodeChannels(in.Pubkey)
		if err != nil {
			return nil, err
		}
		channelAmount, err := strconv.ParseInt(os.Getenv("CHANNEL_AMOUNT"), 0, 64)
		if err != nil || channelAmount <= 0 {
			channelAmount = publicChannelAmount
		}
		log.Printf("os.Getenv(\"CHANNEL_AMOUNT\"): %v, channelAmount: %v, publicChannelAmount: %v, err: %v",
			os.Getenv("CHANNEL_AMOUNT"), channelAmount, publicChannelAmount, err)
		isPrivate, err := strconv.ParseBool(os.Getenv("CHANNEL_PRIVATE"))
		if err != nil {
			isPrivate = false
		}
		log.Printf("os.Getenv(\"CHANNEL_PRIVATE\"): %v, isPrivate: %v, err: %v",
			os.Getenv("CHANNEL_PRIVATE"), isPrivate, err)
		var txidStr string
		var outputIndex uint32
		if len(nodeChannels) == 0 && len(pendingChannels) == 0 {
			response, err := client.OpenChannelSync(clientCtx, &lnrpc.OpenChannelRequest{
				LocalFundingAmount: channelAmount,
				NodePubkeyString:   in.Pubkey,
				PushSat:            0,
				TargetConf:         targetConf,
				MinHtlcMsat:        minHtlcMsat,
				Private:            isPrivate,
			})
			log.Printf("Response from OpenChannel: %#v (TX: %v)", response, hex.EncodeToString(response.GetFundingTxidBytes()))

			if err != nil {
				log.Printf("Error in OpenChannel: %v", err)
				return nil, err
			}

			txid, _ := chainhash.NewHash(response.GetFundingTxidBytes())
			outputIndex = response.GetOutputIndex()
			// don't fail the request in case we can't format the channel id from
			// some reason...
			if txid != nil {
				txidStr = txid.String()
			}
		}
		return &lspdrpc.OpenChannelReply{TxHash: txidStr, OutputIndex: outputIndex}, nil
	})

	if err != nil {
		return nil, err
	}
	return r.(*lspdrpc.OpenChannelReply), err
}

func getSignedEncryptedData(in *lspdrpc.Encrypted) (string, []byte, error) {
	signedBlob, err := btcec.Decrypt(privateKey, in.Data)
	if err != nil {
		log.Printf("btcec.Decrypt(%x) error: %v", in.Data, err)
		return "", nil, fmt.Errorf("btcec.Decrypt(%x) error: %w", in.Data, err)
	}
	var signed lspdrpc.Signed
	err = proto.Unmarshal(signedBlob, &signed)
	if err != nil {
		log.Printf("proto.Unmarshal(%x) error: %v", signedBlob, err)
		return "", nil, fmt.Errorf("proto.Unmarshal(%x) error: %w", signedBlob, err)
	}
	pubkey, err := btcec.ParsePubKey(signed.Pubkey, btcec.S256())
	if err != nil {
		log.Printf("unable to parse pubkey: %v", err)
		return "", nil, fmt.Errorf("unable to parse pubkey: %w", err)
	}
	wireSig, err := lnwire.NewSigFromRawSignature(signed.Signature)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode signature: %v", err)
	}
	sig, err := wireSig.ToSignature()
	if err != nil {
		return "", nil, fmt.Errorf("failed to convert from wire format: %v",
			err)
	}
	// The signature is over the sha256 hash of the message.
	digest := chainhash.HashB(signed.Data)
	if !sig.Verify(digest, pubkey) {
		return "", nil, fmt.Errorf("invalid signature")
	}
	return hex.EncodeToString(signed.Pubkey), signed.Data, nil
}

func (s *server) CheckChannels(ctx context.Context, in *lspdrpc.Encrypted) (*lspdrpc.Encrypted, error) {
	nodeID, data, err := getSignedEncryptedData(in)
	if err != nil {
		log.Printf("getSignedEncryptedData error: %v", err)
		return nil, fmt.Errorf("getSignedEncryptedData error: %v", err)
	}
	var checkChannelsRequest lspdrpc.CheckChannelsRequest
	err = proto.Unmarshal(data, &checkChannelsRequest)
	if err != nil {
		log.Printf("proto.Unmarshal(%x) error: %v", data, err)
		return nil, fmt.Errorf("proto.Unmarshal(%x) error: %w", data, err)
	}
	notFakeChannels, err := getNotFakeChannels(nodeID, checkChannelsRequest.FakeChannels)
	if err != nil {
		log.Printf("getNotFakeChannels(%v) error: %v", checkChannelsRequest.FakeChannels, err)
		return nil, fmt.Errorf("getNotFakeChannels(%v) error: %w", checkChannelsRequest.FakeChannels, err)
	}
	closedChannels, err := getClosedChannels(nodeID, checkChannelsRequest.WaitingCloseChannels)
	if err != nil {
		log.Printf("getNotFakeChannels(%v) error: %v", checkChannelsRequest.FakeChannels, err)
		return nil, fmt.Errorf("getNotFakeChannels(%v) error: %w", checkChannelsRequest.FakeChannels, err)
	}
	checkChannelsReply := lspdrpc.CheckChannelsReply{
		NotFakeChannels: notFakeChannels,
		ClosedChannels:  closedChannels,
	}
	dataReply, err := proto.Marshal(&checkChannelsReply)
	if err != nil {
		log.Printf("proto.Marshall() error: %v", err)
		return nil, fmt.Errorf("proto.Marshal() error: %w", err)
	}
	pubkey, err := btcec.ParsePubKey(checkChannelsRequest.EncryptPubkey, btcec.S256())
	if err != nil {
		log.Printf("unable to parse pubkey: %v", err)
		return nil, fmt.Errorf("unable to parse pubkey: %w", err)
	}
	encrypted, err := btcec.Encrypt(pubkey, dataReply)
	if err != nil {
		log.Printf("btcec.Encrypt() error: %v", err)
		return nil, fmt.Errorf("btcec.Encrypt() error: %w", err)
	}
	return &lspdrpc.Encrypted{Data: encrypted}, nil
}

func getNotFakeChannels(nodeID string, channelPoints map[string]uint64) (map[string]uint64, error) {
	r := make(map[string]uint64)
	if len(channelPoints) == 0 {
		return r, nil
	}
	channels, err := confirmedChannels(nodeID)
	if err != nil {
		return nil, err
	}
	for channelPoint, chanID := range channels {
		if _, ok := channelPoints[channelPoint]; ok {
			r[channelPoint] = chanID
		}
	}
	return r, nil
}

func getClosedChannels(nodeID string, channelPoints map[string]uint64) (map[string]uint64, error) {
	r := make(map[string]uint64)
	if len(channelPoints) == 0 {
		return r, nil
	}
	waitingCloseChannels, err := getWaitingCloseChannels(nodeID)
	if err != nil {
		return nil, err
	}
	wcc := make(map[string]struct{})
	for _, c := range waitingCloseChannels {
		wcc[c.Channel.ChannelPoint] = struct{}{}
	}
	for c, h := range channelPoints {
		if _, ok := wcc[c]; !ok {
			r[c] = h
		}
	}
	return r, nil
}

func getWaitingCloseChannels(nodeID string) ([]*lnrpc.PendingChannelsResponse_WaitingCloseChannel, error) {
	clientCtx := metadata.AppendToOutgoingContext(context.Background(), "macaroon", os.Getenv("LND_MACAROON_HEX"))
	pendingResponse, err := client.PendingChannels(clientCtx, &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return nil, err
	}
	var waitingCloseChannels []*lnrpc.PendingChannelsResponse_WaitingCloseChannel
	for _, p := range pendingResponse.WaitingCloseChannels {
		if p.Channel.RemoteNodePub == nodeID {
			waitingCloseChannels = append(waitingCloseChannels, p)
		}
	}
	return waitingCloseChannels, nil
}

func getNodeChannels(nodeID string) ([]*lnrpc.Channel, error) {
	clientCtx := metadata.AppendToOutgoingContext(context.Background(), "macaroon", os.Getenv("LND_MACAROON_HEX"))
	listResponse, err := client.ListChannels(clientCtx, &lnrpc.ListChannelsRequest{})
	if err != nil {
		return nil, err
	}
	var nodeChannels []*lnrpc.Channel
	for _, channel := range listResponse.Channels {
		if channel.RemotePubkey == nodeID {
			nodeChannels = append(nodeChannels, channel)
		}
	}
	return nodeChannels, nil
}

func getPendingNodeChannels(nodeID string) ([]*lnrpc.PendingChannelsResponse_PendingOpenChannel, error) {
	clientCtx := metadata.AppendToOutgoingContext(context.Background(), "macaroon", os.Getenv("LND_MACAROON_HEX"))
	pendingResponse, err := client.PendingChannels(clientCtx, &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return nil, err
	}
	var pendingChannels []*lnrpc.PendingChannelsResponse_PendingOpenChannel
	for _, p := range pendingResponse.PendingOpenChannels {
		if p.Channel.RemoteNodePub == nodeID {
			pendingChannels = append(pendingChannels, p)
		}
	}
	return pendingChannels, nil
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
	//return nil
}
func onInit(plugin *glightning.Plugin, options map[string]glightning.Option, config *glightning.Config) {
	log.Printf("successfully init'd! %s\n", config.RpcFile)
}

func OnHtlcAccepted(event *glightning.HtlcAcceptedEvent) (*glightning.HtlcAcceptedResponse, error) {
	log.Printf("htlc_accepted called\n")

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

	return event.Continue(), nil
}
func initLspd() string {
	if len(os.Args) > 1 && os.Args[1] == "genkey" {
		p, err := btcec.NewPrivateKey(btcec.S256())
		if err != nil {
			return "btcec.NewPrivateKey() error:" + err.Error()
		}
		fmt.Printf("LSPD_PRIVATE_KEY=\"%x\"\n", p.Serialize())
		return "Keys generated"
	} else {

		err := pgConnect()
		if err != nil {
			return "pgConnect() error: " + err.Error()
		}

		privateKeyBytes, err := hex.DecodeString(os.Getenv("LSPD_PRIVATE_KEY"))
		if err != nil {
			return "hex.DecodeString(os.Getenv(LSPD_PRIVATE_KEY)= " + os.Getenv("LSPD_PRIVATE_KEY") + "error: " + err.Error()
		}
		privateKey, publicKey = btcec.PrivKeyFromBytes(btcec.S256(), privateKeyBytes)

		certmagicDomain := os.Getenv("CERTMAGIC_DOMAIN")
		address := os.Getenv("LISTEN_ADDRESS")

		if certmagicDomain == "" {
			var err error
			lis, err = net.Listen("tcp", address)
			if err != nil {
				return "failed to listen: " + err.Error()
			}
		} else {
			tlsConfig, err := certmagic.TLS([]string{certmagicDomain})
			if err != nil {
				return "failed to run certmagic: " + err.Error()
			}
			lis, err = tls.Listen("tcp", address, tlsConfig)
			if err != nil {
				return "failed to listen: " + err.Error()
			}
		}

		// Creds file to connect to LND gRPC
		cp := x509.NewCertPool()
		if !cp.AppendCertsFromPEM([]byte(strings.Replace(os.Getenv("LND_CERT"), "\\n", "\n", -1))) {
			return "credentials: failed to append certificates"
		}
		creds := credentials.NewClientTLSFromCert(cp, "")

		// Address of an LND instance
		conn, err = grpc.Dial(os.Getenv("LND_ADDRESS"), grpc.WithTransportCredentials(creds))
		if err != nil {
			return "Failed to connect to LND gRPC: " + err.Error()
		}

		client = lnrpc.NewLightningClient(conn)
		routerClient = routerrpc.NewRouterClient(conn)
		chainNotifierClient = chainrpc.NewChainNotifierClient(conn)

		clientCtx := metadata.AppendToOutgoingContext(context.Background(), "macaroon", os.Getenv("LND_MACAROON_HEX"))
		info, err := client.GetInfo(clientCtx, &lnrpc.GetInfoRequest{})
		if err != nil {
			return "client.GetInfo() error: " + err.Error()
		}
		if nodeName == "" {
			nodeName = info.Alias
		}
		if nodePubkey == "" {
			nodePubkey = info.IdentityPubkey
		}

	}
	return "nill"
}

func main() {
	var wg sync.WaitGroup
	//c-lightning plugin started
	wg.Add(1)
	go StartPlugin()
	//if err != nil {
	//	log.Fatalf("failed to serve: %v", err)
	//}

	state := initLspd()

	if state == "Keys generated" {
		log.Printf("Keys generated")
		os.Exit(0)
	}
	if state == "nill" {
		defer conn.Close()
		go intercept()

		go forwardingHistorySynchronize()
		go channelsSynchronize(chainNotifierClient)

		s := grpc.NewServer(
			grpc_middleware.WithUnaryServerChain(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
				if md, ok := metadata.FromIncomingContext(ctx); ok {
					for _, auth := range md.Get("authorization") {
						if auth == "Bearer "+os.Getenv("TOKEN") {
							return handler(ctx, req)
						}
					}
				}
				return nil, status.Errorf(codes.PermissionDenied, "Not authorized")
			}),
		)
		lspdrpc.RegisterChannelOpenerServer(s, &server{})
		if err := s.Serve(lis); err != nil {
			log.Printf("failed to serve: %v", err)
		}
	}
	log.Printf("failed to serve lnd instance: %v", state)
	wg.Wait()
}
