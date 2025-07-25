package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/breez/lspd/btceclegacy"
	"github.com/breez/lspd/common"
	"github.com/breez/lspd/format"
	"github.com/breez/lspd/interceptor"
	lspdrpc "github.com/breez/lspd/rpc"
	ecies "github.com/ecies/go/v2"
	"google.golang.org/protobuf/proto"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightningnetwork/lnd/lnwire"
)

type channelOpenerServer struct {
	lspdrpc.ChannelOpenerServer
	store          interceptor.InterceptStore
	openingService common.OpeningService
}

func NewChannelOpenerServer(
	store interceptor.InterceptStore,
	openingService common.OpeningService,
) *channelOpenerServer {
	return &channelOpenerServer{
		store:          store,
		openingService: openingService,
	}
}

func (s *channelOpenerServer) ChannelInformation(ctx context.Context, in *lspdrpc.ChannelInformationRequest) (*lspdrpc.ChannelInformationReply, error) {
	node, token, err := lspdrpc.GetNode(ctx)
	if err != nil {
		return nil, err
	}

	params, err := s.openingService.GetFeeParamsMenu(token, node.PrivateKey)
	if err != nil {
		return nil, err
	}

	var menu []*lspdrpc.OpeningFeeParams
	for _, p := range params {
		menu = append(menu, &lspdrpc.OpeningFeeParams{
			MinMsat:              p.MinFeeMsat,
			Proportional:         p.Proportional,
			ValidUntil:           p.ValidUntil,
			MaxIdleTime:          p.MinLifetime,
			MaxClientToSelfDelay: p.MaxClientToSelfDelay,
			Promise:              p.Promise,
		})
	}

	return &lspdrpc.ChannelInformationReply{
		Name:                  node.NodeConfig.Name,
		Pubkey:                node.NodeConfig.NodePubkey,
		Host:                  node.NodeConfig.Host,
		ChannelCapacity:       int64(node.NodeConfig.PublicChannelAmount),
		TargetConf:            int32(node.NodeConfig.TargetConf),
		MinHtlcMsat:           int64(node.NodeConfig.MinHtlcMsat),
		BaseFeeMsat:           int64(node.NodeConfig.BaseFeeMsat),
		FeeRate:               node.NodeConfig.FeeRate,
		TimeLockDelta:         node.NodeConfig.TimeLockDelta,
		ChannelFeePermyriad:   int64(node.NodeConfig.ChannelFeePermyriad),
		ChannelMinimumFeeMsat: int64(node.NodeConfig.ChannelMinimumFeeMsat),
		LspPubkey:             node.PublicKey.SerializeCompressed(), // TODO: Is the publicKey different from the ecies public key?
		MaxInactiveDuration:   int64(node.NodeConfig.MaxInactiveDuration),
		OpeningFeeParamsMenu:  menu,
	}, nil
}

func (s *channelOpenerServer) RegisterPayment(
	ctx context.Context,
	in *lspdrpc.RegisterPaymentRequest,
) (*lspdrpc.RegisterPaymentReply, error) {
	node, token, err := lspdrpc.GetNode(ctx)
	if err != nil {
		return nil, err
	}

	data, err := ecies.Decrypt(node.EciesPrivateKey, in.Blob)
	if err != nil {
		log.Printf("ecies.Decrypt(%x) error: %v", in.Blob, err)
		data, err = btceclegacy.Decrypt(node.PrivateKey, in.Blob)
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

	// TODO: Remove this nil check and the else cluase when we enforce all
	// clients to use opening_fee_params.
	if pi.OpeningFeeParams != nil {
		valid := s.openingService.ValidateOpeningFeeParams(
			&common.OpeningFeeParams{
				MinFeeMsat:           pi.OpeningFeeParams.MinMsat,
				Proportional:         pi.OpeningFeeParams.Proportional,
				ValidUntil:           pi.OpeningFeeParams.ValidUntil,
				MinLifetime:          pi.OpeningFeeParams.MaxIdleTime,
				MaxClientToSelfDelay: pi.OpeningFeeParams.MaxClientToSelfDelay,
				Promise:              pi.OpeningFeeParams.Promise,
			},
			node.PublicKey,
		)
		if !valid {
			return nil, fmt.Errorf("invalid opening_fee_params")
		}
	} else {
		tokenHash := sha256.Sum256([]byte(token))
		log.Printf("DEPRECATED: RegisterPayment with deprecated fee mechanism. sha256 hash of token used: %x", tokenHash)
		pi.OpeningFeeParams = &lspdrpc.OpeningFeeParams{
			MinMsat:              uint64(node.NodeConfig.ChannelMinimumFeeMsat),
			Proportional:         uint32(node.NodeConfig.ChannelFeePermyriad * 100),
			ValidUntil:           time.Now().UTC().Add(time.Duration(time.Hour * 24)).Format(format.TIME_FORMAT),
			MaxIdleTime:          uint32(node.NodeConfig.MaxInactiveDuration / 600),
			MaxClientToSelfDelay: uint32(10000),
		}
	}

	err = checkPayment(pi.OpeningFeeParams, pi.IncomingAmountMsat, pi.OutgoingAmountMsat)
	if err != nil {
		log.Printf("checkPayment(%v, %v) error: %v", pi.IncomingAmountMsat, pi.OutgoingAmountMsat, err)
		return nil, fmt.Errorf("checkPayment(%v, %v) error: %v", pi.IncomingAmountMsat, pi.OutgoingAmountMsat, err)
	}
	params := &common.OpeningFeeParams{
		MinFeeMsat:           pi.OpeningFeeParams.MinMsat,
		Proportional:         pi.OpeningFeeParams.Proportional,
		ValidUntil:           pi.OpeningFeeParams.ValidUntil,
		MinLifetime:          pi.OpeningFeeParams.MaxIdleTime,
		MaxClientToSelfDelay: pi.OpeningFeeParams.MaxClientToSelfDelay,
		Promise:              pi.OpeningFeeParams.Promise,
	}
	err = s.store.RegisterPayment(token, params, pi.Destination, pi.PaymentHash, pi.PaymentSecret, pi.IncomingAmountMsat, pi.OutgoingAmountMsat, pi.Tag)
	if err != nil {
		log.Printf("RegisterPayment() error: %v", err)
		return nil, fmt.Errorf("RegisterPayment() error: %w", err)
	}
	return &lspdrpc.RegisterPaymentReply{}, nil
}

func getSignedEncryptedData(n *common.Node, in *lspdrpc.Encrypted) (string, []byte, bool, error) {
	usedEcies := true
	signedBlob, err := ecies.Decrypt(n.EciesPrivateKey, in.Data)
	if err != nil {
		log.Printf("ecies.Decrypt(%x) error: %v", in.Data, err)
		usedEcies = false
		signedBlob, err = btceclegacy.Decrypt(n.PrivateKey, in.Data)
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
	wireSig, err := lnwire.NewSigFromECDSARawSignature(signed.Signature)
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
	node, _, err := lspdrpc.GetNode(ctx)
	if err != nil {
		return nil, err
	}

	nodeID, data, usedEcies, err := getSignedEncryptedData(node, in)
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
	closedChannels, err := node.Client.GetClosedChannels(nodeID, checkChannelsRequest.WaitingCloseChannels)
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
		encrypted, err = ecies.Encrypt(node.EciesPublicKey, dataReply)
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

func checkPayment(params *lspdrpc.OpeningFeeParams, incomingAmountMsat, outgoingAmountMsat int64) error {
	fees := incomingAmountMsat * int64(params.Proportional) / 1_000_000 / 1_000 * 1_000
	if fees < int64(params.MinMsat) {
		fees = int64(params.MinMsat)
	}
	if incomingAmountMsat-outgoingAmountMsat < fees {
		return fmt.Errorf("not enough fees")
	}
	return nil
}
