package itest

import (
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
)

func TestTwoLsp(t *testing.T) {
	h := lntest.NewTestHarness(t, time.Now().Add(defaultTimeout))
	defer func() {
		h.TearDown()
	}()
	log.Printf("Creating miner")
	miner := lntest.NewMiner(h)
	miner.Start()

	lndNode, lndConf := NewLndLspdNode(h, miner, "lnd-lsp")
	clnNode, clnConf := NewClnLspdNode(h, miner, "cln-lsp")
	mem := NewMempoolApi(h)
	alice := lntest.NewLndNode(h, miner, "alice")
	breezClient := newClnBreezClient(h, miner, "breez-client")

	var beforeLspdWg sync.WaitGroup
	beforeLspdWg.Add(3)
	go func() {
		defer beforeLspdWg.Done()
		log.Printf("Starting LND lsp node")
		lndNode.Start()
	}()
	go func() {
		defer beforeLspdWg.Done()
		log.Printf("Starting CLN lsp node")
		clnNode.Start()
	}()
	go func() {
		defer beforeLspdWg.Done()
		log.Printf("Starting mempool api")
		mem.Start()
	}()

	var clientsWg sync.WaitGroup
	clientsWg.Add(2)
	go func() {
		defer clientsWg.Done()
		log.Printf("Starting Alice")
		alice.Start()
		alice.Fund(10000000)
	}()
	go func() {
		defer clientsWg.Done()
		log.Printf("Starting Breez Client")
		breezClient.Start()
		breezClient.Node().Fund(1000000)
	}()

	beforeLspdWg.Wait()

	var allDoneWg sync.WaitGroup
	allDoneWg.Add(2)
	go func() {
		defer allDoneWg.Done()
		lndNode.Fund(10000000)
		clnNode.Fund(10000000)
		lChan := lndNode.OpenChannel(clnNode, &lntest.OpenChannelOptions{
			IsPublic:  true,
			AmountSat: 1000000,
		})
		lndNode.WaitForChannelReady(lChan)
	}()

	// var lspd *Lspd
	go func() {
		defer allDoneWg.Done()
		l, err := NewLspd(
			h,
			"lspd",
			mem,
			&NodeWithConfig{
				Node: lndNode,
				Config: &config.NodeConfig{
					Lnd: lndConf,
				},
			},
			&NodeWithConfig{
				Node: clnNode,
				Config: &config.NodeConfig{
					Cln: clnConf,
				},
			},
		)
		if err != nil {
			h.Fatalf("Failed to create lspd: %v", err)
		}

		// lspd = l
		log.Printf("Starting lspd")
		l.Start()
		<-time.After(htlcInterceptorDelay)
	}()

	clientsWg.Wait()
	aChan := alice.OpenChannel(lndNode, &lntest.OpenChannelOptions{
		IsPublic:  true,
		AmountSat: publicChanAmount,
	})
	cChan := clnNode.OpenChannel(breezClient.Node(), &lntest.OpenChannelOptions{
		IsPublic:  false,
		AmountSat: 1000000,
	})
	alice.WaitForChannelReady(aChan)
	clnNode.WaitForChannelReady(cChan)
	alice.WaitForSync()
	lndNode.WaitForSync()
	clnNode.WaitForSync()
	breezClient.Node().WaitForSync()
	allDoneWg.Wait()

	count := 20
	var invoices []string
	for i := 0; i < count; i++ {
		desc := fmt.Sprintf("invoice %d", i)
		bolt11 := breezClient.Node().CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
			AmountMsat:      10000,
			Description:     &desc,
			IncludeHopHints: true,
		})
		invoices = append(invoices, bolt11.Bolt11)
	}

	var invoiceWg sync.WaitGroup
	invoiceWg.Add(count)
	for _, bolt11 := range invoices {
		go func(bolt11 string) {
			defer invoiceWg.Done()
			log.Printf("Paying invoice %s", bolt11)
			alice.Pay(bolt11)
		}(bolt11)
	}

	invoiceWg.Wait()
}
