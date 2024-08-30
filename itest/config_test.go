package itest

import (
	"encoding/hex"
	"log"
	"testing"
	"time"

	"github.com/breez/lspd/itest/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

func TestConfigParameters(t *testing.T) {
	deadline, _ := t.Deadline()
	h := lntest.NewTestHarness(t, deadline)
	defer h.TearDown()

	m := lntest.NewMiner(h)
	m.Start()

	mem := NewMempoolApi(h)
	mem.Start()

	lsp := NewClnLspdNode(h, m, mem, "lsp", nil)
	lsp.Start()

	log.Printf("Waiting %v to allow lsp server to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)

	info, err := lsp.Rpc().ChannelInformation(
		h.Ctx,
		&lspd.ChannelInformationRequest{},
	)

	if err != nil {
		t.Fatalf("failed to get channelinformation: %v", err)
	}

	assert.Equal(t, hex.EncodeToString(lsp.LightningNode().NodeId()), info.Pubkey)
	assert.Equal(t, "lsp", info.Name)
}
