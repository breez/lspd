package itest

import (
	"testing"

	"github.com/breez/lntest"
	"github.com/breez/lspd/config"
)

type LspFunc func(h *lntest.TestHarness, m *lntest.Miner, c *config.NodeConfig) LspNode
type ClientFunc func(h *lntest.TestHarness, m *lntest.Miner) BreezClient

type testParams struct {
	t          *testing.T
	h          *lntest.TestHarness
	m          *lntest.Miner
	c          BreezClient
	lsp        LspNode
	lspFunc    LspFunc
	clientFunc ClientFunc
}

func (h *testParams) T() *testing.T {
	return h.t
}

func (h *testParams) Miner() *lntest.Miner {
	return h.m
}

func (h *testParams) Lsp() LspNode {
	return h.lsp
}

func (h *testParams) Harness() *lntest.TestHarness {
	return h.h
}

func (h *testParams) BreezClient() BreezClient {
	return h.c
}
