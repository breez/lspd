package itest

import (
	"testing"

	"github.com/breez/lntest"
)

type testParams struct {
	t   *testing.T
	h   *lntest.TestHarness
	m   *lntest.Miner
	c   BreezClient
	lsp LspNode
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
