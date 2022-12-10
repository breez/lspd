package itest

import (
	"log"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

func testZeroReserve(h *lntest.TestHarness, lsp LspNode, miner *lntest.Miner) {
	alice := lntest.NewCoreLightningNode(h, miner, "Alice")
	bob := NewZeroConfNode(h, miner, "Bob")

	alice.Fund(10000000)
	lsp.LightningNode().Fund(10000000)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(lsp.LightningNode(), &lntest.OpenChannelOptions{
		AmountSat: 1000000,
	})
	channelId := alice.WaitForChannelReady(channel)

	log.Printf("Adding bob's invoices")
	outerAmountMsat := uint64(2100000)
	innerAmountMsat := uint64(2100000)
	description := "Please pay me"
	innerInvoice, outerInvoice := bob.GenerateInvoices(generateInvoicesRequest{
		innerAmountMsat: innerAmountMsat,
		outerAmountMsat: outerAmountMsat,
		description:     description,
		lsp:             lsp,
	})

	log.Print("Connecting bob to lspd")
	bob.lightningNode.ConnectPeer(lsp.LightningNode())

	// NOTE: We pretend to be paying fees to the lsp, but actually we won't.
	log.Printf("Registering payment with lsp")
	pretendAmount := outerAmountMsat - 2000000
	RegisterPayment(lsp, &lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        bob.lightningNode.NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(pretendAmount),
	})

	log.Printf("Alice paying")
	route := constructRoute(lsp.LightningNode(), bob.lightningNode, channelId, lntest.NewShortChanIDFromString("1x0x0"), outerAmountMsat)
	alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route)

	// Make sure balance is correct
	chans := bob.lightningNode.GetChannels()
	assert.Len(h.T, chans, 1)

	c := chans[0]
	assert.Equal(h.T, c.RemoteReserveMsat, c.CapacityMsat/100)
	assert.Zero(h.T, c.LocalReserveMsat)
}
