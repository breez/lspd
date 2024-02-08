package cln

import (
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/breez/lspd/lightning"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/elementsproject/glightning/glightning"
	"github.com/elementsproject/glightning/jrpc2"
	"golang.org/x/exp/slices"
)

type ClnClient struct {
	socketPath string
	client     *glightning.Lightning
	mtx        sync.Mutex
}

var (
	OPEN_STATUSES    = []string{"CHANNELD_NORMAL"}
	PENDING_STATUSES = []string{"OPENINGD", "CHANNELD_AWAITING_LOCKIN"}
	CLOSING_STATUSES = []string{"CHANNELD_SHUTTING_DOWN", "CLOSINGD_SIGEXCHANGE", "CLOSINGD_COMPLETE", "AWAITING_UNILATERAL", "FUNDING_SPEND_SEEN", "ONCHAIN"}
	CLOSED_STATUSES  = []string{"CLOSED"}
)

func NewClnClient(socketPath string) (*ClnClient, error) {
	client, err := newGlightningClient(socketPath)
	if err != nil {
		return nil, err
	}
	return &ClnClient{
		socketPath: socketPath,
		client:     client,
	}, nil
}

func newGlightningClient(socketPath string) (*glightning.Lightning, error) {
	rpcFile := filepath.Base(socketPath)
	if rpcFile == "" || rpcFile == "." {
		return nil, fmt.Errorf("invalid socketPath '%s'", socketPath)
	}
	lightningDir := filepath.Dir(socketPath)
	if lightningDir == "" || lightningDir == "." {
		return nil, fmt.Errorf("invalid socketPath '%s'", socketPath)
	}

	client := glightning.NewLightning()
	client.SetTimeout(60)
	err := client.StartUp(rpcFile, lightningDir)
	return client, err
}

func (c *ClnClient) getClient() (*glightning.Lightning, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.client.IsUp() {
		return c.client, nil
	}

	var err error
	c.client, err = newGlightningClient(c.socketPath)
	if err != nil {
		return nil, err
	}
	if c.client.IsUp() {
		return c.client, nil
	}

	return nil, fmt.Errorf("cln is not accessible")
}

func (c *ClnClient) GetInfo() (*lightning.GetInfoResult, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	info, err := client.GetInfo()
	if err != nil {
		log.Printf("CLN: client.GetInfo() error: %v", err)
		return nil, err
	}

	return &lightning.GetInfoResult{
		Alias:  info.Alias,
		Pubkey: info.Id,
	}, nil
}

func (c *ClnClient) IsConnected(destination []byte) (bool, error) {
	client, err := c.getClient()
	if err != nil {
		return false, err
	}

	pubKey := hex.EncodeToString(destination)
	peer, err := client.GetPeer(pubKey)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}

		log.Printf("CLN: client.GetPeer(%v) error: %v", pubKey, err)
		return false, fmt.Errorf("CLN: client.GetPeer(%v) error: %w", pubKey, err)
	}

	if peer.Connected {
		log.Printf("CLN: destination online: %x", destination)
		return true, nil
	}

	log.Printf("CLN: destination offline: %x", destination)
	return false, nil
}

func (c *ClnClient) OpenChannel(req *lightning.OpenChannelRequest) (*wire.OutPoint, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	pubkey := hex.EncodeToString(req.Destination)
	var minConfs *uint16
	if req.MinConfs != nil {
		m := uint16(*req.MinConfs)
		minConfs = &m
	}
	var minDepth uint16 = 0
	var rate *glightning.FeeRate
	if req.FeeSatPerVByte != nil {
		rate = &glightning.FeeRate{
			Rate:  uint(*req.FeeSatPerVByte * 1000),
			Style: glightning.PerKb,
		}
	} else if req.TargetConf != nil {
		if *req.TargetConf < 3 {
			rate = &glightning.FeeRate{
				Directive: glightning.Urgent,
			}
		} else if *req.TargetConf < 30 {
			rate = &glightning.FeeRate{
				Directive: glightning.Normal,
			}
		} else {
			rate = &glightning.FeeRate{
				Directive: glightning.Slow,
			}
		}
	}

	fundResult, err := client.FundChannelExt(
		pubkey,
		glightning.NewSat(int(req.CapacitySat)),
		rate,
		false,
		minConfs,
		glightning.NewMsat(0),
		&minDepth,
		glightning.NewMsat(0),
	)

	if err != nil {
		log.Printf("CLN: client.FundChannelExt(%v, %v) error: %v", pubkey, req.CapacitySat, err)
		rpcError, ok := err.(*jrpc2.RpcError)
		if ok && rpcError.Code == 301 {
			return nil, fmt.Errorf("not enough funds: %w", err)
		}
		return nil, err
	}

	fundingTxId, err := chainhash.NewHashFromStr(fundResult.FundingTxId)
	if err != nil {
		log.Printf("CLN: chainhash.NewHashFromStr(%s) error: %v", fundResult.FundingTxId, err)
		return nil, err
	}

	channelPoint, err := lightning.NewOutPoint(fundingTxId[:], uint32(fundResult.FundingTxOutputNum))
	if err != nil {
		log.Printf("CLN: NewOutPoint(%s, %d) error: %v", fundingTxId.String(), fundResult.FundingTxOutputNum, err)
		return nil, err
	}

	return channelPoint, nil
}

func (c *ClnClient) GetChannel(peerID []byte, channelPoint wire.OutPoint) (*lightning.GetChannelResult, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	pubkey := hex.EncodeToString(peerID)
	channels, err := client.GetPeerChannels(pubkey)
	if err != nil {
		log.Printf("CLN: client.GetPeer(%s) error: %v", pubkey, err)
		return nil, err
	}

	fundingTxID := channelPoint.Hash.String()
	for _, c := range channels {
		log.Printf("getChannel destination: %s, Short channel id: %v, local alias: %v , FundingTxID:%v, State:%v ", pubkey, c.ShortChannelId, c.Alias.Local, c.FundingTxId, c.State)
		if slices.Contains(OPEN_STATUSES, c.State) && c.FundingTxId == fundingTxID {
			confirmedChanID, err := lightning.NewShortChannelIDFromString(c.ShortChannelId)
			if err != nil {
				fmt.Printf("NewShortChannelIDFromString %v error: %v", c.ShortChannelId, err)
				return nil, err
			}
			initialChanID, err := lightning.NewShortChannelIDFromString(c.Alias.Local)
			if err != nil {
				fmt.Printf("NewShortChannelIDFromString %v error: %v", c.Alias.Local, err)
				return nil, err
			}
			return &lightning.GetChannelResult{
				InitialChannelID:   *initialChanID,
				ConfirmedChannelID: *confirmedChanID,
				HtlcMinimumMsat:    c.MinimumHtlcOutMsat.MSat(),
			}, nil
		}
	}

	log.Printf("No channel found: getChannel(%v, %v)", pubkey, fundingTxID)
	return nil, fmt.Errorf("no channel found")
}

func (c *ClnClient) GetClosedChannels(nodeID string, channelPoints map[string]uint64) (map[string]uint64, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	r := make(map[string]uint64)
	if len(channelPoints) == 0 {
		return r, nil
	}

	channels, err := client.GetPeerChannels(nodeID)
	if err != nil {
		log.Printf("CLN: client.GetPeer(%s) error: %v", nodeID, err)
		return nil, err
	}

	lookup := make(map[string]uint64)
	for _, c := range channels {
		if slices.Contains(CLOSING_STATUSES, c.State) {
			cid, err := lightning.NewShortChannelIDFromString(c.ShortChannelId)
			if err != nil {
				log.Printf("CLN: GetClosedChannels NewShortChannelIDFromString(%v) error: %v", c.ShortChannelId, err)
				continue
			}

			outnum := uint64(*cid) & 0xFFFFFF
			cp := fmt.Sprintf("%s:%d", c.FundingTxId, outnum)
			lookup[cp] = uint64(*cid)
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
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	scidStr := scid.ToString()
	channels, err := client.ListPeerChannels()
	if err != nil {
		return nil, err
	}

	var dest *string
	for _, ch := range channels {
		if (ch.Alias != nil && (ch.Alias.Local == scidStr ||
			ch.Alias.Remote == scidStr)) ||
			ch.ShortChannelId == scidStr {
			dest = &ch.PeerId
			break
		}
	}

	if dest == nil {
		return nil, nil
	}

	return hex.DecodeString(*dest)
}

var pollingInterval = 400 * time.Millisecond

func (c *ClnClient) WaitOnline(peerID []byte, deadline time.Time) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}

	peerIDStr := hex.EncodeToString(peerID)
	for {
		peer, err := client.GetPeer(peerIDStr)
		if err == nil && peer.Connected {
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
	return nil
}
