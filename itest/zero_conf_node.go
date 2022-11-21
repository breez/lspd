package itest

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/breez/lntest"
	btcec "github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/zpay32"
)

type ZeroConfNode struct {
	name          string
	harness       *lntest.TestHarness
	lightningNode *lntest.CoreLightningNode
	privKey       *secp256k1.PrivateKey
	scriptDir     string
}

var pluginContent string = `#!/usr/bin/env python3
"""Use the openchannel hook to selectively opt-into zeroconf
"""

from pyln.client import Plugin

plugin = Plugin()


@plugin.hook('openchannel')
def on_openchannel(openchannel, plugin, **kwargs):
	plugin.log(repr(openchannel))
	mindepth = int(0)

	plugin.log(f"This peer is in the zeroconf allowlist, setting mindepth={mindepth}")
	return {'result': 'continue', 'mindepth': mindepth}

plugin.run()
`

var pluginStartupContent string = `python3 -m venv %s > /dev/null 2>&1
source %s > /dev/null 2>&1
pip install pyln-client > /dev/null 2>&1
python %s
`

func NewZeroConfNode(h *lntest.TestHarness, m *lntest.Miner, name string, timeout time.Time) *ZeroConfNode {
	privKey, err := btcec.NewPrivateKey()
	lntest.CheckError(h.T, err)

	s := privKey.Serialize()

	scriptDir, err := os.MkdirTemp(h.Dir, name)
	lntest.CheckError(h.T, err)
	pythonFilePath := filepath.Join(scriptDir, "zero_conf_plugin.py")
	pythonFile, err := os.OpenFile(pythonFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	lntest.CheckError(h.T, err)

	pythonWriter := bufio.NewWriter(pythonFile)
	_, err = pythonWriter.WriteString(pluginContent)
	lntest.CheckError(h.T, err)

	err = pythonWriter.Flush()
	lntest.CheckError(h.T, err)
	pythonFile.Close()

	pluginFilePath := filepath.Join(scriptDir, "start_zero_conf_plugin.sh")
	pluginFile, err := os.OpenFile(pluginFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	lntest.CheckError(h.T, err)

	pluginWriter := bufio.NewWriter(pluginFile)
	venvDir := filepath.Join(scriptDir, "venv")
	activatePath := filepath.Join(venvDir, "bin", "activate")
	_, err = pluginWriter.WriteString(fmt.Sprintf(pluginStartupContent, venvDir, activatePath, pythonFilePath))
	lntest.CheckError(h.T, err)

	err = pluginWriter.Flush()
	lntest.CheckError(h.T, err)
	pluginFile.Close()

	node := lntest.NewCoreLightningNode(
		h,
		m,
		name,
		timeout,
		fmt.Sprintf("--dev-force-privkey=%x", s),
		fmt.Sprintf("--plugin=%s", pluginFilePath),
	)

	return &ZeroConfNode{
		name:          name,
		harness:       h,
		lightningNode: node,
		scriptDir:     scriptDir,
		privKey:       privKey,
	}
}

type generateInvoicesRequest struct {
	innerAmountMsat uint64
	outerAmountMsat uint64
	description     string
	lsp             *LspNode
}

type invoice struct {
	bolt11          string
	paymentHash     []byte
	paymentSecret   []byte
	paymentPreimage []byte
	expiresAt       uint64
}

func (n *ZeroConfNode) GenerateInvoices(req generateInvoicesRequest) (invoice, invoice) {
	preimage, err := GenerateRandomBytes(32)
	lntest.CheckError(n.harness.T, err)

	lspNodeId, err := btcec.ParsePubKey(req.lsp.lightningNode.NodeId())
	lntest.CheckError(n.harness.T, err)

	log.Printf("Adding bob's invoices")
	innerInvoice := n.lightningNode.CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat:  req.innerAmountMsat,
		Description: &req.description,
		Preimage:    &preimage,
	})
	outerInvoiceRaw, err := zpay32.Decode(innerInvoice.Bolt11, &chaincfg.RegressionNetParams)
	lntest.CheckError(n.harness.T, err)

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
			return ecdsa.SignCompact(n.privKey, hash[:], true)
		},
	})
	lntest.CheckError(n.harness.T, err)

	inner := invoice{
		bolt11:          innerInvoice.Bolt11,
		paymentHash:     innerInvoice.PaymentHash,
		paymentSecret:   innerInvoice.PaymentSecret,
		paymentPreimage: preimage,
		expiresAt:       innerInvoice.ExpiresAt,
	}
	outer := invoice{
		bolt11:          outerInvoice,
		paymentHash:     outerInvoiceRaw.PaymentHash[:],
		paymentSecret:   innerInvoice.PaymentSecret,
		paymentPreimage: preimage,
		expiresAt:       innerInvoice.ExpiresAt,
	}

	return inner, outer
}
