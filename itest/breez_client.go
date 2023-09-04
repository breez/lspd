package itest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"math/rand"
	"testing"

	"github.com/breez/lntest"
	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/lsps0/jsonrpc"
	"github.com/breez/lspd/lsps2"
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
	ReceiveCustomMessage() *lntest.CustomMsgRequest
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

func GenerateLsps2Invoices(n BreezClient, req generateInvoicesRequest, scid string) (invoice, invoice) {
	return generateInvoices(n, req, lntest.NewShortChanIDFromString(scid), lspCltvDelta+2)
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
			sig, err := ecdsa.SignCompact(n.Node().PrivateKey(), hash[:], true)
			log.Printf(
				"sign outer invoice. msg: '%x', hash: '%x', sig: '%x', err: %v",
				msg,
				hash,
				sig,
				err,
			)
			return sig, err
		},
	})
	lntest.CheckError(n.Harness().T, err)

	return newInvoice
}

func Lsps2GetInfo(c BreezClient, l LspNode, req lsps2.GetInfoRequest) lsps2.GetInfoResponse {
	req.Version = lsps2.SupportedVersion
	r := lsps2RequestResponse(c, l, "lsps2.get_info", req)
	var resp lsps2.GetInfoResponse
	err := json.Unmarshal(r, &resp)
	lntest.CheckError(c.Harness().T, err)

	return resp
}

func Lsps2Buy(c BreezClient, l LspNode, req lsps2.BuyRequest) lsps2.BuyResponse {
	req.Version = lsps2.SupportedVersion
	r := lsps2RequestResponse(c, l, "lsps2.buy", req)
	var resp lsps2.BuyResponse
	err := json.Unmarshal(r, &resp)
	lntest.CheckError(c.Harness().T, err)

	return resp
}

func lsps2RequestResponse(c BreezClient, l LspNode, method string, req interface{}) []byte {
	id := RandStringBytes(32)
	peerId := hex.EncodeToString(l.NodeId())
	inner, err := json.Marshal(req)
	lntest.CheckError(c.Harness().T, err)
	outer, err := json.Marshal(&jsonrpc.Request{
		JsonRpc: jsonrpc.Version,
		Method:  method,
		Id:      id,
		Params:  inner,
	})
	lntest.CheckError(c.Harness().T, err)

	log.Printf(string(outer))
	c.Node().SendCustomMessage(&lntest.CustomMsgRequest{
		PeerId: peerId,
		Type:   lsps0.Lsps0MessageType,
		Data:   outer,
	})

	m := c.ReceiveCustomMessage()
	log.Printf(string(m.Data))

	var resp jsonrpc.Response
	err = json.Unmarshal(m.Data, &resp)
	lntest.CheckError(c.Harness().T, err)

	if resp.Id != id {
		c.Harness().T.Fatalf("Received custom message, but had different id")
	}

	return resp.Result
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz"

func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
