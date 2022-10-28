package itest

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec"
	btcecv2 "github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/golang/protobuf/proto"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/zpay32"
	"gotest.tools/assert"
)

var pythonFileContent string = `#!/usr/bin/env python3
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

plugin.run()`

func TestOpenZeroConfChannelOnReceive(t *testing.T) {
	harness := lntest.NewTestHarness(t)
	defer harness.TearDown()

	miner := lntest.NewMiner(harness)
	alice := lntest.NewCoreLightningNode(harness, miner, "Alice")

	bobPrivKey, err := btcecv2.NewPrivateKey()
	lntest.CheckError(t, err)

	s := bobPrivKey.Serialize()

	pluginDir, err := ioutil.TempDir(harness.Dir, "bob")
	lntest.CheckError(t, err)
	pythonFilePath := filepath.Join(pluginDir, "zero_conf_plugin.py")
	pythonFile, err := os.OpenFile(pythonFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	lntest.CheckError(t, err)

	pythonWriter := bufio.NewWriter(pythonFile)
	_, err = pythonWriter.WriteString(pythonFileContent)
	lntest.CheckError(t, err)

	err = pythonWriter.Flush()
	lntest.CheckError(t, err)
	pythonFile.Close()

	pluginFilePath := filepath.Join(pluginDir, "start_zero_conf_plugin.sh")
	pluginFile, err := os.OpenFile(pluginFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	lntest.CheckError(t, err)

	pluginWriter := bufio.NewWriter(pluginFile)
	venvDir := filepath.Join(pluginDir, "venv")
	activatePath := filepath.Join(venvDir, "bin", "activate")
	_, err = pluginWriter.WriteString(fmt.Sprintf("python3 -m venv %s > /dev/null 2>&1\n", venvDir))
	lntest.CheckError(t, err)
	_, err = pluginWriter.WriteString(fmt.Sprintf("source %s > /dev/null 2>&1\n", activatePath))
	lntest.CheckError(t, err)
	_, err = pluginWriter.WriteString("pip install pyln-client > /dev/null 2>&1\n")
	lntest.CheckError(t, err)
	_, err = pluginWriter.WriteString(fmt.Sprintf("python %s\n", pythonFilePath))
	lntest.CheckError(t, err)

	err = pluginWriter.Flush()
	lntest.CheckError(t, err)
	pluginFile.Close()

	bob := lntest.NewCoreLightningNode(
		harness,
		miner,
		"Bob",
		fmt.Sprintf("--dev-force-privkey=%x", s),
		fmt.Sprintf("--plugin=%s", pluginFilePath),
	)
	lsp := NewLspdNode(harness, miner, "Lsp")
	alice.Fund(10000000)
	lsp.lightningNode.Fund(10000000)
	alice.WaitForSync()
	lsp.lightningNode.WaitForSync()

	channelOptions := &lntest.OpenChannelOptions{
		AmountSat: 1000000,
	}

	log.Print("Opening channel between Alice and the lsp")
	channel := alice.OpenChannel(lsp.lightningNode, channelOptions)
	miner.MineBlocks(6)
	alice.WaitForSync()
	lsp.lightningNode.WaitForSync()
	channel.WaitForChannelReady()

	preimage, err := GenerateRandomBytes(32)
	lntest.CheckError(t, err)

	// NOTE: The inner amount is equal to the outer amount, because the lsp doesn't charge fees yet.
	innerAmountMsat := uint64(21000000)
	outerAmountMsat := uint64(21000000)
	description := "Please pay me"
	lspNodeId, err := btcec.ParsePubKey(lsp.lightningNode.NodeId(), btcec.S256())
	lntest.CheckError(t, err)

	log.Printf("Adding bob's invoices")
	innerInvoice := bob.CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat:  innerAmountMsat,
		Description: &description,
		Preimage:    &preimage,
	})
	outerInvoiceRaw, err := zpay32.Decode(innerInvoice.Bolt11, &chaincfg.RegressionNetParams)
	lntest.CheckError(t, err)

	milliSat := lnwire.MilliSatoshi(outerAmountMsat)
	outerInvoiceRaw.MilliSat = &milliSat
	fakeChanId := &lnwire.ShortChannelID{BlockHeight: 1, TxIndex: 0, TxPosition: 0}
	outerInvoiceRaw.RouteHints = append(outerInvoiceRaw.RouteHints, []zpay32.HopHint{
		{
			NodeID:                    lspNodeId,
			ChannelID:                 fakeChanId.ToUint64(),
			FeeBaseMSat:               1000,
			FeeProportionalMillionths: 1000,
			CLTVExpiryDelta:           144,
		},
	})

	outerInvoice, err := outerInvoiceRaw.Encode(zpay32.MessageSigner{
		SignCompact: func(msg []byte) ([]byte, error) {
			return ecdsa.SignCompact(bobPrivKey, msg, true)
		},
	})
	lntest.CheckError(t, err)

	log.Printf("Added bob's invoices")
	// NOTE: We pretend to be paying fees to the lsp, but actually we won't.
	pretendAmount := outerAmountMsat - 2000000
	paymentInfo := &lspd.PaymentInformation{
		PaymentHash:        innerInvoice.PaymentHash,
		PaymentSecret:      innerInvoice.PaymentSecret,
		Destination:        bob.NodeId(),
		IncomingAmountMsat: int64(outerAmountMsat),
		OutgoingAmountMsat: int64(pretendAmount),
	}
	serialized, err := proto.Marshal(paymentInfo)
	lntest.CheckError(t, err)

	encrypted, err := btcec.Encrypt(&lsp.publicKey, serialized)
	lntest.CheckError(t, err)

	log.Print("Connecting bob to lspd")
	bob.ConnectPeer(lsp.NodeId(), lsp.lightningNode.Host(), lsp.lightningNode.Port())

	log.Printf("Registering payment")
	_, err = lsp.rpc.RegisterPayment(
		harness.Ctx,
		&lspd.RegisterPaymentRequest{
			Blob: encrypted,
		},
	)
	lntest.CheckError(t, err)

	log.Printf("Alice paying")
	payResp := alice.Pay(outerInvoice)
	log.Printf("Alice waiting for payment complete")
	payInfo := alice.WaitPaymentComplete(payResp.PaymentHash, payResp.Parts)
	bobInvoice := bob.GetInvoice(payResp.PaymentHash)

	assert.DeepEqual(t, payInfo.PaymentPreimage, bobInvoice.PaymentPreimage)
	assert.Equal(t, outerAmountMsat, bobInvoice.AmountReceivedMsat)
}
