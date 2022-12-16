package itest

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/breez/lntest"
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
	return {'result': 'continue', 'mindepth': mindepth}

plugin.run()
`

var pluginStartupContent string = `python3 -m venv %s > /dev/null 2>&1
source %s > /dev/null 2>&1
pip install pyln-client > /dev/null 2>&1
python %s
`

type clnBreezClient struct {
	name           string
	scriptDir      string
	pluginFilePath string
	harness        *lntest.TestHarness
	isInitialized  bool
	node           *lntest.ClnNode
	mtx            sync.Mutex
}

func newClnBreezClient(h *lntest.TestHarness, m *lntest.Miner, name string) BreezClient {
	scriptDir := h.GetDirectory(name)
	pluginFilePath := filepath.Join(scriptDir, "start_zero_conf_plugin.sh")
	node := lntest.NewClnNode(
		h,
		m,
		name,
		fmt.Sprintf("--plugin=%s", pluginFilePath),
		// NOTE: max-concurrent-htlcs is 30 on mainnet by default. In cln V22.11
		// there is a check for 'all dust' commitment transactions. The max
		// concurrent HTLCs of both sides of the channel * dust limit must be
		// lower than the channel capacity in order to open a zero conf zero
		// reserve channel. Relevant code:
		// https://github.com/ElementsProject/lightning/blob/774d16a72e125e4ae4e312b9e3307261983bec0e/openingd/openingd.c#L481-L520
		"--max-concurrent-htlcs=30",
	)

	return &clnBreezClient{
		name:           name,
		harness:        h,
		node:           node,
		scriptDir:      scriptDir,
		pluginFilePath: pluginFilePath,
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
	return c.node.Stop()
}
