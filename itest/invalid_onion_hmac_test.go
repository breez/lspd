package itest

import (
	"log"
	"sync"
	"testing"
	"time"

	"github.com/breez/lspd/itest/lntest"
)

func TestInvalidOnionHmac(t *testing.T) {
	h := lntest.NewTestHarness(t, time.Now().Add(time.Minute*5))
	defer func() {
		h.TearDown()
	}()
	log.Printf("Creating miner")
	miner := lntest.NewMiner(h)
	miner.Start()

	lndSender := lntest.NewLndNode(h, miner, "lnd-sender")
	lndRouter := lntest.NewLndNode(h, miner, "lnd-router")
	clnRouter := lntest.NewClnNode(h, miner, "cln-router")
	clnRecipient := lntest.NewClnNode(h, miner, "cln-recipient")

	var startupWg sync.WaitGroup
	startupWg.Add(4)
	go func() {
		defer startupWg.Done()
		log.Printf("Starting LND routing node")
		lndRouter.Start()
		lndRouter.Fund(10000000)
		lndRouter.WaitForSync()
	}()
	go func() {
		defer startupWg.Done()
		log.Printf("Starting CLN routing node")
		clnRouter.Start()
		clnRouter.Fund(10000000)
		clnRouter.WaitForSync()
	}()
	go func() {
		defer startupWg.Done()
		log.Printf("Starting sender")
		lndSender.Start()
		lndSender.Fund(10000000)
		lndSender.WaitForSync()
	}()
	go func() {
		defer startupWg.Done()
		log.Printf("Starting recipient")
		clnRecipient.Start()
		clnRecipient.Fund(10000000)
		clnRecipient.WaitForSync()
	}()

	startupWg.Wait()

	aChan := lndSender.OpenChannel(lndRouter, &lntest.OpenChannelOptions{
		IsPublic:  true,
		AmountSat: 1000000,
	})
	bChan := lndRouter.OpenChannel(clnRouter, &lntest.OpenChannelOptions{
		IsPublic:  true,
		AmountSat: 1000000,
	})
	cChan := clnRouter.OpenChannel(clnRecipient, &lntest.OpenChannelOptions{
		IsPublic:  false,
		AmountSat: 1000000,
	})
	lndSender.WaitForChannelReady(aChan)
	lndRouter.WaitForChannelReady(bChan)
	clnRouter.WaitForChannelReady(cChan)
	<-time.After(time.Second * 4)

	desc := "paying invoice fails with INVALID_ONION_HMAC"
	bolt11 := clnRecipient.CreateBolt11Invoice(&lntest.CreateInvoiceOptions{
		AmountMsat:      10000,
		Description:     &desc,
		IncludeHopHints: true,
	})

	log.Printf("Paying invoice %s", bolt11.Bolt11)
	lndSender.Pay(bolt11.Bolt11)
	log.Printf("Payment succeeded")
}
