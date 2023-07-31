package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/breez/lspd/basetypes"
	"github.com/breez/lspd/btceclegacy"
	"github.com/breez/lspd/interceptor"
	"github.com/breez/lspd/lightning"
	lspdrpc "github.com/breez/lspd/rpc"
	ecies "github.com/ecies/go/v2"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
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

type contextKey string

func (s *channelOpenerServer) ChannelInformation(ctx context.Context, in *lspdrpc.ChannelInformationRequest) (*lspdrpc.ChannelInformationReply, error) {
	node, token, err := s.getNode(ctx)
	if err != nil {
		return nil, err
	}

	params, err := s.createOpeningParamsMenu(ctx, node, token)
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
		OpeningFeeParamsMenu:  params,
	}, nil
}

func (s *channelOpenerServer) createOpeningParamsMenu(
	ctx context.Context,
	node *node,
	token string,
) ([]*lspdrpc.OpeningFeeParams, error) {
	var menu []*lspdrpc.OpeningFeeParams

	settings, err := s.store.GetFeeParamsSettings(token)
	if err != nil {
		log.Printf("Failed to fetch fee params settings: %v", err)
		return nil, fmt.Errorf("failed to get opening_fee_params")
	}

	for _, setting := range settings {
		validUntil := time.Now().UTC().Add(setting.Validity)
		params := &lspdrpc.OpeningFeeParams{
			MinMsat:              setting.Params.MinMsat,
			Proportional:         setting.Params.Proportional,
			ValidUntil:           validUntil.Format(basetypes.TIME_FORMAT),
			MaxIdleTime:          setting.Params.MaxIdleTime,
			MaxClientToSelfDelay: setting.Params.MaxClientToSelfDelay,
		}

		promise, err := createPromise(node, params)
		if err != nil {
			log.Printf("Failed to create promise: %v", err)
			return nil, err
		}

		params.Promise = *promise
		menu = append(menu, params)
	}

	sort.Slice(menu, func(i, j int) bool {
		if menu[i].MinMsat == menu[j].MinMsat {
			return menu[i].Proportional < menu[j].Proportional
		}

		return menu[i].MinMsat < menu[j].MinMsat
	})
	return menu, nil
}

func paramsHash(params *lspdrpc.OpeningFeeParams) ([]byte, error) {
	// First hash all the values in the params in a fixed order.
	items := []interface{}{
		params.MinMsat,
		params.Proportional,
		params.ValidUntil,
		params.MaxIdleTime,
		params.MaxClientToSelfDelay,
	}
	blob, err := json.Marshal(items)
	if err != nil {
		log.Printf("paramsHash error: %v", err)
		return nil, err
	}
	hash := sha256.Sum256(blob)
	return hash[:], nil
}

func createPromise(node *node, params *lspdrpc.OpeningFeeParams) (*string, error) {
	hash, err := paramsHash(params)
	if err != nil {
		return nil, err
	}
	// Sign the hash with the private key of the LSP id.
	sig, err := ecdsa.SignCompact(node.privateKey, hash[:], true)
	if err != nil {
		log.Printf("createPromise: SignCompact error: %v", err)
		return nil, err
	}
	promise := hex.EncodeToString(sig)
	return &promise, nil
}

func verifyPromise(node *node, params *lspdrpc.OpeningFeeParams) error {
	hash, err := paramsHash(params)
	if err != nil {
		return err
	}
	sig, err := hex.DecodeString(params.Promise)
	if err != nil {
		log.Printf("verifyPromise: hex.DecodeString error: %v", err)
		return err
	}
	pub, _, err := ecdsa.RecoverCompact(sig, hash)
	if err != nil {
		log.Printf("verifyPromise: RecoverCompact(%x) error: %v", sig, err)
		return err
	}
	if !node.publicKey.IsEqual(pub) {
		log.Print("verifyPromise: not signed by us", err)
		return fmt.Errorf("invalid promise")
	}
	return nil
}

func validateOpeningFeeParams(node *node, params *lspdrpc.OpeningFeeParams) bool {
	if params == nil {
		return false
	}

	err := verifyPromise(node, params)
	if err != nil {
		return false
	}

	t, err := time.Parse(basetypes.TIME_FORMAT, params.ValidUntil)
	if err != nil {
		log.Printf("validateOpeningFeeParams: time.Parse(%v, %v) error: %v", basetypes.TIME_FORMAT, params.ValidUntil, err)
		return false
	}

	if time.Now().UTC().After(t) {
		log.Printf("validateOpeningFeeParams: promise not valid anymore: %v", t)
		return false
	}

	return true
}

func (s *channelOpenerServer) RegisterPayment(
	ctx context.Context,
	in *lspdrpc.RegisterPaymentRequest,
) (*lspdrpc.RegisterPaymentReply, error) {
	node, token, err := s.getNode(ctx)
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

	// TODO: Remove this nil check and the else cluase when we enforce all
	// clients to use opening_fee_params.
	if pi.OpeningFeeParams != nil {
		valid := validateOpeningFeeParams(node, pi.OpeningFeeParams)
		if !valid {
			return nil, fmt.Errorf("invalid opening_fee_params")
		}
	} else {
		log.Printf("DEPRECATED: RegisterPayment with deprecated fee mechanism.")
		pi.OpeningFeeParams = &lspdrpc.OpeningFeeParams{
			MinMsat:              uint64(node.nodeConfig.ChannelMinimumFeeMsat),
			Proportional:         uint32(node.nodeConfig.ChannelFeePermyriad * 100),
			ValidUntil:           time.Now().UTC().Add(time.Duration(time.Hour * 24)).Format(basetypes.TIME_FORMAT),
			MaxIdleTime:          uint32(node.nodeConfig.MaxInactiveDuration / 600),
			MaxClientToSelfDelay: uint32(10000),
		}
	}

	err = checkPayment(pi.OpeningFeeParams, pi.IncomingAmountMsat, pi.OutgoingAmountMsat)
	if err != nil {
		log.Printf("checkPayment(%v, %v) error: %v", pi.IncomingAmountMsat, pi.OutgoingAmountMsat, err)
		return nil, fmt.Errorf("checkPayment(%v, %v) error: %v", pi.IncomingAmountMsat, pi.OutgoingAmountMsat, err)
	}
	params := &interceptor.OpeningFeeParams{
		MinMsat:              pi.OpeningFeeParams.MinMsat,
		Proportional:         pi.OpeningFeeParams.Proportional,
		ValidUntil:           pi.OpeningFeeParams.ValidUntil,
		MaxIdleTime:          pi.OpeningFeeParams.MaxIdleTime,
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

func (s *channelOpenerServer) OpenChannel(ctx context.Context, in *lspdrpc.OpenChannelRequest) (*lspdrpc.OpenChannelReply, error) {
	node, _, err := s.getNode(ctx)
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
	node, _, err := s.getNode(ctx)
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

func (s *channelOpenerServer) getNode(ctx context.Context) (*node, string, error) {
	nd := ctx.Value(contextKey("node"))
	if nd == nil {
		return nil, "", status.Errorf(codes.PermissionDenied, "Not authorized")
	}

	nodeContext, ok := nd.(*nodeContext)
	if !ok {
		return nil, "", status.Errorf(codes.PermissionDenied, "Not authorized")
	}

	return nodeContext.node, nodeContext.token, nil
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
