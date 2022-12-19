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
	runTests(t, testCases, "LND-lspd", func(h *lntest.TestHarness, m *lntest.Miner) (LspNode, BreezClient) {
		return NewLndLspdNode(h, m, "lsp"), newLndBreezClient(h, m, "breez-client")
	})

	runTests(t, testCases, "CLN-lspd", func(h *lntest.TestHarness, m *lntest.Miner) (LspNode, BreezClient) {
		return NewClnLspdNode(h, m, "lsp"), newClnBreezClient(h, m, "breez-client")
	})
}

func runTests(t *testing.T, testCases []*testCase, prefix string, nodesFunc func(h *lntest.TestHarness, m *lntest.Miner) (LspNode, BreezClient)) {
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(fmt.Sprintf("%s: %s", prefix, testCase.name), func(t *testing.T) {
			runTest(t, testCase, prefix, nodesFunc)
		})
	}
}

func runTest(t *testing.T, testCase *testCase, prefix string, nodesFunc func(h *lntest.TestHarness, m *lntest.Miner) (LspNode, BreezClient)) {
	log.Printf("%s: Running test case '%s'", prefix, testCase.name)
	var dd time.Duration
	to := testCase.timeout
	if to == dd {
		to = defaultTimeout
	}

	deadline := time.Now().Add(to)
	log.Printf("Using deadline %v", deadline.String())

	h := lntest.NewTestHarness(t, deadline)
	defer h.TearDown()

	log.Printf("Creating miner")
	miner := lntest.NewMiner(h)
	miner.Start()
	log.Printf("Creating lsp")
	lsp, c := nodesFunc(h, miner)
	lsp.Start()
	c.Start()
	log.Printf("Run testcase")
	testCase.test(&testParams{
		t:   t,
		h:   h,
		m:   miner,
		c:   c,
		lsp: lsp,
	})
}

type testCase struct {
	name    string
	test    func(t *testParams)
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
	{
		name: "testZeroReserve",
		test: testZeroReserve,
	},
	{
		name: "testFailureBobOffline",
		test: testFailureBobOffline,
	},
	{
		name: "testNoBalance",
		test: testNoBalance,
	},
	{
		name: "testRegularForward",
		test: testRegularForward,
	},
}
