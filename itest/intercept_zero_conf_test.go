package itest

import (
	"log"
	"testing"
	"time"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"gotest.tools/assert"
)

func TestOpenZeroConfChannelOnReceive(t *testing.T) {
	harness := lntest.NewTestHarness(t)
	defer harness.TearDown()

	timeout := time.Now().Add(time.Minute)

	miner := lntest.NewMiner(harness)
	alice := lntest.NewCoreLightningNode(harness, miner, "Alice", timeout)
	bob := NewZeroConfNode(harness, miner, "Bob", timeout)
	lsp := NewLspdNode(harness, miner, "Lsp", timeout)

	alice.Fund(10000000, timeout)
	lsp.lightningNode.Fund(10000000, timeout)

	log.Print("Opening channel between Alice and the lsp")
	alice.OpenChannelAndWait(lsp.lightningNode, &lntest.OpenChannelOptions{
		AmountSat: 1000000,
	}, timeout)

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
	bob.lightningNode.ConnectPeer(lsp.lightningNode)

	// NOTE: We pretend to be paying fees to the lsp, but actually we won't.
	log.Printf("Registering payment with lsp")
	pretendAmount := outerAmountMsat - 2000000
	lsp.RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        bob.lightningNode.NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(pretendAmount),
	})

	log.Printf("Alice paying")
	payResp := alice.Pay(outerInvoice.bolt11, timeout)
	bobInvoice := bob.lightningNode.GetInvoice(payResp.PaymentHash)

	assert.DeepEqual(t, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(t, outerAmountMsat, bobInvoice.AmountReceivedMsat)
}

func TestOpenZeroConfSingleHtlc(t *testing.T) {
	harness := lntest.NewTestHarness(t)
	defer harness.TearDown()

	timeout := time.Now().Add(time.Minute)

	miner := lntest.NewMiner(harness)
	alice := lntest.NewCoreLightningNode(harness, miner, "Alice", timeout)
	bob := NewZeroConfNode(harness, miner, "Bob", timeout)
	lsp := NewLspdNode(harness, miner, "Lsp", timeout)

	alice.Fund(10000000, timeout)
	lsp.lightningNode.Fund(10000000, timeout)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannelAndWait(lsp.lightningNode, &lntest.OpenChannelOptions{
		AmountSat: 1000000,
	}, timeout)

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
	bob.lightningNode.ConnectPeer(lsp.lightningNode)

	// NOTE: We pretend to be paying fees to the lsp, but actually we won't.
	log.Printf("Registering payment with lsp")
	pretendAmount := outerAmountMsat - 2000000
	lsp.RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        bob.lightningNode.NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(pretendAmount),
	})

	log.Printf("Alice paying")
	route := constructRoute(lsp.lightningNode, bob.lightningNode, channel.ChannelId, "1x0x0", outerAmountMsat)
	alice.StartPayPartViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, 0, route)
	payResp := alice.WaitForPaymentPart(outerInvoice.paymentHash, timeout, 0)
	bobInvoice := bob.lightningNode.GetInvoice(payResp.PaymentHash)

	assert.DeepEqual(t, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(t, outerAmountMsat, bobInvoice.AmountReceivedMsat)
}

func constructRoute(
	lsp *lntest.CoreLightningNode,
	bob *lntest.CoreLightningNode,
	aliceLspChannel string,
	lspBobChannel string,
	amountMsat uint64) *lntest.Route {
	return &lntest.Route{
		Route: []*lntest.Hop{
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
