package itest

import (
	"fmt"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
)

func NewLndLspdNode(h *lntest.TestHarness, m *lntest.Miner, name string) (*lntest.LndNode, *config.LndConfig) {
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

	node := lntest.NewLndNode(h, m, name, args...)
	return node, &config.LndConfig{
		Address:  node.GrpcHost(),
		Cert:     string(node.TlsCertPath()),
		Macaroon: string(node.MacaroonPath()),
	}
}
