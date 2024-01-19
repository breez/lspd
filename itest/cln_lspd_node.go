package itest

import (
	"flag"
	"fmt"
	"path/filepath"
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

var clnPluginExec = flag.String(
	"clnpluginexec", "", "full path to cln plugin wrapper binary",
)

type ClnLspNode struct {
	harness       *lntest.TestHarness
	lightningNode *lntest.ClnNode
	lspBase       *lspBase
	logFilePath   string
	runtime       *clnLspNodeRuntime
	mtx           sync.Mutex
	pluginBinary  string
	pluginAddress string
}

type clnLspNodeRuntime struct {
	rpc             lspd.ChannelOpenerClient
	notificationRpc notifications.NotificationsClient
	cleanups        []*lntest.Cleanup
}

func NewClnLspdNode(h *lntest.TestHarness, m *lntest.Miner, mem *mempoolApi, name string, nodeConfig *config.NodeConfig) LspNode {
	scriptDir := h.GetDirectory("lspd")
	pluginBinary := *clnPluginExec
	pluginPort, err := lntest.GetPort()
	if err != nil {
		h.T.Fatalf("failed to get port for the htlc interceptor plugin.")
	}
	pluginAddress := fmt.Sprintf("127.0.0.1:%d", pluginPort)

	args := []string{
		fmt.Sprintf("--plugin=%s", pluginBinary),
		fmt.Sprintf("--lsp-listen=%s", pluginAddress),
		fmt.Sprintf("--fee-base=%d", lspBaseFeeMsat),
		fmt.Sprintf("--fee-per-satoshi=%d", lspFeeRatePpm),
		fmt.Sprintf("--cltv-delta=%d", lspCltvDelta),
		"--max-concurrent-htlcs=30",
		"--dev-allowdustreserve=true",
		"--developer",
		"--experimental-splicing",
	}
	lightningNode := lntest.NewClnNode(h, m, name, args...)
	cln := &config.ClnConfig{
		PluginAddress: pluginAddress,
		GrpcAddress:   fmt.Sprintf("localhost:%d", lightningNode.GrpcPort()),
		CaCert:        lightningNode.CaCertPath(),
		ClientCert:    lightningNode.ClientCertPath(),
		ClientKey:     lightningNode.ClientKeyPath(),
	}
	lspbase, err := newLspd(h, mem, name, nodeConfig, nil, cln)
	if err != nil {
		h.T.Fatalf("failed to initialize lspd")
	}

	logFilePath := filepath.Join(scriptDir, "lspd.log")
	h.RegisterLogfile(logFilePath, fmt.Sprintf("lspd-%s", name))

	lspNode := &ClnLspNode{
		harness:       h,
		lightningNode: lightningNode,
		logFilePath:   logFilePath,
		lspBase:       lspbase,
		pluginBinary:  pluginBinary,
		pluginAddress: pluginAddress,
	}

	h.AddStoppable(lspNode)
	return lspNode
}

func (c *ClnLspNode) Start() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	var cleanups []*lntest.Cleanup
	c.lspBase.Start(c.lightningNode, func(nc *config.NodeConfig) {})
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
	notif := notifications.NewNotificationsClient(conn)
	c.runtime = &clnLspNodeRuntime{
		rpc:             client,
		notificationRpc: notif,
		cleanups:        cleanups,
	}
}

func (c *ClnLspNode) Stop() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.runtime != nil {
		lntest.PerformCleanup(c.runtime.cleanups)
	}
	c.lspBase.StopRuntime()
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

func (c *ClnLspNode) NotificationsRpc() notifications.NotificationsClient {
	return c.runtime.notificationRpc
}

func (l *ClnLspNode) NodeId() []byte {
	return l.lightningNode.NodeId()
}

func (l *ClnLspNode) LightningNode() lntest.LightningNode {
	return l.lightningNode
}

func (l *ClnLspNode) PostgresBackend() *PostgresContainer {
	return l.lspBase.postgresBackend
}
