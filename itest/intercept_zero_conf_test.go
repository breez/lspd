package itest

import (
	"log"
	"time"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"gotest.tools/assert"
)

func testOpenZeroConfChannelOnReceive(h *lntest.TestHarness, lsp LspNode, miner *lntest.Miner, timeout time.Time) {
	alice := lntest.NewCoreLightningNode(h, miner, "Alice", timeout)
	bob := NewZeroConfNode(h, miner, "Bob", timeout)

	alice.Fund(10000000, timeout)
	lsp.LightningNode().Fund(10000000, timeout)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(lsp.LightningNode(), &lntest.OpenChannelOptions{
		AmountSat: 1000000,
	})
	alice.WaitForChannelReady(channel, timeout)

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
	payResp := alice.Pay(outerInvoice.bolt11, timeout)
	bobInvoice := bob.lightningNode.GetInvoice(payResp.PaymentHash)

	assert.DeepEqual(h.T, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(h.T, outerAmountMsat, bobInvoice.AmountReceivedMsat)
}

func testOpenZeroConfSingleHtlc(h *lntest.TestHarness, lsp LspNode, miner *lntest.Miner, timeout time.Time) {
	alice := lntest.NewCoreLightningNode(h, miner, "Alice", timeout)
	bob := NewZeroConfNode(h, miner, "Bob", timeout)

	alice.Fund(10000000, timeout)
	lsp.LightningNode().Fund(10000000, timeout)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(lsp.LightningNode(), &lntest.OpenChannelOptions{
		AmountSat: 1000000,
	})
	channelId := alice.WaitForChannelReady(channel, timeout)

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
	payResp := alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route, timeout)
	bobInvoice := bob.lightningNode.GetInvoice(payResp.PaymentHash)

	assert.DeepEqual(h.T, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(h.T, outerAmountMsat, bobInvoice.AmountReceivedMsat)
}

func constructRoute(
	lsp lntest.LightningNode,
	bob lntest.LightningNode,
	aliceLspChannel lntest.ShortChannelID,
	lspBobChannel lntest.ShortChannelID,
	amountMsat uint64,
) *lntest.Route {
	return &lntest.Route{
		Hops: []*lntest.Hop{
			{
				Id:         lsp.NodeId(),
				Channel:    aliceLspChannel,
				AmountMsat: amountMsat + uint64(lspBaseFeeMsat) + (amountMsat * uint64(lspFeeRatePpm) / 1000000),
				Delay:      144 + lspCltvDelta,
			},
			{
				Id:         bob.NodeId(),
				Channel:    lspBobChannel,
				AmountMsat: amountMsat,
				Delay:      144,
			},
		},
	}
}
