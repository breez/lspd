package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	"github.com/breez/lspd/btceclegacy"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/interceptor"
	"github.com/breez/lspd/lightning"
	lspdrpc "github.com/breez/lspd/rpc"
	ecies "github.com/ecies/go/v2"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/lnwire"
)

type channelOpenerServer struct {
	lspdrpc.ChannelOpenerServer
	store interceptor.InterceptStore
}

func NewChannelOpenerServer(
	store interceptor.InterceptStore,
) *channelOpenerServer {
	return &channelOpenerServer{
		store: store,
	}
}

func (s *channelOpenerServer) ChannelInformation(ctx context.Context, in *lspdrpc.ChannelInformationRequest) (*lspdrpc.ChannelInformationReply, error) {
	node, err := getNode(ctx)
	if err != nil {
		return nil, err
	}

	return &lspdrpc.ChannelInformationReply{
		Name:                  node.nodeConfig.Name,
		Pubkey:                node.nodeConfig.NodePubkey,
		Host:                  node.nodeConfig.Host,
		ChannelCapacity:       int64(node.nodeConfig.PublicChannelAmount),
		TargetConf:            int32(node.nodeConfig.TargetConf),
		MinHtlcMsat:           int64(node.nodeConfig.MinHtlcMsat),
		BaseFeeMsat:           int64(node.nodeConfig.BaseFeeMsat),
		FeeRate:               node.nodeConfig.FeeRate,
		TimeLockDelta:         node.nodeConfig.TimeLockDelta,
		ChannelFeePermyriad:   int64(node.nodeConfig.ChannelFeePermyriad),
		ChannelMinimumFeeMsat: int64(node.nodeConfig.ChannelMinimumFeeMsat),
		LspPubkey:             node.publicKey.SerializeCompressed(), // TODO: Is the publicKey different from the ecies public key?
		MaxInactiveDuration:   int64(node.nodeConfig.MaxInactiveDuration),
	}, nil
}

func (s *channelOpenerServer) RegisterPayment(ctx context.Context, in *lspdrpc.RegisterPaymentRequest) (*lspdrpc.RegisterPaymentReply, error) {
	node, err := getNode(ctx)
	if err != nil {
		return nil, err
	}

	data, err := ecies.Decrypt(node.eciesPrivateKey, in.Blob)
	if err != nil {
		log.Printf("ecies.Decrypt(%x) error: %v", in.Blob, err)
		data, err = btceclegacy.Decrypt(node.privateKey, in.Blob)
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
	log.Printf("RegisterPayment - Destination: %x, pi.PaymentHash: %x, pi.PaymentSecret: %x, pi.IncomingAmountMsat: %v, pi.OutgoingAmountMsat: %v, pi.Tag: %v",
		pi.Destination, pi.PaymentHash, pi.PaymentSecret, pi.IncomingAmountMsat, pi.OutgoingAmountMsat, pi.Tag)

	if len(pi.Tag) > 1000 {
		return nil, fmt.Errorf("tag too long")
	}

	if len(pi.Tag) != 0 {
		var tag json.RawMessage
		err = json.Unmarshal([]byte(pi.Tag), &tag)
		if err != nil {
			return nil, fmt.Errorf("tag is not a valid json object")
		}
	}

	err = checkPayment(node.nodeConfig, pi.IncomingAmountMsat, pi.OutgoingAmountMsat)
	if err != nil {
		log.Printf("checkPayment(%v, %v) error: %v", pi.IncomingAmountMsat, pi.OutgoingAmountMsat, err)
		return nil, fmt.Errorf("checkPayment(%v, %v) error: %v", pi.IncomingAmountMsat, pi.OutgoingAmountMsat, err)
	}
	err = s.store.RegisterPayment(pi.Destination, pi.PaymentHash, pi.PaymentSecret, pi.IncomingAmountMsat, pi.OutgoingAmountMsat, pi.Tag)
	if err != nil {
		log.Printf("RegisterPayment() error: %v", err)
		return nil, fmt.Errorf("RegisterPayment() error: %w", err)
	}
	return &lspdrpc.RegisterPaymentReply{}, nil
}

func (s *channelOpenerServer) OpenChannel(ctx context.Context, in *lspdrpc.OpenChannelRequest) (*lspdrpc.OpenChannelReply, error) {
	node, err := getNode(ctx)
	if err != nil {
		return nil, err
	}

	r, err, _ := node.openChannelReqGroup.Do(in.Pubkey, func() (interface{}, error) {
		pubkey, err := hex.DecodeString(in.Pubkey)
		if err != nil {
			return nil, err
		}

		channelCount, err := node.client.GetNodeChannelCount(pubkey)
		if err != nil {
			return nil, err
		}

		var outPoint *wire.OutPoint
		if channelCount == 0 {
			outPoint, err = node.client.OpenChannel(&lightning.OpenChannelRequest{
				CapacitySat: node.nodeConfig.ChannelAmount,
				Destination: pubkey,
				TargetConf:  &node.nodeConfig.TargetConf,
				MinHtlcMsat: node.nodeConfig.MinHtlcMsat,
				IsPrivate:   node.nodeConfig.ChannelPrivate,
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

func (n *node) getSignedEncryptedData(in *lspdrpc.Encrypted) (string, []byte, bool, error) {
	usedEcies := true
	signedBlob, err := ecies.Decrypt(n.eciesPrivateKey, in.Data)
	if err != nil {
		log.Printf("ecies.Decrypt(%x) error: %v", in.Data, err)
		usedEcies = false
		signedBlob, err = btceclegacy.Decrypt(n.privateKey, in.Data)
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

func (s *channelOpenerServer) CheckChannels(ctx context.Context, in *lspdrpc.Encrypted) (*lspdrpc.Encrypted, error) {
	node, err := getNode(ctx)
	if err != nil {
		return nil, err
	}

	nodeID, data, usedEcies, err := node.getSignedEncryptedData(in)
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
	closedChannels, err := node.client.GetClosedChannels(nodeID, checkChannelsRequest.WaitingCloseChannels)
	if err != nil {
		log.Printf("GetClosedChannels(%v) error: %v", checkChannelsRequest.FakeChannels, err)
		return nil, fmt.Errorf("GetClosedChannels(%v) error: %w", checkChannelsRequest.FakeChannels, err)
	}
	checkChannelsReply := lspdrpc.CheckChannelsReply{
		NotFakeChannels: make(map[string]uint64),
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
		encrypted, err = ecies.Encrypt(node.eciesPublicKey, dataReply)
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

func getNode(ctx context.Context) (*node, error) {
	n := ctx.Value("node")
	if n == nil {
		return nil, status.Errorf(codes.PermissionDenied, "Not authorized")
	}

	node, ok := n.(*node)
	if !ok || node == nil {
		return nil, status.Errorf(codes.PermissionDenied, "Not authorized")
	}

	return node, nil
}

func checkPayment(config *config.NodeConfig, incomingAmountMsat, outgoingAmountMsat int64) error {
	fees := incomingAmountMsat * config.ChannelFeePermyriad / 10_000 / 1_000 * 1_000
	if fees < config.ChannelMinimumFeeMsat {
		fees = config.ChannelMinimumFeeMsat
	}
	if incomingAmountMsat-outgoingAmountMsat < fees {
		return fmt.Errorf("not enough fees")
	}
	return nil
}
