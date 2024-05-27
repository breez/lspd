package itest

import (
	"log"
	"time"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

func testOpenZeroConfUtxo(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)

	minConfs := uint32(0)
	node, lsp := p.lspFunc(p.h, p.m, p.mem, &config.NodeConfig{MinConfs: &minConfs})
	node.Start()
	lsp.Start()

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(node, &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
	})
	channelId := alice.WaitForChannelReady(channel)

	tempaddr := node.GetNewAddress()
	p.m.SendToAddress(tempaddr, 210000)
	reserveaddr := node.GetNewAddress()
	p.m.SendToAddress(reserveaddr, 50000)
	p.m.MineBlocks(6)
	node.WaitForSync()

	initialHeight := p.m.GetBlockHeight()
	addr := node.GetNewAddress()
	node.SendToAddress(addr, 200000)

	log.Printf("Adding bob's invoices")
	outerAmountMsat := uint64(2100000)
	innerAmountMsat := calculateInnerAmountMsat(p.Harness(), outerAmountMsat, nil)
	description := "Please pay me"
	innerInvoice, outerInvoice := GenerateInvoices(p.BreezClient(),
		generateInvoicesRequest{
			innerAmountMsat: innerAmountMsat,
			outerAmountMsat: outerAmountMsat,
			description:     description,
			lsp:             lsp.Client(0),
		})
	p.BreezClient().SetHtlcAcceptor(innerAmountMsat)

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(node)

	log.Printf("Registering payment with lsp")
	lsp.Client(0).RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(innerAmountMsat),
	}, false)

	// TODO: Fix race waiting for htlc interceptor.
	log.Printf("Waiting %v to allow htlc interceptor to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)
	log.Printf("Alice paying")
	route := constructRoute(node, p.BreezClient().Node(), channelId, lntest.NewShortChanIDFromString("1x0x0"), outerAmountMsat)
	payResp, err := alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route)
	lntest.CheckError(p.t, err)
	bobInvoice := p.BreezClient().Node().GetInvoice(payResp.PaymentHash)

	assert.Equal(p.t, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(p.t, innerAmountMsat, bobInvoice.AmountReceivedMsat)

	// Make sure there's not accidently a block mined in between
	finalHeight := p.m.GetBlockHeight()
	assert.Equal(p.t, initialHeight, finalHeight)
}
