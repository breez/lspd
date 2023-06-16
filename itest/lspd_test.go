package itest

import (
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/breez/lntest"
	"github.com/breez/lspd/config"
)

var defaultTimeout time.Duration = time.Second * 120

func TestLspd(t *testing.T) {
	testCases := allTestCases
	runTests(t, testCases, "LND-lspd", lndLspFunc, lndClientFunc)
	runTests(t, testCases, "CLN-lspd", clnLspFunc, clnClientFunc)
}

func lndLspFunc(h *lntest.TestHarness, m *lntest.Miner, mem *mempoolApi, c *config.NodeConfig) LspNode {
	return NewLndLspdNode(h, m, mem, "lsp", c)
}

func clnLspFunc(h *lntest.TestHarness, m *lntest.Miner, mem *mempoolApi, c *config.NodeConfig) LspNode {
	return NewClnLspdNode(h, m, mem, "lsp", c)
}

func lndClientFunc(h *lntest.TestHarness, m *lntest.Miner) BreezClient {
	return newLndBreezClient(h, m, "breez-client")
}

func clnClientFunc(h *lntest.TestHarness, m *lntest.Miner) BreezClient {
	return newClnBreezClient(h, m, "breez-client")
}

func runTests(
	t *testing.T,
	testCases []*testCase,
	prefix string,
	lspFunc LspFunc,
	clientFunc ClientFunc,
) {
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(fmt.Sprintf("%s: %s", prefix, testCase.name), func(t *testing.T) {
			runTest(t, testCase, prefix, lspFunc, clientFunc)
		})
	}
}

func runTest(
	t *testing.T,
	testCase *testCase,
	prefix string,
	lspFunc LspFunc,
	clientFunc ClientFunc,
) {
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

	log.Printf("Creating mempool api")
	mem := NewMempoolApi(h)
	mem.Start()

	log.Printf("Creating lsp")
	var lsp LspNode
	if !testCase.skipCreateLsp {
		lsp = lspFunc(h, miner, mem, nil)
		lsp.Start()
	}
	c := clientFunc(h, miner)
	c.Start()
	log.Printf("Run testcase")
	testCase.test(&testParams{
		t:          t,
		h:          h,
		m:          miner,
		mem:        mem,
		c:          c,
		lsp:        lsp,
		lspFunc:    lspFunc,
		clientFunc: clientFunc,
	})
}

type testCase struct {
	name          string
	test          func(t *testParams)
	skipCreateLsp bool
	timeout       time.Duration
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
	{
		name: "testProbing",
		test: testProbing,
	},
	{
		name: "testInvalidCltv",
		test: testInvalidCltv,
	},
	{
		name: "registerPaymentWithTag",
		test: registerPaymentWithTag,
	},
	{
		name:          "testOpenZeroConfUtxo",
		test:          testOpenZeroConfUtxo,
		skipCreateLsp: true,
	},
	{
		name: "testDynamicFeeFlow",
		test: testDynamicFeeFlow,
	},
	{
		name: "testOfflineNotificationPaymentRegistered",
		test: testOfflineNotificationPaymentRegistered,
	},
	{
		name: "testOfflineNotificationRegularForward",
		test: testOfflineNotificationRegularForward,
	},
	{
		name: "testOfflineNotificationZeroConfChannel",
		test: testOfflineNotificationZeroConfChannel,
	},
}
