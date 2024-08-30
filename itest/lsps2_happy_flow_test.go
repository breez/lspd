package itest

import (
	"log"
	"time"

	"github.com/breez/lspd/itest/lntest"
	"github.com/breez/lspd/lsps2"
	"github.com/stretchr/testify/assert"
)

func testLsps2HappyFlow(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)
	p.lsp.LightningNode().Fund(10000000)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(p.lsp.LightningNode(), &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
	})
	alice.WaitForChannelReady(channel)

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(p.lsp.LightningNode())

	// Make sure everything is activated.
	<-time.After(htlcInterceptorDelay)

	log.Printf("Calling lsps2.get_info")
	info := Lsps2GetInfo(p.BreezClient(), p.Lsp(), lsps2.GetInfoRequest{
		Token: &WorkingToken,
	})

	outerAmountMsat := uint64(2100000)
	innerAmountMsat := lsps2CalculateInnerAmountMsat(p.lsp, outerAmountMsat, info.OpeningFeeParamsMenu[0])
	p.BreezClient().SetHtlcAcceptor(innerAmountMsat)

	log.Printf("Calling lsps2.buy")
	buyResp := Lsps2Buy(p.BreezClient(), p.Lsp(), lsps2.BuyRequest{
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
			lsp:             p.lsp,
		},
		buyResp.JitChannelScid)

	// TODO: Fix race waiting for htlc interceptor.
	log.Printf("Waiting %v to allow htlc interceptor to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)
	log.Printf("Alice paying")
	payResp := alice.Pay(outerInvoice.bolt11)
	bobInvoice := p.BreezClient().Node().GetInvoice(payResp.PaymentHash)

	assert.Equal(p.t, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(p.t, innerAmountMsat, bobInvoice.AmountReceivedMsat)

	// Make sure capacity is correct
	chans := p.BreezClient().Node().GetChannels()
	assert.Equal(p.t, 1, len(chans))
	c := chans[0]
	AssertChannelCapacity(p.t, innerAmountMsat, c.CapacityMsat)

	assert.Equal(p.t, c.RemoteReserveMsat, c.CapacityMsat/100)
	log.Printf("local reserve: %d, remote reserve: %d", c.LocalReserveMsat, c.RemoteReserveMsat)
	assert.Zero(p.t, c.LocalReserveMsat)
}
