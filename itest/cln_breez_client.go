package itest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/breez/lntest"
	"github.com/breez/lspd/cln_plugin/proto"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/tlv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

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
	if openchannel.funding_msat == 200000000:
	    return {'result': 'continue'}

	return {'result': 'continue', 'mindepth': mindepth}

plugin.run()
`

var pluginStartupContent string = `python3 -m venv %s > /dev/null 2>&1
source %s > /dev/null 2>&1
pip install pyln-client > /dev/null 2>&1
python %s
`

type clnBreezClient struct {
	name                string
	scriptDir           string
	pluginFilePath      string
	htlcAcceptorAddress string
	htlcAcceptor        func(*proto.HtlcAccepted) *proto.HtlcResolution
	htlcAcceptorCancel  context.CancelFunc
	harness             *lntest.TestHarness
	isInitialized       bool
	node                *lntest.ClnNode
	mtx                 sync.Mutex
}

func newClnBreezClient(h *lntest.TestHarness, m *lntest.Miner, name string) BreezClient {
	scriptDir := h.GetDirectory(name)
	pluginFilePath := filepath.Join(scriptDir, "start_zero_conf_plugin.sh")
	htlcAcceptorPort, err := lntest.GetPort()
	if err != nil {
		h.T.Fatalf("Could not get port for htlc acceptor plugin: %v", err)
	}

	htlcAcceptorAddress := fmt.Sprintf("127.0.0.1:%v", htlcAcceptorPort)
	node := lntest.NewClnNode(
		h,
		m,
		name,
		fmt.Sprintf("--plugin=%s", pluginFilePath),
		fmt.Sprintf("--plugin=%s", *clnPluginExec),
		fmt.Sprintf("--lsp-listen=%s", htlcAcceptorAddress),
		// NOTE: max-concurrent-htlcs is 30 on mainnet by default. In cln V22.11
		// there is a check for 'all dust' commitment transactions. The max
		// concurrent HTLCs of both sides of the channel * dust limit must be
		// lower than the channel capacity in order to open a zero conf zero
		// reserve channel. Relevant code:
		// https://github.com/ElementsProject/lightning/blob/774d16a72e125e4ae4e312b9e3307261983bec0e/openingd/openingd.c#L481-L520
		"--max-concurrent-htlcs=30",
		"--experimental-anchors",
	)

	return &clnBreezClient{
		name:                name,
		harness:             h,
		node:                node,
		scriptDir:           scriptDir,
		pluginFilePath:      pluginFilePath,
		htlcAcceptorAddress: htlcAcceptorAddress,
	}
}

func (c *clnBreezClient) Name() string {
	return c.name
}

func (c *clnBreezClient) Harness() *lntest.TestHarness {
	return c.harness
}

func (c *clnBreezClient) Node() lntest.LightningNode {
	return c.node
}

func (c *clnBreezClient) Start() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if !c.isInitialized {
		c.initialize()
		c.isInitialized = true
	}

	c.node.Start()
	c.startHtlcAcceptor()
}

func (c *clnBreezClient) ResetHtlcAcceptor() {
	c.htlcAcceptor = nil
}
func (c *clnBreezClient) SetHtlcAcceptor(totalMsat uint64) {
	c.htlcAcceptor = func(htlc *proto.HtlcAccepted) *proto.HtlcResolution {
		origPayload, err := hex.DecodeString(htlc.Onion.Payload)
		if err != nil {
			c.harness.T.Fatalf("failed to hex decode onion payload %s: %v", htlc.Onion.Payload, err)
		}
		bufReader := bytes.NewBuffer(origPayload)
		var b [8]byte
		varInt, err := sphinx.ReadVarInt(bufReader, &b)
		if err != nil {
			c.harness.T.Fatalf("Failed to read payload: %v", err)
		}

		innerPayload := make([]byte, varInt)
		if _, err := io.ReadFull(bufReader, innerPayload[:]); err != nil {
			c.harness.T.Fatalf("failed to decode payload %x: %v", innerPayload[:], err)
		}

		s, _ := tlv.NewStream()
		tlvMap, err := s.DecodeWithParsedTypes(bytes.NewReader(innerPayload))
		if err != nil {
			c.harness.T.Fatalf("DecodeWithParsedTypes failed for %x: %v", innerPayload[:], err)
		}

		amt := record.NewAmtToFwdRecord(&htlc.Htlc.AmountMsat)
		amtbuf := bytes.NewBuffer([]byte{})
		if err := amt.Encode(amtbuf); err != nil {
			c.harness.T.Fatalf("failed to encode AmtToFwd %x: %v", innerPayload[:], err)
		}

		uTlvMap := make(map[uint64][]byte)
		for t, b := range tlvMap {
			if t == record.AmtOnionType {
				uTlvMap[uint64(t)] = amtbuf.Bytes()
				continue
			}

			if t == record.MPPOnionType {
				addr := [32]byte{}
				copy(addr[:], b[:32])
				mppbuf := bytes.NewBuffer([]byte{})
				mpp := record.NewMPP(
					lnwire.MilliSatoshi(totalMsat),
					addr,
				)
				record := mpp.Record()
				record.Encode(mppbuf)
				uTlvMap[uint64(t)] = mppbuf.Bytes()
				continue
			}

			uTlvMap[uint64(t)] = b
		}
		tlvRecords := tlv.MapToRecords(uTlvMap)
		s, err = tlv.NewStream(tlvRecords...)
		if err != nil {
			c.harness.T.Fatalf("tlv.NewStream(%+v) error: %v", tlvRecords, err)
		}
		var newPayloadBuf bytes.Buffer
		err = s.Encode(&newPayloadBuf)
		if err != nil {
			c.harness.T.Fatalf("encode error: %v", err)
		}
		payload := hex.EncodeToString(newPayloadBuf.Bytes())

		return &proto.HtlcResolution{
			Correlationid: htlc.Correlationid,
			Outcome: &proto.HtlcResolution_Continue{
				Continue: &proto.HtlcContinue{
					Payload: &payload,
				},
			},
		}
	}
}

func (c *clnBreezClient) startHtlcAcceptor() {
	ctx, cancel := context.WithCancel(c.harness.Ctx)
	c.htlcAcceptorCancel = cancel

	go func() {
		for {
			if ctx.Err() != nil {
				return
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}

			conn, err := grpc.DialContext(
				ctx,
				c.htlcAcceptorAddress,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithKeepaliveParams(keepalive.ClientParameters{
					Time:    time.Duration(10) * time.Second,
					Timeout: time.Duration(10) * time.Second,
				}),
			)
			if err != nil {
				log.Printf("%s: Dial htlc acceptor error: %v", c.name, err)
				continue
			}

			client := proto.NewClnPluginClient(conn)
			acceptor, err := client.HtlcStream(ctx)
			if err != nil {
				log.Printf("%s: client.HtlcStream() error: %v", c.name, err)
				break
			}
			for {
				htlc, err := acceptor.Recv()
				if err != nil {
					log.Printf("%s: acceptor.Recv() error: %v", c.name, err)
					break
				}

				f := c.htlcAcceptor
				var resp *proto.HtlcResolution
				if f != nil {
					resp = f(htlc)
				}
				if resp == nil {
					resp = &proto.HtlcResolution{
						Correlationid: htlc.Correlationid,
						Outcome: &proto.HtlcResolution_Continue{
							Continue: &proto.HtlcContinue{},
						},
					}
				}

				err = acceptor.Send(resp)
				if err != nil {
					log.Printf("%s: acceptor.Send() error: %v", c.name, err)
					break
				}
			}
		}
	}()
}

func (c *clnBreezClient) initialize() error {
	var cleanups []*lntest.Cleanup

	pythonFilePath := filepath.Join(c.scriptDir, "zero_conf_plugin.py")
	pythonFile, err := os.OpenFile(pythonFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Errorf("failed to create python file '%s': %v", pythonFilePath, err)
	}
	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: python file", c.name),
		Fn:   pythonFile.Close,
	})

	pythonWriter := bufio.NewWriter(pythonFile)
	_, err = pythonWriter.WriteString(pluginContent)
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return fmt.Errorf("failed to write content to python file '%s': %v", pythonFilePath, err)
	}

	err = pythonWriter.Flush()
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return fmt.Errorf("failed to flush python file '%s': %v", pythonFilePath, err)
	}

	pluginFile, err := os.OpenFile(c.pluginFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return fmt.Errorf("failed to create plugin file '%s': %v", c.pluginFilePath, err)
	}
	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: python file", c.name),
		Fn:   pluginFile.Close,
	})

	pluginWriter := bufio.NewWriter(pluginFile)
	venvDir := filepath.Join(c.scriptDir, "venv")
	activatePath := filepath.Join(venvDir, "bin", "activate")
	_, err = pluginWriter.WriteString(fmt.Sprintf(pluginStartupContent, venvDir, activatePath, pythonFilePath))
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return fmt.Errorf("failed to write content to plugin file '%s': %v", c.pluginFilePath, err)
	}

	err = pluginWriter.Flush()
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return fmt.Errorf("failed to flush plugin file '%s': %v", c.pluginFilePath, err)
	}

	lntest.PerformCleanup(cleanups)
	return nil
}

func (c *clnBreezClient) Stop() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.htlcAcceptorCancel != nil {
		c.htlcAcceptorCancel()
		c.htlcAcceptorCancel = nil
	}
	return c.node.Stop()
}
