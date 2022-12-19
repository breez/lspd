package itest

import (
	"log"
	"time"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

var htlcInterceptorDelay = time.Second * 7

func testOpenZeroConfChannelOnReceive(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
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
	innerAmountMsat, lspAmountMsat := calculateInnerAmountMsat(p.lsp, outerAmountMsat)
	description := "Please pay me"
	innerInvoice, outerInvoice := GenerateInvoices(p.BreezClient(),
		generateInvoicesRequest{
			innerAmountMsat: innerAmountMsat,
			outerAmountMsat: outerAmountMsat,
			description:     description,
			lsp:             p.lsp,
		})

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(p.lsp.LightningNode())

	log.Printf("Registering payment with lsp")
	RegisterPayment(p.lsp, &lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(lspAmountMsat),
	})

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
	assert.Len(p.t, chans, 1)
	c := chans[0]
	AssertChannelCapacity(p.t, outerAmountMsat, c.CapacityMsat)
}

func testOpenZeroConfSingleHtlc(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)
	p.lsp.LightningNode().Fund(10000000)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(p.lsp.LightningNode(), &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
	})
	channelId := alice.WaitForChannelReady(channel)

	log.Printf("Adding bob's invoices")
	outerAmountMsat := uint64(2100000)
	innerAmountMsat, lspAmountMsat := calculateInnerAmountMsat(p.lsp, outerAmountMsat)
	description := "Please pay me"
	innerInvoice, outerInvoice := GenerateInvoices(p.BreezClient(),
		generateInvoicesRequest{
			innerAmountMsat: innerAmountMsat,
			outerAmountMsat: outerAmountMsat,
			description:     description,
			lsp:             p.lsp,
		})

	log.Print("Connecting bob to lspd")
	p.BreezClient().Node().ConnectPeer(p.lsp.LightningNode())

	log.Printf("Registering payment with lsp")
	RegisterPayment(p.lsp, &lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(lspAmountMsat),
	})

	// TODO: Fix race waiting for htlc interceptor.
	log.Printf("Waiting %v to allow htlc interceptor to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)
	log.Printf("Alice paying")
	route := constructRoute(p.lsp.LightningNode(), p.BreezClient().Node(), channelId, lntest.NewShortChanIDFromString("1x0x0"), outerAmountMsat)
	payResp, err := alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route)
	lntest.CheckError(p.t, err)
	bobInvoice := p.BreezClient().Node().GetInvoice(payResp.PaymentHash)

	assert.Equal(p.t, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(p.t, innerAmountMsat, bobInvoice.AmountReceivedMsat)

	// Make sure capacity is correct
	chans := p.BreezClient().Node().GetChannels()
	assert.Len(p.t, chans, 1)
	c := chans[0]
	AssertChannelCapacity(p.t, outerAmountMsat, c.CapacityMsat)
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
