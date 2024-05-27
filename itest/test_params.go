package itest

import (
	"testing"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
)

type LspFunc func(h *lntest.TestHarness, m *lntest.Miner, mem *mempoolApi, c *config.NodeConfig) (lntest.LightningNode, *Lspd)
type ClientFunc func(h *lntest.TestHarness, m *lntest.Miner) BreezClient

type testParams struct {
	t          *testing.T
	h          *lntest.TestHarness
	m          *lntest.Miner
	mem        *mempoolApi
	c          BreezClient
	lspd       *Lspd
	node       lntest.LightningNode
	lspFunc    LspFunc
	clientFunc ClientFunc
}

func (h *testParams) T() *testing.T {
	return h.t
}

func (h *testParams) Miner() *lntest.Miner {
	return h.m
}

func (h *testParams) Mempool() *mempoolApi {
	return h.mem
}

func (h *testParams) Lspd() *Lspd {
	return h.lspd
}

func (h *testParams) Node() lntest.LightningNode {
	return h.node
}

func (h *testParams) Harness() *lntest.TestHarness {
	return h.h
}

func (h *testParams) BreezClient() BreezClient {
	return h.c
}
