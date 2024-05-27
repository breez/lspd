package itest

import (
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

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

	lndNode := lntest.NewLndNode(h, miner, "lnd-lsp")
	clnNode := lntest.NewClnNode(h, miner, "cln-lsp")
	alice := lntest.NewLndNode(h, miner, "alice")
	breezClient := lntest.NewClnNode(h, miner, "breez-client")

	var beforeLspdWg sync.WaitGroup
	beforeLspdWg.Add(2)
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
		breezClient.Fund(1000000)
	}()

	beforeLspdWg.Wait()

	var allDoneWg sync.WaitGroup
	allDoneWg.Add(1)
	go func() {
		defer allDoneWg.Done()
		lndNode.WaitForSync()
		clnNode.WaitForSync()
		lndNode.Fund(10000000)
		clnNode.Fund(10000000)
		<-time.After(time.Second * 4)
		lndNode.WaitForSync()
		clnNode.WaitForSync()
		lChan := lndNode.OpenChannel(clnNode, &lntest.OpenChannelOptions{
			IsPublic:  true,
			AmountSat: 1000000,
		})
		lndNode.WaitForChannelReady(lChan)
	}()

	clientsWg.Wait()
	alice.WaitForSync()
	<-time.After(time.Second * 4)
	aChan := alice.OpenChannel(lndNode, &lntest.OpenChannelOptions{
		IsPublic:  true,
		AmountSat: publicChanAmount,
	})
	clnNode.WaitForSync()
	breezClient.WaitForSync()
	cChan := clnNode.OpenChannel(breezClient, &lntest.OpenChannelOptions{
		IsPublic:  false,
		AmountSat: 1000000,
	})
	alice.WaitForChannelReady(aChan)
	clnNode.WaitForChannelReady(cChan)
	alice.WaitForSync()
	lndNode.WaitForSync()
	clnNode.WaitForSync()
	breezClient.WaitForSync()
	allDoneWg.Wait()

	<-time.After(time.Second * 4)
	count := 20
	var invoices []string
	for i := 0; i < count; i++ {
		desc := fmt.Sprintf("invoice %d", i)
		bolt11 := breezClient.CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
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
			log.Printf("Payment succeeded")
		}(bolt11)
	}

	invoiceWg.Wait()
}
