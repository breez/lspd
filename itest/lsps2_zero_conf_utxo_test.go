package itest

import (
	"log"
	"time"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
	"github.com/breez/lspd/lsps2"
	"github.com/stretchr/testify/assert"
)

func testLsps2ZeroConfUtxo(p *testParams) {
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

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(node)

	// Make sure everything is activated.
	<-time.After(htlcInterceptorDelay)

	log.Printf("Calling lsps2.get_info")
	info := lsp.Client(0).Lsps2GetInfo(p.BreezClient(), lsps2.GetInfoRequest{
		Token: &WorkingToken,
	})

	outerAmountMsat := uint64(2100000)
	innerAmountMsat := Lsps2CalculateInnerAmountMsat(p.Harness(), outerAmountMsat, info.OpeningFeeParamsMenu[0])
	p.BreezClient().SetHtlcAcceptor(innerAmountMsat)

	log.Printf("Calling lsps2.buy")
	buyResp := lsp.Client(0).Lsps2Buy(p.BreezClient(), lsps2.BuyRequest{
		OpeningFeeParams: *info.OpeningFeeParamsMenu[0],
		PaymentSizeMsat:  &outerAmountMsat,
	})

	log.Printf("Adding bob's invoices")
	description := "Please pay me"
	_, outerInvoice := GenerateLsps2Invoices(p.BreezClient(),
		generateInvoicesRequest{
			innerAmountMsat: innerAmountMsat,
			outerAmountMsat: outerAmountMsat,
			description:     description,
			lsp:             lsp.Client(0),
		},
		buyResp.JitChannelScid)

	// TODO: Fix race waiting for htlc interceptor.
	log.Printf("Waiting %v to allow htlc interceptor to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)
	log.Printf("Alice paying")
	route := constructRoute(node, p.BreezClient().Node(), channelId, lntest.NewShortChanIDFromString(buyResp.JitChannelScid), outerAmountMsat)
	route.Hops[0].Delay += 2
	payResp, err := alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route)
	lntest.CheckError(p.t, err)
	bobInvoice := p.BreezClient().Node().GetInvoice(payResp.PaymentHash)

	assert.Equal(p.t, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(p.t, innerAmountMsat, bobInvoice.AmountReceivedMsat)

	// Make sure there's not accidently a block mined in between
	finalHeight := p.m.GetBlockHeight()
	assert.Equal(p.t, initialHeight, finalHeight)
}
