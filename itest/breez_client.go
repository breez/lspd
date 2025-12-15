package itest

import (
	"crypto/sha256"
	"log"
	"math/rand"
	"testing"

	"github.com/breez/lspd/itest/lntest"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/zpay32"
)

type BreezClient interface {
	Name() string
	Harness() *lntest.TestHarness
	Node() lntest.LightningNode
	Start()
	Stop() error
	SetHtlcAcceptor(totalMsat uint64)
	ResetHtlcAcceptor()
}

type generateInvoicesRequest struct {
	innerAmountMsat uint64
	outerAmountMsat uint64
	description     string
	lsp             LspNode
}

type invoice struct {
	bolt11          string
	paymentHash     []byte
	paymentSecret   []byte
	paymentPreimage []byte
}

func GenerateInvoices(n BreezClient, req generateInvoicesRequest) (invoice, invoice) {
	return generateInvoices(n, req, lntest.ShortChannelID{
		BlockHeight: 1,
		TxIndex:     0,
		OutputIndex: 0,
	}, lspCltvDelta)
}

func generateInvoices(n BreezClient, req generateInvoicesRequest, scid lntest.ShortChannelID, cltvDelta uint16) (invoice, invoice) {
	preimage, err := GenerateRandomBytes(32)
	lntest.CheckError(n.Harness().T, err)

	innerInvoice := n.Node().CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat:  req.innerAmountMsat,
		Description: &req.description,
		Preimage:    &preimage,
	})
	outerInvoice := AddHopHint(n, innerInvoice.Bolt11, req.lsp, scid, &req.outerAmountMsat, cltvDelta)

	inner := invoice{
		bolt11:          innerInvoice.Bolt11,
		paymentHash:     innerInvoice.PaymentHash,
		paymentSecret:   innerInvoice.PaymentSecret,
		paymentPreimage: preimage,
	}
	outer := invoice{
		bolt11:          outerInvoice,
		paymentHash:     innerInvoice.PaymentHash[:],
		paymentSecret:   innerInvoice.PaymentSecret,
		paymentPreimage: preimage,
	}

	return inner, outer
}

func ContainsHopHint(t *testing.T, invoice string) bool {
	rawInvoice, err := zpay32.Decode(invoice, &chaincfg.RegressionNetParams)
	lntest.CheckError(t, err)

	return len(rawInvoice.RouteHints) > 0
}

func AddHopHint(n BreezClient, invoice string, lsp LspNode, chanid lntest.ShortChannelID, amountMsat *uint64, cltvDelta uint16) string {
	rawInvoice, err := zpay32.Decode(invoice, &chaincfg.RegressionNetParams)
	lntest.CheckError(n.Harness().T, err)

	if amountMsat != nil {
		milliSat := lnwire.MilliSatoshi(*amountMsat)
		rawInvoice.MilliSat = &milliSat
	}

	lspNodeId, err := btcec.ParsePubKey(lsp.NodeId())
	lntest.CheckError(n.Harness().T, err)
	rawInvoice.RouteHints = append(rawInvoice.RouteHints, []zpay32.HopHint{
		{
			NodeID:                    lspNodeId,
			ChannelID:                 chanid.ToUint64(),
			FeeBaseMSat:               lspBaseFeeMsat,
			FeeProportionalMillionths: lspFeeRatePpm,
			CLTVExpiryDelta:           cltvDelta,
		},
	})

	log.Printf(
		"Encoding invoice. privkey: '%x', invoice: '%+v', original bolt11: '%s'",
		n.Node().PrivateKey().Serialize(),
		rawInvoice,
		invoice,
	)
	newInvoice, err := rawInvoice.Encode(zpay32.MessageSigner{
		SignCompact: func(msg []byte) ([]byte, error) {
			hash := sha256.Sum256(msg)
			sig := ecdsa.SignCompact(n.Node().PrivateKey(), hash[:], true)
			log.Printf(
				"sign outer invoice. msg: '%x', hash: '%x', sig: '%x'",
				msg,
				hash,
				sig,
			)
			return sig, nil
		},
	})
	lntest.CheckError(n.Harness().T, err)

	return newInvoice
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz"

func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
