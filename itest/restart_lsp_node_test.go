package itest

import (
	"log"
	"time"

	"github.com/breez/lspd/itest/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

func testRestartLspNode(p *testParams) {
	alice := lntest.NewLndNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)
	p.lsp.LightningNode().Fund(10000000)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(p.lsp.LightningNode(), &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
	})
	alice.WaitForChannelReady(channel)

	log.Printf("Adding bob's invoices")
	outerAmountMsat := uint64(2100000)
	innerAmountMsat := calculateInnerAmountMsat(p.lsp, outerAmountMsat, nil)
	description := "Please pay me"
	innerInvoice, outerInvoice := GenerateInvoices(p.BreezClient(),
		generateInvoicesRequest{
			innerAmountMsat: innerAmountMsat,
			outerAmountMsat: outerAmountMsat,
			description:     description,
			lsp:             p.lsp,
		})
	p.BreezClient().SetHtlcAcceptor(innerAmountMsat)

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(p.lsp.LightningNode())

	log.Printf("Registering payment with lsp")
	RegisterPayment(p.lsp, &lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(innerAmountMsat),
	}, false)

	log.Printf("stopping lsp lightning node")
	p.lsp.LightningNode().Stop()
	log.Printf("waiting %v to allow lsp lightning node to stop completely", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)
	log.Printf("starting lsp lightning node again")
	p.lsp.LightningNode().Start()

	// TODO: Fix race waiting for htlc interceptor.
	log.Printf("Waiting %v to allow htlc interceptor to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)

	log.Printf("Connect Bob to LSP again")
	p.BreezClient().Node().ConnectPeer(p.lsp.LightningNode())

	log.Printf("Alice paying")
	payResp := alice.Pay(outerInvoice.bolt11)
	bobInvoice := p.BreezClient().Node().GetInvoice(payResp.PaymentHash)

	assert.Equal(p.t, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(p.t, innerAmountMsat, bobInvoice.AmountReceivedMsat)

	// Make sure capacity is correct
	chans := p.BreezClient().Node().GetChannels()
	assert.Equal(p.t, 1, len(chans))
	c := chans[0]
	AssertChannelCapacity(p.t, outerAmountMsat, c.CapacityMsat)
}
