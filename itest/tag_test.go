package itest

import (
	"log"

	"github.com/breez/lspd/itest/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/stretchr/testify/assert"
)

func registerPaymentWithTag(p *testParams) {
	expected := "{\"apiKey\": \"11111111111111\"}"
	i := p.BreezClient().Node().CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat: 25000000,
	})

	log.Printf("Registering payment with lsp")
	p.Lspd().Client(0).RegisterPayment(&lspd.PaymentInformation{
		PaymentHash:        i.PaymentHash,
		PaymentSecret:      i.PaymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(25000000),
		OutgoingAmountMsat: int64(21000000),
		Tag:                expected,
	}, false)

	rows, err := p.Lspd().PostgresBackend().Pool().Query(
		p.h.Ctx,
		"SELECT tag FROM public.payments WHERE payment_hash=$1",
		i.PaymentHash,
	)
	if err != nil {
		p.h.T.Fatalf("Failed to query tag: %v", err)
	}

	defer rows.Close()
	if !rows.Next() {
		p.h.T.Fatal("No rows found")
	}

	var actual string
	err = rows.Scan(&actual)
	if err != nil {
		p.h.T.Fatalf("Failed to get tag from row: %v", err)
	}

	assert.Equal(p.h.T, expected, actual)
}
