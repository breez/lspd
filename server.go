package main

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"log"
	"net"
	"os"
	"strings"

	lspdrpc "github.com/breez/lspd/rpc"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightningnetwork/lnd/lnrpc"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const (
	channelAmount = 1000000
)

type server struct{}

var (
	client              lnrpc.LightningClient
	openChannelReqGroup singleflight.Group
)

func (s *server) ChannelInformation(ctx context.Context, in *lspdrpc.ChannelInformationRequest) (*lspdrpc.ChannelInformationReply, error) {
	return &lspdrpc.ChannelInformationReply{
		Name:            os.Getenv("NODE_NAME"),
		Pubkey:          os.Getenv("NODE_PUBKEY"),
		Host:            os.Getenv("NODE_HOST"),
		ChannelCapacity: channelAmount,
		TargetConf:      1,
		MinHtlcMsat:     600,
		BaseFeeMsat:     1000,
		FeeRate:         0.000001,
		TimeLockDelta:   144,
	}, nil
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
		var txidStr string
		if len(nodeChannels) == 0 && len(pendingChannels) == 0 {
			response, err := client.OpenChannelSync(clientCtx, &lnrpc.OpenChannelRequest{
				LocalFundingAmount: channelAmount,
				NodePubkeyString:   in.Pubkey,
				PushSat:            0,
				TargetConf:         1,
				MinHtlcMsat:        600,
				Private:            true,
			})
			log.Printf("Response from OpenChannel: %#v (TX: %v)", response, hex.EncodeToString(response.GetFundingTxidBytes()))

			if err != nil {
				log.Printf("Error in OpenChannel: %v", err)
				return nil, err
			}

			txid, _ := chainhash.NewHash(response.GetFundingTxidBytes())

			// don't fail the request in case we can't format the channel id from
			// some reason...
			if txid != nil {
				txidStr = txid.String()
			}
		}
		return &lspdrpc.OpenChannelReply{TxHash: txidStr}, nil
	})

	if err != nil {
		return nil, err
	}
	return r.(*lspdrpc.OpenChannelReply), err
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

func main() {
	lis, err := net.Listen("tcp", os.Getenv("LISTEN_ADDRESS"))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// Creds file to connect to LND gRPC
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM([]byte(strings.Replace(os.Getenv("LND_CERT"), "\\n", "\n", -1))) {
		log.Fatalf("credentials: failed to append certificates")
	}
	creds := credentials.NewClientTLSFromCert(cp, "")

	// Address of an LND instance
	conn, err := grpc.Dial(os.Getenv("LND_ADDRESS"), grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("Failed to connect to LND gRPC: %v", err)
	}
	defer conn.Close()
	client = lnrpc.NewLightningClient(conn)

	s := grpc.NewServer()
	lspdrpc.RegisterChannelOpenerServer(s, &server{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
