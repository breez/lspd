package itest

import (
	"context"
	"encoding/hex"
	"flag"
	"log"
	"sync"
	"time"

	"github.com/breez/lntest"
	"github.com/breez/lntest/lnd"
	"github.com/lightningnetwork/lnd/lnwire"
)

var lndMobileExecutable = flag.String(
	"lndmobileexec", "", "full path to lnd mobile binary",
)

type lndBreezClient struct {
	name           string
	harness        *lntest.TestHarness
	node           *lntest.LndNode
	customMsgQueue chan *lntest.CustomMsgRequest
	cancel         context.CancelFunc
	mtx            sync.Mutex
}

func newLndBreezClient(h *lntest.TestHarness, m *lntest.Miner, name string) BreezClient {
	lnd := lntest.NewLndNodeFromBinary(h, m, name, *lndMobileExecutable,
		"--protocol.zero-conf",
		"--protocol.option-scid-alias",
		"--bitcoin.defaultchanconfs=0",
	)

	c := &lndBreezClient{
		name:    name,
		harness: h,
		node:    lnd,
	}
	h.AddStoppable(c)
	return c
}

func (c *lndBreezClient) Name() string {
	return c.name
}

func (c *lndBreezClient) Harness() *lntest.TestHarness {
	return c.harness
}

func (c *lndBreezClient) Node() lntest.LightningNode {
	return c.node
}

func (c *lndBreezClient) Start() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.node.IsStarted() {
		return
	}

	c.node.Start()

	ctx, cancel := context.WithCancel(c.harness.Ctx)
	c.cancel = cancel
	go c.startChannelAcceptor(ctx)
	c.customMsgQueue = make(chan *lntest.CustomMsgRequest, 100)
	c.startCustomMsgListener(ctx)
}

func (c *lndBreezClient) Stop() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	// Stop the channel acceptor
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	return c.node.Stop()
}

func (c *lndBreezClient) ResetHtlcAcceptor() {

}

func (c *lndBreezClient) SetHtlcAcceptor(totalMsat uint64) {
	// No need for a htlc acceptor in the LND breez client
}

func (c *lndBreezClient) startCustomMsgListener(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}

			if !c.node.IsStarted() {
				log.Printf("%s: cannot listen to custom messages, node is not started.", c.name)
				break
			}

			listener, err := c.node.LightningClient().SubscribeCustomMessages(
				ctx,
				&lnd.SubscribeCustomMessagesRequest{},
			)
			if err != nil {
				log.Printf("%s: client.SubscribeCustomMessages() error: %v", c.name, err)
				break
			}
			for {
				if ctx.Err() != nil {
					return
				}
				msg, err := listener.Recv()
				if err != nil {
					log.Printf("%s: listener.Recv() error: %v", c.name, err)
					break
				}

				c.customMsgQueue <- &lntest.CustomMsgRequest{
					PeerId: hex.EncodeToString(msg.Peer),
					Type:   msg.Type,
					Data:   msg.Data,
				}
			}
		}
	}()
}

func (c *lndBreezClient) ReceiveCustomMessage() *lntest.CustomMsgRequest {
	msg := <-c.customMsgQueue
	return msg
}

func (c *lndBreezClient) startChannelAcceptor(ctx context.Context) error {
	client, err := c.node.LightningClient().ChannelAcceptor(ctx)
	if err != nil {
		c.harness.T.Fatalf("%s: failed to create channel acceptor: %v", c.name, err)
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		request, err := client.Recv()
		if err != nil {
			return err
		}

		private := request.ChannelFlags&uint32(lnwire.FFAnnounceChannel) == 0
		resp := &lnd.ChannelAcceptResponse{
			PendingChanId: request.PendingChanId,
			Accept:        private,
		}
		if request.WantsZeroConf {
			resp.MinAcceptDepth = 0
			resp.ZeroConf = true
		}

		err = client.Send(resp)
		if err != nil {
			c.harness.T.Fatalf("%s: failed to send acceptor response: %v", c.name, err)
		}
	}
}
