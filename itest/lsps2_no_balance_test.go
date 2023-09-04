package itest

import (
	"log"
	"time"

	"github.com/breez/lntest"
	"github.com/breez/lspd/lsps2"
	"github.com/stretchr/testify/assert"
)

func testLsps2NoBalance(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(p.lsp.LightningNode(), &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
	})
	aliceLspScid := alice.WaitForChannelReady(channel)

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(p.lsp.LightningNode())

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
	route := constructRoute(p.Lsp().LightningNode(), p.BreezClient().Node(), aliceLspScid, lntest.NewShortChanIDFromString(buyResp.JitChannelScid), outerAmountMsat)

	// Increment the delay by two to support the spec
	route.Hops[0].Delay += 2
	_, err := alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route)
	assert.Contains(p.t, err.Error(), "WIRE_TEMPORARY_CHANNEL_FAILURE")
}
