package itest

import (
	"fmt"
	"sync"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	ecies "github.com/ecies/go/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ClnLspNode struct {
	harness       *lntest.TestHarness
	lightningNode *lntest.ClnNode
	lspBase       *lspBase
	runtime       *clnLspNodeRuntime
	isInitialized bool
	mtx           sync.Mutex
}

type clnLspNodeRuntime struct {
	rpc      lspd.ChannelOpenerClient
	cleanups []*lntest.Cleanup
}

func NewClnLspdNode(h *lntest.TestHarness, m *lntest.Miner, name string) LspNode {
	lspbase, err := newLspd(h, name, "RUN_CLN=true")
	if err != nil {
		h.T.Fatalf("failed to initialize lspd")
	}

	args := []string{
		fmt.Sprintf("--plugin=%s", lspbase.scriptFilePath),
		fmt.Sprintf("--fee-base=%d", lspBaseFeeMsat),
		fmt.Sprintf("--fee-per-satoshi=%d", lspFeeRatePpm),
		fmt.Sprintf("--cltv-delta=%d", lspCltvDelta),
		"--max-concurrent-htlcs=30",
		"--dev-allowdustreserve=true",
	}
	lightningNode := lntest.NewClnNode(h, m, name, args...)

	lspNode := &ClnLspNode{
		harness:       h,
		lightningNode: lightningNode,
		lspBase:       lspbase,
	}

	h.AddStoppable(lspNode)
	return lspNode
}

func (c *ClnLspNode) Start() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	var cleanups []*lntest.Cleanup
	if !c.isInitialized {
		err := c.lspBase.Initialize()
		if err != nil {
			c.harness.T.Fatalf("failed to initialize lsp: %v", err)
		}
		c.isInitialized = true
		cleanups = append(cleanups, &lntest.Cleanup{
			Name: fmt.Sprintf("%s: lsp base", c.lspBase.name),
			Fn:   c.lspBase.Stop,
		})
	}

	c.lightningNode.Start()
	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: lightning node", c.lspBase.name),
		Fn:   c.lightningNode.Stop,
	})
	conn, err := grpc.Dial(
		c.lspBase.grpcAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(&token{token: "hello"}),
	)
	if err != nil {
		lntest.PerformCleanup(cleanups)
		c.harness.T.Fatalf("%s: failed to create grpc connection: %v", c.lspBase.name, err)
	}

	client := lspd.NewChannelOpenerClient(conn)
	c.runtime = &clnLspNodeRuntime{
		rpc:      client,
		cleanups: cleanups,
	}
}

func (c *ClnLspNode) Stop() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.runtime == nil {
		return nil
	}

	lntest.PerformCleanup(c.runtime.cleanups)
	c.runtime = nil
	return nil
}

func (c *ClnLspNode) Harness() *lntest.TestHarness {
	return c.harness
}

func (c *ClnLspNode) PublicKey() *btcec.PublicKey {
	return c.lspBase.pubkey
}

func (c *ClnLspNode) EciesPublicKey() *ecies.PublicKey {
	return c.lspBase.eciesPubkey
}

func (c *ClnLspNode) Rpc() lspd.ChannelOpenerClient {
	return c.runtime.rpc
}

func (l *ClnLspNode) NodeId() []byte {
	return l.lightningNode.NodeId()
}

func (l *ClnLspNode) LightningNode() lntest.LightningNode {
	return l.lightningNode
}

func (l *ClnLspNode) SupportsChargingFees() bool {
	return false
}
