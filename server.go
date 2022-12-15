package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/breez/lspd/btceclegacy"
	lspdrpc "github.com/breez/lspd/rpc"
	ecies "github.com/ecies/go/v2"
	"github.com/golang/protobuf/proto"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/caddyserver/certmagic"
	"github.com/lightningnetwork/lnd/lnwire"
	"golang.org/x/sync/singleflight"
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

type server struct {
	lis net.Listener
	s   *grpc.Server
}

var (
	client              LightningClient
	openChannelReqGroup singleflight.Group
	privateKey          *btcec.PrivateKey
	publicKey           *btcec.PublicKey
	eciesPrivateKey     *ecies.PrivateKey
	eciesPublicKey      *ecies.PublicKey
	nodeName            = os.Getenv("NODE_NAME")
	nodePubkey          = os.Getenv("NODE_PUBKEY")
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
	data, err := ecies.Decrypt(eciesPrivateKey, in.Blob)
	if err != nil {
		log.Printf("ecies.Decrypt(%x) error: %v", in.Blob, err)
		data, err = btceclegacy.Decrypt(privateKey, in.Blob)
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

func (s *server) OpenChannel(ctx context.Context, in *lspdrpc.OpenChannelRequest) (*lspdrpc.OpenChannelReply, error) {
	r, err, _ := openChannelReqGroup.Do(in.Pubkey, func() (interface{}, error) {
		pubkey, err := hex.DecodeString(in.Pubkey)
		if err != nil {
			return nil, err
		}

		channelCount, err := client.GetNodeChannelCount(pubkey)
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
		var outPoint *wire.OutPoint
		if channelCount == 0 {
			outPoint, err = client.OpenChannel(&OpenChannelRequest{
				CapacitySat: uint64(channelAmount),
				Destination: pubkey,
				TargetConf:  targetConf,
				MinHtlcMsat: minHtlcMsat,
				IsPrivate:   isPrivate,
			})

			if err != nil {
				log.Printf("Error in OpenChannel: %v", err)
				return nil, err
			}

			log.Printf("Response from OpenChannel: (TX: %v)", outPoint.String())
		}

		return &lspdrpc.OpenChannelReply{TxHash: outPoint.Hash.String(), OutputIndex: outPoint.Index}, nil
	})

	if err != nil {
		return nil, err
	}
	return r.(*lspdrpc.OpenChannelReply), err
}

func getSignedEncryptedData(in *lspdrpc.Encrypted) (string, []byte, bool, error) {
	usedEcies := true
	signedBlob, err := ecies.Decrypt(eciesPrivateKey, in.Data)
	if err != nil {
		log.Printf("ecies.Decrypt(%x) error: %v", in.Data, err)
		usedEcies = false
		signedBlob, err = btceclegacy.Decrypt(privateKey, in.Data)
		if err != nil {
			log.Printf("btcec.Decrypt(%x) error: %v", in.Data, err)
			return "", nil, usedEcies, fmt.Errorf("btcec.Decrypt(%x) error: %w", in.Data, err)
		}
	}
	var signed lspdrpc.Signed
	err = proto.Unmarshal(signedBlob, &signed)
	if err != nil {
		log.Printf("proto.Unmarshal(%x) error: %v", signedBlob, err)
		return "", nil, usedEcies, fmt.Errorf("proto.Unmarshal(%x) error: %w", signedBlob, err)
	}
	pubkey, err := btcec.ParsePubKey(signed.Pubkey)
	if err != nil {
		log.Printf("unable to parse pubkey: %v", err)
		return "", nil, usedEcies, fmt.Errorf("unable to parse pubkey: %w", err)
	}
	wireSig, err := lnwire.NewSigFromRawSignature(signed.Signature)
	if err != nil {
		return "", nil, usedEcies, fmt.Errorf("failed to decode signature: %v", err)
	}
	sig, err := wireSig.ToSignature()
	if err != nil {
		return "", nil, usedEcies, fmt.Errorf("failed to convert from wire format: %v",
			err)
	}
	// The signature is over the sha256 hash of the message.
	digest := chainhash.HashB(signed.Data)
	if !sig.Verify(digest, pubkey) {
		return "", nil, usedEcies, fmt.Errorf("invalid signature")
	}
	return hex.EncodeToString(signed.Pubkey), signed.Data, usedEcies, nil
}

func (s *server) CheckChannels(ctx context.Context, in *lspdrpc.Encrypted) (*lspdrpc.Encrypted, error) {
	nodeID, data, usedEcies, err := getSignedEncryptedData(in)
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
	closedChannels, err := client.GetClosedChannels(nodeID, checkChannelsRequest.WaitingCloseChannels)
	if err != nil {
		log.Printf("GetClosedChannels(%v) error: %v", checkChannelsRequest.FakeChannels, err)
		return nil, fmt.Errorf("GetClosedChannels(%v) error: %w", checkChannelsRequest.FakeChannels, err)
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
	pubkey, err := btcec.ParsePubKey(checkChannelsRequest.EncryptPubkey)
	if err != nil {
		log.Printf("unable to parse pubkey: %v", err)
		return nil, fmt.Errorf("unable to parse pubkey: %w", err)
	}

	var encrypted []byte
	if usedEcies {
		encrypted, err = ecies.Encrypt(eciesPublicKey, dataReply)
		if err != nil {
			log.Printf("ecies.Encrypt() error: %v", err)
			return nil, fmt.Errorf("ecies.Encrypt() error: %w", err)
		}
	} else {
		encrypted, err = btceclegacy.Encrypt(pubkey, dataReply)
		if err != nil {
			log.Printf("btcec.Encrypt() error: %v", err)
			return nil, fmt.Errorf("btcec.Encrypt() error: %w", err)
		}
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

func NewGrpcServer() *server {
	return &server{}
}

func (s *server) Start() error {
	pk, err := hex.DecodeString(os.Getenv("LSPD_PRIVATE_KEY"))
	if err != nil {
		log.Fatalf("hex.DecodeString(os.Getenv(\"LSPD_PRIVATE_KEY\")=%v) error: %v", os.Getenv("LSPD_PRIVATE_KEY"), err)
	}

	eciesPrivateKey = ecies.NewPrivateKeyFromBytes(pk)
	eciesPublicKey = eciesPrivateKey.PublicKey
	privateKey, publicKey = btcec.PrivKeyFromBytes(pk)

	certmagicDomain := os.Getenv("CERTMAGIC_DOMAIN")
	address := os.Getenv("LISTEN_ADDRESS")
	var lis net.Listener
	if certmagicDomain == "" {
		var err error
		lis, err = net.Listen("tcp", address)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
	} else {
		tlsConfig, err := certmagic.TLS([]string{certmagicDomain})
		if err != nil {
			log.Fatalf("failed to run certmagic: %v", err)
		}
		lis, err = tls.Listen("tcp", address, tlsConfig)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
	}

	srv := grpc.NewServer(
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
	lspdrpc.RegisterChannelOpenerServer(srv, &server{})

	s.s = srv
	s.lis = lis
	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %v", err)
	}

	return nil
}

func (s *server) Stop() {
	srv := s.s
	if srv != nil {
		srv.GracefulStop()
	}
}
