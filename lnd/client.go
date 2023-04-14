package lnd

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/breez/lspd/basetypes"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/htlcswitch/hop"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type LndClient struct {
	client              lnrpc.LightningClient
	routerClient        routerrpc.RouterClient
	chainNotifierClient chainrpc.ChainNotifierClient
	conn                *grpc.ClientConn
}

func NewLndClient(conf *config.LndConfig) (*LndClient, error) {
	_, err := hex.DecodeString(conf.Macaroon)
	if err != nil {
		return nil, fmt.Errorf("failed to decode macaroon: %w", err)
	}

	// Creds file to connect to LND gRPC
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM([]byte(conf.Cert)) {
		return nil, fmt.Errorf("credentials: failed to append certificates")
	}
	creds := credentials.NewClientTLSFromCert(cp, "")
	macCred := NewMacaroonCredential(conf.Macaroon)

	// Address of an LND instance
	conn, err := grpc.Dial(
		conf.Address,
		grpc.WithTransportCredentials(creds),
		grpc.WithPerRPCCredentials(macCred),
	)
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
	}, nil
}

func (c *LndClient) Close() {
	c.conn.Close()
}

func (c *LndClient) GetInfo() (*lightning.GetInfoResult, error) {
	info, err := c.client.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Printf("LND: client.GetInfo() error: %v", err)
		return nil, err
	}

	return &lightning.GetInfoResult{
		Alias:  info.Alias,
		Pubkey: info.IdentityPubkey,
	}, nil
}

func (c *LndClient) IsConnected(destination []byte) (bool, error) {
	pubKey := hex.EncodeToString(destination)

	r, err := c.client.ListPeers(context.Background(), &lnrpc.ListPeersRequest{LatestError: true})
	if err != nil {
		log.Printf("LND: client.ListPeers() error: %v", err)
		return false, fmt.Errorf("LND: client.ListPeers() error: %w", err)
	}
	for _, peer := range r.Peers {
		if pubKey == peer.PubKey {
			log.Printf("destination online: %x", destination)
			return true, nil
		}
	}

	log.Printf("LND: destination offline: %x", destination)
	return false, nil
}

func (c *LndClient) OpenChannel(req *lightning.OpenChannelRequest) (*wire.OutPoint, error) {
	lnReq := &lnrpc.OpenChannelRequest{
		NodePubkey:         req.Destination,
		LocalFundingAmount: int64(req.CapacitySat),
		PushSat:            0,
		Private:            req.IsPrivate,
		CommitmentType:     lnrpc.CommitmentType_ANCHORS,
		ZeroConf:           req.IsZeroConf,
	}

	if req.MinConfs != nil {
		minConfs := *req.MinConfs
		lnReq.MinConfs = int32(minConfs)
		if minConfs == 0 {
			lnReq.SpendUnconfirmed = true
		}
	}

	if req.FeeSatPerVByte != nil {
		lnReq.SatPerVbyte = uint64(*req.FeeSatPerVByte)
	} else if req.TargetConf != nil {
		lnReq.TargetConf = int32(*req.TargetConf)
	}

	channelPoint, err := c.client.OpenChannelSync(context.Background(), lnReq)
	if err != nil {
		log.Printf("LND: client.OpenChannelSync(%x, %v) error: %v", req.Destination, req.CapacitySat, err)
		return nil, fmt.Errorf("LND: OpenChannel() error: %w", err)
	}

	result, err := basetypes.NewOutPoint(channelPoint.GetFundingTxidBytes(), channelPoint.OutputIndex)
	if err != nil {
		log.Printf("LND: OpenChannel returned invalid outpoint. error: %v", err)
		return nil, err
	}

	return result, nil
}

func (c *LndClient) GetChannel(peerID []byte, channelPoint wire.OutPoint) (*lightning.GetChannelResult, error) {
	r, err := c.client.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{Peer: peerID})
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
			return &lightning.GetChannelResult{
				InitialChannelID:   basetypes.ShortChannelID(c.ChanId),
				ConfirmedChannelID: basetypes.ShortChannelID(confirmedChanId),
			}, nil
		}
	}
	log.Printf("No channel found: getChannel(%x)", peerID)
	return nil, fmt.Errorf("no channel found")
}

func (c *LndClient) GetPeerId(scid *basetypes.ShortChannelID) ([]byte, error) {
	scidu64 := uint64(*scid)
	chans, err := c.client.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		return nil, err
	}

	var dest *string
	for _, ch := range chans.Channels {
		if slices.Contains(ch.AliasScids, scidu64) ||
			ch.ChanId == scidu64 {
			dest = &ch.RemotePubkey
			break
		}
	}

	if dest == nil {
		return nil, nil
	}

	return hex.DecodeString(*dest)
}

func (c *LndClient) GetNodeChannelCount(nodeID []byte) (int, error) {
	nodeIDStr := hex.EncodeToString(nodeID)
	listResponse, err := c.client.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		return 0, err
	}

	pendingResponse, err := c.client.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
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
	pendingResponse, err := c.client.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
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

func (c *LndClient) WaitOnline(peerID []byte, timeout time.Time) error {
	ctx, cancel := context.WithDeadline(context.Background(), timeout)
	defer cancel()

	pkStr := hex.EncodeToString(peerID)
	sub, err := c.client.SubscribePeerEvents(ctx, &lnrpc.PeerEventSubscription{})
	if err != nil {
		return err
	}

	// NOTE: It would be nice to have a lookup for a single peer in LND.
	peers, err := c.client.ListPeers(ctx, &lnrpc.ListPeersRequest{})
	if err != nil {
		return err
	}

	for _, peer := range peers.Peers {
		if peer.PubKey == pkStr {
			return nil
		}
	}

	for {
		ev, err := sub.Recv()
		if err != nil {
			return err
		}

		if ev.PubKey == pkStr && ev.Type == lnrpc.PeerEvent_PEER_ONLINE {
			return nil
		}
	}
}
