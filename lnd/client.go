package lnd

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/breez/lspd/basetypes"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/htlcswitch/hop"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type LndClient struct {
	client              lnrpc.LightningClient
	routerClient        routerrpc.RouterClient
	chainNotifierClient chainrpc.ChainNotifierClient
	conn                *grpc.ClientConn
	listenerCtx         context.Context
	listenerCancel      context.CancelFunc
	peersubs            map[string]map[uint64]chan struct{}
	chansubs            map[string]map[uint64]chan struct{}
	submtx              sync.RWMutex
	index               uint64
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
		peersubs:            make(map[string]map[uint64]chan struct{}),
		chansubs:            make(map[string]map[uint64]chan struct{}),
	}, nil
}

func (c *LndClient) Close() {
	cancel := c.listenerCancel
	if cancel != nil {
		cancel()
	}
	c.conn.Close()
}

func (c *LndClient) StartListeners() {
	c.listenerCtx, c.listenerCancel = context.WithCancel(context.Background())
	go c.listenPeerEvents()
	go c.listenChannelEvents()
}

func (c *LndClient) listenPeerEvents() {
	ctx := c.listenerCtx
	for {
		if ctx.Err() != nil {
			return
		}

		sub, err := c.client.SubscribePeerEvents(
			ctx,
			&lnrpc.PeerEventSubscription{},
		)
		if err != nil {
			log.Printf("SubscribePeerEvents: %v", err)
			<-time.After(time.Second)
			continue
		}

		for {
			if ctx.Err() != nil {
				return
			}

			msg, err := sub.Recv()
			if err != nil {
				status, ok := status.FromError(err)
				if ok && status.Code() == codes.Canceled {
					log.Printf("listenPeerEvents: Got code canceled. Break.")
					break
				}

				log.Printf("unexpected error in listenPeerEvents: %v", err)
				break
			}

			if msg.Type != lnrpc.PeerEvent_PEER_ONLINE {
				continue
			}

			c.submtx.RLock()
			subs, ok := c.peersubs[msg.PubKey]
			if ok {
				for _, sub := range subs {
					sub <- struct{}{}
				}
			}
			c.submtx.RUnlock()
		}

		<-time.After(time.Second)
	}
}

func (c *LndClient) listenChannelEvents() {
	ctx := c.listenerCtx
	for {
		if ctx.Err() != nil {
			return
		}

		sub, err := c.client.SubscribeChannelEvents(
			ctx,
			&lnrpc.ChannelEventSubscription{},
		)
		if err != nil {
			log.Printf("listenChannelEvents: SubscribeChannelEvents: %v", err)
			<-time.After(time.Second)
			continue
		}

		for {
			if ctx.Err() != nil {
				return
			}

			msg, err := sub.Recv()
			if err != nil {
				status, ok := status.FromError(err)
				if ok && status.Code() == codes.Canceled {
					log.Printf("listenChannelEvents: Got code canceled. Break.")
					break
				}

				log.Printf("unexpected error in listenChannelEvents: %v", err)
				break
			}

			if msg.Type != lnrpc.ChannelEventUpdate_ACTIVE_CHANNEL {
				continue
			}

			ch := msg.GetActiveChannel()
			point, err := extractChannelPoint(ch)
			if err != nil {
				log.Printf("listenChannelEvents: Failed to extract channel point %+v: %v", ch, err)
				continue
			}

			c.submtx.RLock()
			subs, ok := c.chansubs[point]
			if ok {
				for _, sub := range subs {
					sub <- struct{}{}
				}
			}
			c.submtx.RUnlock()
		}

		<-time.After(time.Second)
	}
}

func extractChannelPoint(cp *lnrpc.ChannelPoint) (string, error) {
	str := cp.GetFundingTxidStr()
	if str == "" {
		b := cp.GetFundingTxidBytes()
		h, err := chainhash.NewHash(b)
		if err != nil {
			return "", err
		}
		str = h.String()
	}

	return fmt.Sprintf("%s:%d", str, cp.OutputIndex), nil
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
	pubkey := hex.EncodeToString(destination)

	r, err := c.client.GetPeerConnected(context.Background(), &lnrpc.GetPeerConnectedRequest{
		Pubkey: pubkey,
	})
	if err != nil {
		log.Printf("LND: client.GetPeerConnected() error: %v", err)
		return false, fmt.Errorf("LND: client.GetPeerConnected() error: %w", err)
	}
	if r.Connected {
		log.Printf("LND: destination online: %x", destination)
		return true, nil
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
				HtlcMinimumMsat:    c.LocalConstraints.MinHtlcMsat,
			}, nil
		}
	}
	log.Printf("No channel found: getChannel(%x)", peerID)
	return nil, fmt.Errorf("no channel found")
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

func (c *LndClient) GetPeerId(scid *basetypes.ShortChannelID) ([]byte, error) {
	scidu64 := uint64(*scid)
	peer, err := c.client.GetPeerIdByScid(context.Background(), &lnrpc.GetPeerIdByScidRequest{
		Scid: scidu64,
	})
	if err != nil {
		return nil, err
	}

	if peer.PeerId == "" {
		return nil, nil
	}

	peerid, _ := hex.DecodeString(peer.PeerId)
	return peerid, nil
}

func (c *LndClient) WaitOnline(peerID []byte, deadline time.Time) error {
	pkStr := hex.EncodeToString(peerID)
	signal := make(chan struct{}, 10)
	defer close(signal)

	c.submtx.Lock()
	subid := c.index
	c.index++
	subs, ok := c.peersubs[pkStr]
	if !ok {
		subs = make(map[uint64]chan struct{})
		c.peersubs[pkStr] = subs
	}

	subs[subid] = signal
	c.submtx.Unlock()

	defer func() {
		c.submtx.Lock()
		subs, ok := c.peersubs[pkStr]
		if ok {
			delete(subs, subid)
			if len(subs) == 0 {
				delete(c.peersubs, pkStr)
			}
		}
		c.submtx.Unlock()
	}()

	connected, err := c.IsConnected(peerID)
	if err != nil {
		return err
	}
	if connected {
		return nil
	}

	select {
	case <-signal:
		return nil
	case <-time.After(time.Until(deadline)):
		return fmt.Errorf("deadline exceeded")
	}
}

func (c *LndClient) WaitChannelActive(peerID []byte, deadline time.Time) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fetch the channels for this peer
	chans, err := c.client.ListChannels(ctx, &lnrpc.ListChannelsRequest{
		Peer: peerID,
	})
	if err != nil {
		return err
	}
	if len(chans.Channels) == 0 {
		return fmt.Errorf("no channels with peer")
	}

	// Exit now if a channel is already active.
	for _, ch := range chans.Channels {
		if ch.Active {
			return nil
		}
	}

	signal := make(chan struct{}, 10)
	defer close(signal)

	// Subscribe to channel active events from this channel
	c.submtx.Lock()
	for _, ch := range chans.Channels {
		chansignal := make(chan struct{}, 10)
		defer close(chansignal)

		// forward signals from all channels to the signal aggregate
		go func(c chan struct{}) {
			for msg := range c {
				signal <- msg
			}
		}(chansignal)

		outpoint := ch.ChannelPoint
		subid := c.index
		c.index++
		subs, ok := c.chansubs[outpoint]
		if !ok {
			subs = make(map[uint64]chan struct{})
			c.chansubs[outpoint] = subs
		}

		subs[subid] = chansignal
		defer func() {
			c.submtx.Lock()
			subs, ok := c.chansubs[outpoint]
			if ok {
				delete(subs, subid)
				if len(subs) == 0 {
					delete(c.chansubs, outpoint)
				}
			}
			c.submtx.Unlock()
		}()
	}
	c.submtx.Unlock()

	// Fetch the channels for this peer again, so there is no gap between the
	// subscription and the call.
	chans, err = c.client.ListChannels(ctx, &lnrpc.ListChannelsRequest{
		Peer: peerID,
	})
	if err != nil {
		return err
	}

	// Exit now if a channel is already active.
	for _, ch := range chans.Channels {
		if ch.Active {
			return nil
		}
	}

	select {
	case <-signal:
		return nil
	case <-time.After(time.Until(deadline)):
		return fmt.Errorf("deadline exceeded")
	}
}
