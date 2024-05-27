package itest

import (
	"flag"
	"fmt"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
)

var clnPluginExec = flag.String(
	"clnpluginexec", "", "full path to cln plugin wrapper binary",
)

type ClnLspdNode struct {
	Node          *lntest.ClnNode
	PluginAddress string
}

func NewClnLspdNode(h *lntest.TestHarness, m *lntest.Miner, name string) (lntest.LightningNode, *config.ClnConfig) {
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
	}
	node := lntest.NewClnNode(h, m, name, args...)
	return node, &config.ClnConfig{
		PluginAddress: pluginAddress,
		GrpcAddress:   fmt.Sprintf("localhost:%d", node.GrpcPort()),
		CaCert:        node.CaCertPath(),
		ClientCert:    node.ClientCertPath(),
		ClientKey:     node.ClientKeyPath(),
	}
}
