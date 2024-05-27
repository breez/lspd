package itest

import (
	"log"
	"time"

	"github.com/breez/lspd/itest/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

func testDynamicFeeFlow(p *testParams) {
	alice := lntest.NewClnNode(p.h, p.m, "Alice")
	alice.Start()
	alice.Fund(10000000)
	p.Node().Fund(10000000)

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(p.Node(), &lntest.OpenChannelOptions{
		AmountSat: publicChanAmount,
	})
	channelId := alice.WaitForChannelReady(channel)

	log.Printf("Setting fee params")
	p.Lspd().Client(0).SetFeeParams(
		[]*FeeParamSetting{
			{
				Validity:     time.Second * 3600,
				MinMsat:      3000000,
				Proportional: 1000,
			},
		},
	)

	log.Printf("Getting channel information")

	info := p.Lspd().Client(0).ChannelInformation()
	assert.Len(p.t, info.OpeningFeeParamsMenu, 1)
	params := info.OpeningFeeParamsMenu[0]
	assert.Equal(p.t, uint64(3000000), params.MinMsat)

	log.Printf("opening_fee_params: %+v", params)
	log.Printf("Adding bob's invoices")
	outerAmountMsat := uint64(4200000)
	innerAmountMsat := calculateInnerAmountMsat(p.Harness(), outerAmountMsat, params)
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

	log.Printf("Testing some bad registrations")
	err := p.Lspd().Client(0).RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(innerAmountMsat),
		OpeningFeeParams: &lspd.OpeningFeeParams{
			// modify minmsat
			MinMsat:              params.MinMsat + 1,
			Proportional:         params.Proportional,
			ValidUntil:           params.ValidUntil,
			MaxIdleTime:          params.MaxIdleTime,
			MaxClientToSelfDelay: params.MaxClientToSelfDelay,
			Promise:              params.Promise,
		},
	}, true)
	assert.Contains(p.t, err.Error(), "invalid opening_fee_params")

	err = p.Lspd().Client(0).RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(innerAmountMsat),
		OpeningFeeParams: &lspd.OpeningFeeParams{
			MinMsat:              params.MinMsat,
			Proportional:         params.Proportional,
			ValidUntil:           params.ValidUntil,
			MaxIdleTime:          params.MaxIdleTime,
			MaxClientToSelfDelay: params.MaxClientToSelfDelay,
			// Modify promise
			Promise: params.Promise + "aa",
		},
	}, true)
	assert.Contains(p.t, err.Error(), "invalid opening_fee_params")

	err = p.Lspd().Client(0).RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:   innerInvoice.paymentHash,
		PaymentSecret: innerInvoice.paymentSecret,
		Destination:   p.BreezClient().Node().NodeId(),
		// Fee too low
		IncomingAmountMsat: int64(2999999),
		OutgoingAmountMsat: int64(0),
		OpeningFeeParams:   params,
	}, true)
	assert.Contains(p.t, err.Error(), "not enough fees")

	err = p.Lspd().Client(0).RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(innerAmountMsat + 1),
		OpeningFeeParams:   params,
	}, true)
	assert.Contains(p.t, err.Error(), "not enough fees")

	// Now register the payment for real
	log.Printf("Registering payment with lsp")
	p.Lspd().Client(0).RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:        innerInvoice.paymentHash,
		PaymentSecret:      innerInvoice.paymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(innerAmountMsat),
		OpeningFeeParams:   info.OpeningFeeParamsMenu[0],
	}, false)

	// TODO: Fix race waiting for htlc interceptor.
	log.Printf("Waiting %v to allow htlc interceptor to activate.", htlcInterceptorDelay)
	<-time.After(htlcInterceptorDelay)
	log.Printf("Alice paying")
	route := constructRoute(p.Node(), p.BreezClient().Node(), channelId, lntest.NewShortChanIDFromString("1x0x0"), outerAmountMsat)
	payResp, err := alice.PayViaRoute(outerAmountMsat, outerInvoice.paymentHash, outerInvoice.paymentSecret, route)
	lntest.CheckError(p.t, err)
	bobInvoice := p.BreezClient().Node().GetInvoice(payResp.PaymentHash)

	assert.Equal(p.t, payResp.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(p.t, innerAmountMsat, bobInvoice.AmountReceivedMsat)

	// Make sure capacity is correct
	chans := p.BreezClient().Node().GetChannels()
	assert.Equal(p.t, 1, len(chans))
	c := chans[0]
	AssertChannelCapacity(p.t, outerAmountMsat, c.CapacityMsat)
}
