package itest

import (
	"log"
	"time"

	"github.com/breez/lntest"
	"github.com/stretchr/testify/assert"
)

func testRegularForward(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)
	p.lsp.LightningNode().Fund(10000000)
	p.BreezClient().Node().Fund(100000)

	log.Print("Opening channel between Alice and the lsp")
	channelAL := alice.OpenChannel(p.lsp.LightningNode(), &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
		IsPublic:  true,
	})

	log.Print("Opening channel between lsp and Breez client")
	channelLB := p.lsp.LightningNode().OpenChannel(p.BreezClient().Node(), &lntest.OpenChannelOptions{
		AmountSat: 200000,
		IsPublic:  false,
	})

	log.Print("Waiting for channel between Alice and the lsp to be ready.")
	alice.WaitForChannelReady(channelAL)
	log.Print("Waiting for channel between LSP and Bob to be ready.")
	p.lsp.LightningNode().WaitForChannelReady(channelLB)
	p.BreezClient().Node().WaitForChannelReady(channelLB)

	// TODO: Fix race waiting for htlc interceptor.
	log.Printf("Waiting %v to allow htlc interceptor to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)

	log.Printf("Adding bob's invoice")
	amountMsat := uint64(2100000)
	bobInvoice := p.BreezClient().Node().CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat:      amountMsat,
		IncludeHopHints: true,
	})
	log.Printf(bobInvoice.Bolt11)

	invoiceWithHint := bobInvoice.Bolt11
	if !ContainsHopHint(p.t, bobInvoice.Bolt11) {
		chans := p.BreezClient().Node().GetChannels()
		assert.Len(p.t, chans, 1)

		var id lntest.ShortChannelID
		if chans[0].RemoteAlias != nil {
			id = *chans[0].RemoteAlias
		} else if chans[0].LocalAlias != nil {
			id = *chans[0].LocalAlias
		} else {
			id = chans[0].ShortChannelID
		}
		invoiceWithHint = AddHopHint(p.BreezClient(), bobInvoice.Bolt11, p.Lsp(), id, nil, 144)
	}
	log.Printf("invoice with hint: %v", invoiceWithHint)

	log.Printf("Alice paying")
	payResp := alice.Pay(invoiceWithHint)
	invoiceResult := p.BreezClient().Node().GetInvoice(bobInvoice.PaymentHash)

	assert.Equal(p.t, payResp.PaymentPreimage, invoiceResult.PaymentPreimage)
	assert.Equal(p.t, amountMsat, invoiceResult.AmountReceivedMsat)
}
