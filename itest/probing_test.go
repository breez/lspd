package itest

import (
	"crypto/sha256"
	"log"
	"time"

	"github.com/breez/lspd/itest/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

func testProbing(p *testParams) {
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

	h := sha256.New()
	_, _ = h.Write([]byte("probing-01:"))
	_, _ = h.Write(outerInvoice.paymentHash)
	fakePaymentHash := h.Sum(nil)

	log.Printf("Alice paying with fake payment hash with Bob online %x", fakePaymentHash)
	route := constructRoute(p.Node(), p.BreezClient().Node(), channelId, lntest.NewShortChanIDFromString("1x0x0"), outerAmountMsat)
	_, err := alice.PayViaRoute(outerAmountMsat, fakePaymentHash, outerInvoice.paymentSecret, route)

	// Expect incorrect or unknown payment details if the peer is online
	assert.Contains(p.t, err.Error(), "WIRE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS")

	// Kill the mobile client
	log.Printf("Stopping breez client")
	p.BreezClient().Stop()

	log.Printf("Alice paying with fake payment hash with Bob offline %x", fakePaymentHash)
	_, err = alice.PayViaRoute(outerAmountMsat, fakePaymentHash, outerInvoice.paymentSecret, route)

	// Expect unknown next peer if the peer is offline
	assert.Contains(p.t, err.Error(), "WIRE_UNKNOWN_NEXT_PEER")
}
