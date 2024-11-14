package cln

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/breez/lspd/cln/rpc"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"github.com/btcsuite/btcd/wire"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type ClnClient struct {
	client rpc.NodeClient
}

var (
	OPEN_STATUSES = []int32{
		int32(rpc.ListpeerchannelsChannels_CHANNELD_NORMAL),
	}
	PENDING_STATUSES = []int32{
		int32(rpc.ListpeerchannelsChannels_OPENINGD),
		int32(rpc.ListpeerchannelsChannels_CHANNELD_AWAITING_LOCKIN),
		int32(rpc.ListpeerchannelsChannels_DUALOPEND_OPEN_INIT),
		int32(rpc.ListpeerchannelsChannels_DUALOPEND_AWAITING_LOCKIN),
		int32(rpc.ListpeerchannelsChannels_CHANNELD_AWAITING_SPLICE),
		int32(rpc.ListpeerchannelsChannels_DUALOPEND_OPEN_COMMITTED),
		int32(rpc.ListpeerchannelsChannels_DUALOPEND_OPEN_COMMIT_READY),
	}
	CLOSING_STATUSES = []int32{
		int32(rpc.ListpeerchannelsChannels_CHANNELD_SHUTTING_DOWN),
		int32(rpc.ListpeerchannelsChannels_CLOSINGD_SIGEXCHANGE),
		int32(rpc.ListpeerchannelsChannels_CLOSINGD_COMPLETE),
		int32(rpc.ListpeerchannelsChannels_AWAITING_UNILATERAL),
		int32(rpc.ListpeerchannelsChannels_FUNDING_SPEND_SEEN),
		int32(rpc.ListpeerchannelsChannels_ONCHAIN),
	}
)

func NewClnClient(config *config.ClnConfig) (*ClnClient, error) {
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM([]byte(config.CaCert)) {
		return nil, fmt.Errorf("failed to add grpc server CA's certificate")
	}

	clientCert, err := tls.X509KeyPair(
		[]byte(config.ClientCert),
		[]byte(config.ClientKey),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create X509 key pair: %w", err)
	}

	tlsConfig := &tls.Config{
		RootCAs:      certPool,
		Certificates: []tls.Certificate{clientCert},
	}

	tlsCredentials := credentials.NewTLS(tlsConfig)

	conn, err := grpc.Dial(
		config.GrpcAddress,
		grpc.WithTransportCredentials(tlsCredentials),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to CLN gRPC: %w", err)
	}

	client := rpc.NewNodeClient(conn)
	return &ClnClient{
		client: client,
	}, nil
}

func (c *ClnClient) GetInfo() (*lightning.GetInfoResult, error) {
	info, err := c.client.Getinfo(context.Background(), &rpc.GetinfoRequest{})
	if err != nil {
		log.Printf("CLN: client.GetInfo() error: %v", err)
		return nil, err
	}

	var alias string
	if info.Alias != nil {
		alias = *info.Alias
	}
	return &lightning.GetInfoResult{
		Alias:  alias,
		Pubkey: hex.EncodeToString(info.Id),
	}, nil
}

func (c *ClnClient) IsConnected(destination []byte) (bool, error) {
	pubKey := hex.EncodeToString(destination)
	peers, err := c.client.ListPeers(context.Background(), &rpc.ListpeersRequest{
		Id: destination,
	})
	if err != nil {
		log.Printf("CLN: client.ListPeers(%v) error: %v", pubKey, err)
		return false, fmt.Errorf("CLN: client.ListPeers(%v) error: %w", pubKey, err)
	}

	if len(peers.Peers) == 0 {
		return false, nil
	}

	peer := peers.Peers[0]
	if peer.Connected {
		log.Printf("CLN: destination online: %x", destination)
		return true, nil
	}

	log.Printf("CLN: destination offline: %x", destination)
	return false, nil
}

func (c *ClnClient) OpenChannel(req *lightning.OpenChannelRequest) (*wire.OutPoint, error) {
	var minDepth uint32 = 0
	announce := false
	var rate *rpc.Feerate
	if req.FeeSatPerVByte != nil {
		rate = &rpc.Feerate{
			Style: &rpc.Feerate_Perkb{
				Perkb: uint32(*req.FeeSatPerVByte * 1000),
			},
		}
	} else if req.TargetConf != nil {
		if *req.TargetConf < 3 {
			rate = &rpc.Feerate{
				Style: &rpc.Feerate_Urgent{
					Urgent: true,
				},
			}
		} else if *req.TargetConf < 30 {
			rate = &rpc.Feerate{
				Style: &rpc.Feerate_Normal{
					Normal: true,
				},
			}
		} else {
			rate = &rpc.Feerate{
				Style: &rpc.Feerate_Slow{
					Slow: true,
				},
			}
		}
	}

	fundResult, err := c.client.FundChannel(
		context.Background(),
		&rpc.FundchannelRequest{
			Id: req.Destination,
			Amount: &rpc.AmountOrAll{
				Value: &rpc.AmountOrAll_Amount{
					Amount: &rpc.Amount{
						Msat: req.CapacitySat * 1000,
					},
				},
			},
			Feerate:  rate,
			Announce: &announce,
			Minconf:  req.MinConfs,
			Mindepth: &minDepth,
			Reserve: &rpc.Amount{
				Msat: 0,
			},
		},
	)

	if err != nil {
		log.Printf("CLN: client.FundChannel(%x, %v) error: %v", req.Destination, req.CapacitySat, err)
		if e, ok := status.FromError(err); ok {
			if strings.Contains(e.Message(), "code: Some(301)") {
				return nil, fmt.Errorf("not enough funds: %w", err)
			}
		}
		return nil, err
	}

	channelPoint, err := lightning.NewOutPoint(reverseBytes(fundResult.Txid), fundResult.Outnum)
	if err != nil {
		log.Printf("CLN: NewOutPoint(%x, %d) error: %v", fundResult.Txid,
			fundResult.Outnum, err)
		return nil, err
	}

	if req.RoutingPolicy != nil {
		var delay uint32 = 0

		_, err = c.client.SetChannel(context.Background(), &rpc.SetchannelRequest{
			Id:           hex.EncodeToString(fundResult.ChannelId),
			Feebase:      &rpc.Amount{Msat: req.RoutingPolicy.BaseMsat},
			Feeppm:       &req.RoutingPolicy.Ppm,
			Enforcedelay: &delay,
		})
		if err != nil {
			log.Printf("CLN: client.SetChannel error: %v. Ignoring error and continue funding flow.", err)
		}
	}

	return channelPoint, nil
}

func (c *ClnClient) GetChannel(peerID []byte, channelPoint wire.OutPoint) (*lightning.GetChannelResult, error) {
	pubkey := hex.EncodeToString(peerID)
	channels, err := c.client.ListPeerChannels(
		context.Background(),
		&rpc.ListpeerchannelsRequest{
			Id: peerID,
		},
	)
	if err != nil {
		log.Printf("CLN: client.ListPeerChannels(%s) error: %v", pubkey, err)
		return nil, err
	}

	for _, c := range channels.Channels {
		if c.State == nil {
			log.Printf("Channel '%+v' with peer '%x' doesn't have a state (yet).",
				c.ShortChannelId, c.PeerId)
			continue
		}
		state := int32(*c.State)
		log.Printf("getChannel destination: %s, scid: %+v, local alias: %+v, "+
			"FundingTxID:%x, State:%+v ", pubkey, c.ShortChannelId,
			c.Alias.Local, c.FundingTxid, c.State)
		if slices.Contains(OPEN_STATUSES, state) &&
			bytes.Equal(reverseBytes(c.FundingTxid), channelPoint.Hash[:]) {

			aliasScid, confirmedScid, err := mapScidsFromChannel(c)
			if err != nil {
				return nil, err
			}
			return &lightning.GetChannelResult{
				AliasScid:       aliasScid,
				ConfirmedScid:   confirmedScid,
				HtlcMinimumMsat: c.MinimumHtlcOutMsat.Msat,
			}, nil
		}
	}

	log.Printf("No channel found: getChannel(%v, %v)", pubkey,
		channelPoint.Hash.String())
	return nil, fmt.Errorf("no channel found")
}

func (c *ClnClient) GetClosedChannels(
	nodeID string,
	channelPoints map[string]uint64,
) (map[string]uint64, error) {
	r := make(map[string]uint64)
	if len(channelPoints) == 0 {
		return r, nil
	}

	nodeIDBytes, err := hex.DecodeString(nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nodeid %s", nodeID)
	}

	channels, err := c.client.ListPeerChannels(
		context.Background(),
		&rpc.ListpeerchannelsRequest{
			Id: nodeIDBytes,
		},
	)
	if err != nil {
		log.Printf("CLN: client.ListPeerChannels(%s) error: %v", nodeID, err)
		return nil, err
	}

	lookup := make(map[string]uint64)
	for _, c := range channels.Channels {
		if c.State == nil {
			log.Printf("Channel '%+v' with peer '%x' doesn't have a state (yet).",
				c.ShortChannelId, c.PeerId)
			continue
		}
		state := int32(*c.State)

		if slices.Contains(CLOSING_STATUSES, state) {
			if c.ShortChannelId == nil {
				log.Printf("CLN: GetClosedChannels. Channel does not have "+
					"ShortChannelId. %x:%d", c.FundingTxid, c.FundingOutnum)
				continue
			}
			scid, err := lightning.NewShortChannelIDFromString(*c.ShortChannelId)
			if err != nil {
				log.Printf("CLN: GetClosedChannels NewShortChannelIDFromString(%v) error: %v", c.ShortChannelId, err)
				continue
			}

			if c.FundingOutnum == nil {
				log.Printf("CLN: GetClosedChannels. Channel does not have "+
					"FundingOutnum. %s %x:%d", *c.ShortChannelId, c.FundingTxid,
					c.FundingOutnum)
				continue
			}

			cp, err := lightning.NewOutPoint(reverseBytes(c.FundingTxid), *c.FundingOutnum)
			if err != nil {
				log.Printf("CLN: GetClosedChannels lightning.NewOutPoint(%x, %d) error: %v",
					c.FundingTxid,
					c.FundingOutnum,
					err)
				continue
			}
			lookup[cp.String()] = uint64(*scid)
		}
	}

	for c, h := range channelPoints {
		if _, ok := lookup[c]; !ok {
			r[c] = h
		}
	}

	return r, nil
}

func (c *ClnClient) GetPeerId(scid *lightning.ShortChannelID) ([]byte, error) {
	scidStr := scid.ToString()
	channels, err := c.client.ListPeerChannels(
		context.Background(),
		&rpc.ListpeerchannelsRequest{},
	)
	if err != nil {
		return nil, err
	}

	var dest []byte
	for _, ch := range channels.Channels {
		if ch.ShortChannelId != nil && *ch.ShortChannelId == scidStr {
			dest = ch.PeerId
			break
		}

		if ch.Alias != nil && ch.Alias.Local != nil && *ch.Alias.Local == scidStr {
			dest = ch.PeerId
			break
		}
	}

	if dest == nil {
		return nil, nil
	}

	return dest, nil
}

var pollingInterval = 400 * time.Millisecond

func (c *ClnClient) WaitOnline(peerID []byte, deadline time.Time) error {
	for {
		connected, err := c.IsConnected(peerID)
		if err == nil && connected {
			return nil
		}

		select {
		case <-time.After(time.Until(deadline)):
			return fmt.Errorf("timeout")
		case <-time.After(pollingInterval):
		}
	}
}

func (c *ClnClient) WaitChannelActive(peerID []byte, deadline time.Time) error {
	for {
		peer, err := c.client.ListPeerChannels(
			context.Background(),
			&rpc.ListpeerchannelsRequest{
				Id: peerID,
			},
		)
		if err == nil {
			for _, c := range peer.Channels {
				if c.State == nil {
					continue
				}

				if slices.Contains(OPEN_STATUSES, int32(*c.State)) {
					return nil
				}
			}
		}

		select {
		case <-time.After(time.Until(deadline)):
			return fmt.Errorf("timeout")
		case <-time.After(pollingInterval):
		}
	}
}

func (c *ClnClient) ListChannels() ([]*lightning.Channel, error) {
	channels, err := c.client.ListPeerChannels(
		context.Background(),
		&rpc.ListpeerchannelsRequest{},
	)
	if err != nil {
		return nil, err
	}

	result := make([]*lightning.Channel, len(channels.Channels))
	for i, channel := range channels.Channels {
		aliasScid, confirmedScid, err := mapScidsFromChannel(channel)
		if err != nil {
			return nil, err
		}

		var outpoint *wire.OutPoint
		if channel.FundingTxid != nil && len(channel.FundingTxid) > 0 && channel.FundingOutnum != nil {
			outpoint, _ = lightning.NewOutPoint(reverseBytes(channel.FundingTxid), *channel.FundingOutnum)
		}
		if outpoint == nil {
			log.Printf("cln.ListChannels returned channel without outpoint: %+v", channel)
			continue
		}
		result[i] = &lightning.Channel{
			AliasScid:     aliasScid,
			ConfirmedScid: confirmedScid,
			ChannelPoint:  outpoint,
			PeerId:        channel.PeerId,
		}
	}

	return result, nil
}

func mapScidsFromChannel(c *rpc.ListpeerchannelsChannels) (*lightning.ShortChannelID, *lightning.ShortChannelID, error) {
	var confirmedScid *lightning.ShortChannelID
	var aliasScid *lightning.ShortChannelID
	var err error
	if c.ShortChannelId != nil {
		confirmedScid, err = lightning.NewShortChannelIDFromString(*c.ShortChannelId)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse scid '%+v': %w", c.ShortChannelId, err)
		}
	}

	if c.Alias != nil && c.Alias.Local != nil {
		aliasScid, err = lightning.NewShortChannelIDFromString(*c.Alias.Local)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse scid '%+v': %w", c.Alias.Local, err)
		}
	}

	return aliasScid, confirmedScid, nil
}

func reverseBytes(b []byte) []byte {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	return b
}
