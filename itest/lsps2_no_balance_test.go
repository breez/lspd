package itest

import (
	"log"
	"time"

	"github.com/breez/lspd/itest/lntest"
	"github.com/breez/lspd/lsps2"
	"github.com/stretchr/testify/assert"
)

func testLsps2NoBalance(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(p.Node(), &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
	})
	aliceLspScid := alice.WaitForChannelReady(channel)

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(p.Node())

	// Make sure everything is activated.
	<-time.After(htlcInterceptorDelay)

	log.Printf("Calling lsps2.get_info")
	info := p.Lspd().Client(0).Lsps2GetInfo(p.BreezClient(), lsps2.GetInfoRequest{
		Token: &WorkingToken,
	})

	outerAmountMsat := uint64(2100000)
	innerAmountMsat := Lsps2CalculateInnerAmountMsat(p.Harness(), outerAmountMsat, info.OpeningFeeParamsMenu[0])
	p.BreezClient().SetHtlcAcceptor(innerAmountMsat)

	log.Printf("Calling lsps2.buy")
	buyResp := p.Lspd().Client(0).Lsps2Buy(p.BreezClient(), lsps2.BuyRequest{
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
			lsp:             p.Lspd().Client(0),
		},
		buyResp.JitChannelScid)

	// TODO: Fix race waiting for htlc interceptor.
	log.Printf("Waiting %v to allow htlc interceptor to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)
	log.Printf("Alice paying")
	route := constructRoute(p.Node(), p.BreezClient().Node(), aliceLspScid, lntest.NewShortChanIDFromString(buyResp.JitChannelScid), outerAmountMsat)

	// Increment the delay by two to support the spec
	route.Hops[0].Delay += 2
	_, err := alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route)
	assert.Contains(p.t, err.Error(), "WIRE_TEMPORARY_CHANNEL_FAILURE")
}
