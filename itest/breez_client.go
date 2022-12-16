package itest

import (
	"crypto/sha256"

	"github.com/breez/lntest"
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
	preimage, err := GenerateRandomBytes(32)
	lntest.CheckError(n.Harness().T, err)

	lspNodeId, err := btcec.ParsePubKey(req.lsp.NodeId())
	lntest.CheckError(n.Harness().T, err)

	innerInvoice := n.Node().CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat:  req.innerAmountMsat,
		Description: &req.description,
		Preimage:    &preimage,
	})
	outerInvoiceRaw, err := zpay32.Decode(innerInvoice.Bolt11, &chaincfg.RegressionNetParams)
	lntest.CheckError(n.Harness().T, err)

	milliSat := lnwire.MilliSatoshi(req.outerAmountMsat)
	outerInvoiceRaw.MilliSat = &milliSat
	fakeChanId := &lnwire.ShortChannelID{BlockHeight: 1, TxIndex: 0, TxPosition: 0}
	outerInvoiceRaw.RouteHints = append(outerInvoiceRaw.RouteHints, []zpay32.HopHint{
		{
			NodeID:                    lspNodeId,
			ChannelID:                 fakeChanId.ToUint64(),
			FeeBaseMSat:               lspBaseFeeMsat,
			FeeProportionalMillionths: lspFeeRatePpm,
			CLTVExpiryDelta:           lspCltvDelta,
		},
	})

	outerInvoice, err := outerInvoiceRaw.Encode(zpay32.MessageSigner{
		SignCompact: func(msg []byte) ([]byte, error) {
			hash := sha256.Sum256(msg)
			return ecdsa.SignCompact(n.Node().PrivateKey(), hash[:], true)
		},
	})
	lntest.CheckError(n.Harness().T, err)

	inner := invoice{
		bolt11:          innerInvoice.Bolt11,
		paymentHash:     innerInvoice.PaymentHash,
		paymentSecret:   innerInvoice.PaymentSecret,
		paymentPreimage: preimage,
	}
	outer := invoice{
		bolt11:          outerInvoice,
		paymentHash:     outerInvoiceRaw.PaymentHash[:],
		paymentSecret:   innerInvoice.PaymentSecret,
		paymentPreimage: preimage,
	}

	return inner, outer
}
