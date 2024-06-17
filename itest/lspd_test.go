package itest

import (
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
)

var defaultTimeout time.Duration = time.Second * 120

func TestLspd(t *testing.T) {
	if testing.Short() {
		t.Skip()
		return
	}
	testCases := allTestCases
	var lndTestCases []*testCase
	for _, c := range testCases {
		if !c.isLsps2 {
			lndTestCases = append(lndTestCases, c)
		}
	}
	runTests(t, lndTestCases, "LND-lsp-CLN-client", lndLspFunc, clnClientFunc)
	runTests(t, lndTestCases, "LND-lsp-LND-client", legacyOnionLndLspFunc, lndClientFunc)
	runTests(t, testCases, "CLN-lsp-CLN-client", clnLspFunc, clnClientFunc)
}

func legacyOnionLndLspFunc(h *lntest.TestHarness, m *lntest.Miner, mem *mempoolApi, c *config.NodeConfig) (lntest.LightningNode, *Lspd) {
	cfg := c
	if cfg == nil {
		cfg = &config.NodeConfig{}
	}

	cfg.LegacyOnionTokens = []string{"hello0"}

	node, config := NewLndLspdNode(h, m, "lsp-node")
	cfg.Lnd = config
	lspd, err := NewLspd(h, "lsp", mem, &NodeWithConfig{
		Node:   node,
		Config: cfg,
	})
	if err != nil {
		h.T.Fatalf("Failed to initialize lspd: %v", err)
	}

	return node, lspd
}

func lndLspFunc(h *lntest.TestHarness, m *lntest.Miner, mem *mempoolApi, c *config.NodeConfig) (lntest.LightningNode, *Lspd) {
	cfg := c
	if cfg == nil {
		cfg = &config.NodeConfig{}
	}

	node, config := NewLndLspdNode(h, m, "lsp-node")
	cfg.Lnd = config
	lspd, err := NewLspd(h, "lsp", mem, &NodeWithConfig{
		Node:   node,
		Config: cfg,
	})
	if err != nil {
		h.T.Fatalf("Failed to initialize lspd: %v", err)
	}

	return node, lspd
}

func clnLspFunc(h *lntest.TestHarness, m *lntest.Miner, mem *mempoolApi, c *config.NodeConfig) (lntest.LightningNode, *Lspd) {
	cfg := c
	if cfg == nil {
		cfg = &config.NodeConfig{}
	}

	node, config := NewClnLspdNode(h, m, "lsp-node")
	cfg.Cln = config
	lspd, err := NewLspd(h, "lsp", mem, &NodeWithConfig{
		Node:   node,
		Config: cfg,
	})
	if err != nil {
		h.T.Fatalf("Failed to initialize lspd: %v", err)
	}

	return node, lspd
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
	var lspd *Lspd
	var node lntest.LightningNode
	if !testCase.skipCreateLsp {
		node, lspd = lspFunc(h, miner, mem, nil)
		node.Start()
		lspd.Start()
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
		lspd:       lspd,
		node:       node,
		lspFunc:    lspFunc,
		clientFunc: clientFunc,
	})
}

type testCase struct {
	name          string
	test          func(t *testParams)
	skipCreateLsp bool
	isLsps2       bool
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
	{
		name: "testNotificationsUnsubscribe",
		test: testNotificationsUnsubscribe,
	},
	{
		name:    "testLsps0GetProtocolVersions",
		test:    testLsps0GetProtocolVersions,
		isLsps2: true,
	},
	{
		name:    "testLsps2GetVersions",
		test:    testLsps2GetVersions,
		isLsps2: true,
	},
	{
		name:    "testLsps2GetInfo",
		test:    testLsps2GetInfo,
		isLsps2: true,
	},
	{
		name:    "testLsps2Buy",
		test:    testLsps2Buy,
		isLsps2: true,
	},
	{
		name:    "testLsps2HappyFlow",
		test:    testLsps2HappyFlow,
		isLsps2: true,
	},
	{
		name:    "testLsps2NoBalance",
		test:    testLsps2NoBalance,
		isLsps2: true,
	},
	{
		name:          "testLsps2ZeroConfUtxo",
		test:          testLsps2ZeroConfUtxo,
		isLsps2:       true,
		skipCreateLsp: true,
	},
	{
		name: "testRestartLspNode",
		test: testRestartLspNode,
	},
}
