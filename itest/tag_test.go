package itest

import (
	"log"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/stretchr/testify/assert"
)

func registerPaymentWithTag(p *testParams) {
	expected := "{\"apiKey\": \"11111111111111\"}"
	i := p.BreezClient().Node().CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat: 25000000,
	})

	log.Printf("Registering payment with lsp")
	RegisterPayment(p.lsp, &lspd.PaymentInformation{
		PaymentHash:        i.PaymentHash,
		PaymentSecret:      i.PaymentSecret,
		Destination:        p.BreezClient().Node().NodeId(),
		IncomingAmountMsat: int64(25000000),
		OutgoingAmountMsat: int64(21000000),
		Tag:                expected,
	})

	pgxPool, err := pgxpool.Connect(p.h.Ctx, p.lsp.PostgresBackend().ConnectionString())
	if err != nil {
		p.h.T.Fatalf("Failed to connect to postgres backend: %v", err)
	}
	defer pgxPool.Close()

	rows, err := pgxPool.Query(
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
