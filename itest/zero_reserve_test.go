package itest

import (
	"log"
	"time"

	"github.com/breez/lspd/itest/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

func testZeroReserve(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)
	p.Node().Fund(10000000)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(p.Node(), &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
	})
	channelId := alice.WaitForChannelReady(channel)

	log.Printf("Adding bob's invoices")
	outerAmountMsat := uint64(2100000)
	innerAmountMsat := calculateInnerAmountMsat(p.Harness(), outerAmountMsat, nil)
	description := "Please pay me"
	innerInvoice, outerInvoice := GenerateInvoices(p.BreezClient(),
		generateInvoicesRequest{
			innerAmountMsat: innerAmountMsat,
			outerAmountMsat: outerAmountMsat,
			description:     description,
			lsp:             p.Lspd().Client(0),
		})
	p.BreezClient().SetHtlcAcceptor(innerAmountMsat)

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(p.Node())

	log.Printf("Registering payment with lsp")
	p.Lspd().Client(0).RegisterPayment(&lspd.PaymentInformation{
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
	route := constructRoute(p.Node(), p.BreezClient().Node(), channelId, lntest.NewShortChanIDFromString("1x0x0"), outerAmountMsat)
	alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route)

	// Make sure balance is correct
	chans := p.BreezClient().Node().GetChannels()
	assert.Equal(p.t, 1, len(chans))

	c := chans[0]
	assert.Equal(p.t, c.RemoteReserveMsat, c.CapacityMsat/100)
	log.Printf("local reserve: %d, remote reserve: %d", c.LocalReserveMsat, c.RemoteReserveMsat)
	assert.Zero(p.t, c.LocalReserveMsat)

	lspInvoice := p.Node().CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat: innerAmountMsat - 1,
	})
	<-time.After(time.Second * 1)
	p.BreezClient().Node().Pay(lspInvoice.Bolt11)
}
