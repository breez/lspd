package itest

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
	"github.com/breez/lspd/notifications"
	lspd "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	ecies "github.com/ecies/go/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type LndLspNode struct {
	harness       *lntest.TestHarness
	lightningNode *lntest.LndNode
	lspBase       *lspBase
	runtime       *lndLspNodeRuntime
	mtx           sync.Mutex
}

type lndLspNodeRuntime struct {
	rpc             lspd.ChannelOpenerClient
	notificationRpc notifications.NotificationsClient
	cleanups        []*lntest.Cleanup
}

func NewLndLspdNode(h *lntest.TestHarness, m *lntest.Miner, mem *mempoolApi, name string, nodeConfig *config.NodeConfig) LspNode {
	args := []string{
		"--protocol.zero-conf",
		"--protocol.option-scid-alias",
		"--requireinterceptor",
		"--bitcoin.defaultchanconfs=0",
		fmt.Sprintf("--bitcoin.chanreservescript=\"0 if (chanAmt != %d) else chanAmt/100\"", publicChanAmount),
		fmt.Sprintf("--bitcoin.basefee=%d", lspBaseFeeMsat),
		fmt.Sprintf("--bitcoin.feerate=%d", lspFeeRatePpm),
		fmt.Sprintf("--bitcoin.timelockdelta=%d", lspCltvDelta),
	}

	lightningNode := lntest.NewLndNode(h, m, name, args...)
	lnd := &config.LndConfig{
		Address:  lightningNode.GrpcHost(),
		Cert:     string(lightningNode.TlsCert()),
		Macaroon: hex.EncodeToString(lightningNode.Macaroon()),
	}
	lspBase, err := newLspd(h, mem, name, nodeConfig, lnd, nil)
	if err != nil {
		h.T.Fatalf("failed to initialize lspd")
	}

	lspNode := &LndLspNode{
		harness:       h,
		lightningNode: lightningNode,
		lspBase:       lspBase,
	}

	h.AddStoppable(lspNode)
	return lspNode
}

func (c *LndLspNode) Start() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	var cleanups []*lntest.Cleanup
	c.lspBase.Start(c.lightningNode, func(conf *config.NodeConfig) {
		conf.Lnd = &config.LndConfig{
			Address:  c.lightningNode.GrpcHost(),
			Cert:     string(c.lightningNode.TlsCert()),
			Macaroon: hex.EncodeToString(c.lightningNode.Macaroon()),
		}
	})

	conn, err := grpc.Dial(
		c.lspBase.grpcAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(&token{token: "hello"}),
	)
	if err != nil {
		lntest.PerformCleanup(cleanups)
		c.harness.T.Fatalf("failed to create grpc connection: %v", err)
	}
	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: grpc conn", c.lspBase.name),
		Fn:   conn.Close,
	})

	client := lspd.NewChannelOpenerClient(conn)
	notifyClient := notifications.NewNotificationsClient(conn)
	c.runtime = &lndLspNodeRuntime{
		rpc:             client,
		notificationRpc: notifyClient,
		cleanups:        cleanups,
	}
}

func (c *LndLspNode) Stop() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.runtime != nil {
		lntest.PerformCleanup(c.runtime.cleanups)
	}

	c.lspBase.StopRuntime()
	c.runtime = nil
	return nil
}

func (c *LndLspNode) Harness() *lntest.TestHarness {
	return c.harness
}

func (c *LndLspNode) PublicKey() *btcec.PublicKey {
	return c.lspBase.pubkey
}

func (c *LndLspNode) EciesPublicKey() *ecies.PublicKey {
	return c.lspBase.eciesPubkey
}

func (c *LndLspNode) Rpc() lspd.ChannelOpenerClient {
	return c.runtime.rpc
}

func (c *LndLspNode) NotificationsRpc() notifications.NotificationsClient {
	return c.runtime.notificationRpc
}

func (l *LndLspNode) NodeId() []byte {
	return l.lightningNode.NodeId()
}

func (l *LndLspNode) LightningNode() lntest.LightningNode {
	return l.lightningNode
}

func (l *LndLspNode) PostgresBackend() *PostgresContainer {
	return l.lspBase.postgresBackend
}
