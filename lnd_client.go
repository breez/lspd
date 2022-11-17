package main

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/htlcswitch/hop"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type LndClient struct {
	client              lnrpc.LightningClient
	routerClient        routerrpc.RouterClient
	chainNotifierClient chainrpc.ChainNotifierClient
	conn                *grpc.ClientConn
}

func NewLndClient() *LndClient {
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

	client := lnrpc.NewLightningClient(conn)
	routerClient := routerrpc.NewRouterClient(conn)
	chainNotifierClient := chainrpc.NewChainNotifierClient(conn)
	return &LndClient{
		client:              client,
		routerClient:        routerClient,
		chainNotifierClient: chainNotifierClient,
		conn:                conn,
	}
}

func (c *LndClient) Close() {
	c.conn.Close()
}

func (c *LndClient) GetInfo() (*GetInfoResult, error) {
	info, err := c.client.GetInfo(macaroonContext(), &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Printf("LND: client.GetInfo() error: %v", err)
		return nil, err
	}

	return &GetInfoResult{
		Alias:  info.Alias,
		Pubkey: info.IdentityPubkey,
	}, nil
}

func (c *LndClient) IsConnected(destination []byte) (*bool, error) {
	pubKey := hex.EncodeToString(destination)

	r, err := c.client.ListPeers(macaroonContext(), &lnrpc.ListPeersRequest{LatestError: true})
	if err != nil {
		log.Printf("LND: client.ListPeers() error: %v", err)
		return nil, fmt.Errorf("LND: client.ListPeers() error: %w", err)
	}
	for _, peer := range r.Peers {
		if pubKey == peer.PubKey {
			log.Printf("destination online: %x", destination)
			result := true
			return &result, nil
		}
	}

	log.Printf("LND: destination offline: %x", destination)
	result := false
	return &result, nil
}

func (c *LndClient) OpenChannel(req *OpenChannelRequest) (*wire.OutPoint, error) {
	channelPoint, err := c.client.OpenChannelSync(macaroonContext(), &lnrpc.OpenChannelRequest{
		NodePubkey:         req.Destination,
		LocalFundingAmount: int64(req.CapacitySat),
		TargetConf:         int32(req.TargetConf),
		PushSat:            0,
		Private:            req.IsPrivate,
		CommitmentType:     lnrpc.CommitmentType_ANCHORS,
		ZeroConf:           req.IsZeroConf,
	})

	if err != nil {
		log.Printf("LND: client.OpenChannelSync(%x, %v) error: %v", req.Destination, req.CapacitySat, err)
		return nil, fmt.Errorf("LND: OpenChannel() error: %w", err)
	}

	result, err := NewOutPoint(channelPoint.GetFundingTxidBytes(), channelPoint.OutputIndex)
	if err != nil {
		log.Printf("LND: OpenChannel returned invalid outpoint. error: %v", err)
		return nil, err
	}

	return result, nil
}

func (c *LndClient) GetChannel(peerID []byte, channelPoint wire.OutPoint) (*GetChannelResult, error) {
	r, err := c.client.ListChannels(macaroonContext(), &lnrpc.ListChannelsRequest{Peer: peerID})
	if err != nil {
		log.Printf("client.ListChannels(%x) error: %v", peerID, err)
		return nil, err
	}

	channelPointStr := channelPoint.String()
	if err != nil {
		return nil, err
	}

	for _, c := range r.Channels {
		log.Printf("getChannel(%x): %v", peerID, c.ChanId)
		if c.ChannelPoint == channelPointStr && c.Active {
			confirmedChanId := c.ChanId
			if c.ZeroConf {
				confirmedChanId = c.ZeroConfConfirmedScid
				if confirmedChanId == hop.Source.ToUint64() {
					confirmedChanId = 0
				}
			}
			return &GetChannelResult{
				InitialChannelID:   ShortChannelID(c.ChanId),
				ConfirmedChannelID: ShortChannelID(confirmedChanId),
			}, nil
		}
	}
	log.Printf("No channel found: getChannel(%x)", peerID)
	return nil, fmt.Errorf("no channel found")
}

func (c *LndClient) GetNodeChannelCount(nodeID []byte) (int, error) {
	nodeIDStr := hex.EncodeToString(nodeID)
	clientCtx := metadata.AppendToOutgoingContext(context.Background(), "macaroon", os.Getenv("LND_MACAROON_HEX"))
	listResponse, err := c.client.ListChannels(clientCtx, &lnrpc.ListChannelsRequest{})
	if err != nil {
		return 0, err
	}

	pendingResponse, err := c.client.PendingChannels(clientCtx, &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, channel := range listResponse.Channels {
		if channel.RemotePubkey == nodeIDStr {
			count++
		}
	}

	for _, p := range pendingResponse.PendingOpenChannels {
		if p.Channel.RemoteNodePub == nodeIDStr {
			count++
		}
	}

	return count, nil
}

func (c *LndClient) GetClosedChannels(nodeID string, channelPoints map[string]uint64) (map[string]uint64, error) {
	r := make(map[string]uint64)
	if len(channelPoints) == 0 {
		return r, nil
	}
	waitingCloseChannels, err := c.getWaitingCloseChannels(nodeID)
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

func (c *LndClient) getWaitingCloseChannels(nodeID string) ([]*lnrpc.PendingChannelsResponse_WaitingCloseChannel, error) {
	pendingResponse, err := c.client.PendingChannels(macaroonContext(), &lnrpc.PendingChannelsRequest{})
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

func macaroonContext() context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "macaroon", os.Getenv("LND_MACAROON_HEX"))
}
