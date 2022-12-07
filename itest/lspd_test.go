package itest

import (
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/breez/lntest"
)

var defaultTimeout time.Duration = time.Second * 120

func TestLspd(t *testing.T) {
	testCases := allTestCases
	// runTests(t, testCases, "LND-lspd", func(h *lntest.TestHarness, m *lntest.Miner) LspNode {
	// 	return NewLndLspdNode(h, m, "lsp")
	// })

	runTests(t, testCases, "CLN-lspd", func(h *lntest.TestHarness, m *lntest.Miner) LspNode {
		return NewClnLspdNode(h, m, "lsp")
	})
}

func runTests(t *testing.T, testCases []*testCase, prefix string, lspFunc func(h *lntest.TestHarness, m *lntest.Miner) LspNode) {
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(fmt.Sprintf("%s: %s", prefix, testCase.name), func(t *testing.T) {
			runTest(t, testCase, prefix, lspFunc)
		})
	}
}

func runTest(t *testing.T, testCase *testCase, prefix string, lspFunc func(h *lntest.TestHarness, m *lntest.Miner) LspNode) {
	log.Printf("%s: Running test case '%s'", prefix, testCase.name)
	var dd time.Duration
	to := testCase.timeout
	if to == dd {
		to = defaultTimeout
	}

	deadline := time.Now().Add(to)
	log.Printf("Using deadline %v", deadline.String())

	harness := lntest.NewTestHarness(t, deadline)
	defer harness.TearDown()

	log.Printf("Creating miner")
	miner := lntest.NewMiner(harness)
	log.Printf("Creating lsp")
	lsp := lspFunc(harness, miner)
	log.Printf("Run testcase")
	testCase.test(harness, lsp, miner)
}

type testCase struct {
	name    string
	test    func(h *lntest.TestHarness, lsp LspNode, miner *lntest.Miner)
	timeout time.Duration
}

var allTestCases = []*testCase{
	{
		name: "testOpenZeroConfChannelOnReceive",
		test: testOpenZeroConfChannelOnReceive,
	},
	{
		name: "testOpenZeroConfSingleHtlc",
		test: testOpenZeroConfSingleHtlc,
	},
}
