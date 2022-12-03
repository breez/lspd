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
	// runTests(t, testCases, "LND-lspd", func(h *lntest.TestHarness, m *lntest.Miner, t time.Time) LspNode {
	// 	return NewLndLspdNode(h, m, "lsp", t)
	// })

	runTests(t, testCases, "CLN-lspd", func(h *lntest.TestHarness, m *lntest.Miner, t time.Time) LspNode {
		return NewClnLspdNode(h, m, "lsp", t)
	})
}

func runTests(t *testing.T, testCases []*testCase, prefix string, lspFunc func(h *lntest.TestHarness, m *lntest.Miner, t time.Time) LspNode) {
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(fmt.Sprintf("%s: %s", prefix, testCase.name), func(t *testing.T) {
			runTest(t, testCase, prefix, lspFunc)
		})
	}
}

func runTest(t *testing.T, testCase *testCase, prefix string, lspFunc func(h *lntest.TestHarness, m *lntest.Miner, t time.Time) LspNode) {
	log.Printf("%s: Running test case '%s'", prefix, testCase.name)
	harness := lntest.NewTestHarness(t)
	defer harness.TearDown()

	var dd time.Duration
	to := testCase.timeout
	if to == dd {
		to = defaultTimeout
	}

	timeout := time.Now().Add(to)
	log.Printf("Using timeout %v", timeout.String())
	log.Printf("Creating miner")
	miner := lntest.NewMiner(harness)
	log.Printf("Creating lsp")
	lsp := lspFunc(harness, miner, timeout)
	log.Printf("Run testcase")
	testCase.test(harness, lsp, miner, timeout)
}

type testCase struct {
	name    string
	test    func(h *lntest.TestHarness, lsp LspNode, miner *lntest.Miner, timeout time.Time)
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
