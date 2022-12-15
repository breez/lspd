package main

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "genkey" {
		p, err := btcec.NewPrivateKey()
		if err != nil {
			log.Fatalf("btcec.NewPrivateKey() error: %v", err)
		}
		fmt.Printf("LSPD_PRIVATE_KEY=\"%x\"\n", p.Serialize())
		return
	}

	err := pgConnect()
	if err != nil {
		log.Fatalf("pgConnect() error: %v", err)
	}

	runCln := os.Getenv("RUN_CLN") == "true"
	runLnd := os.Getenv("RUN_LND") == "true"

	if runCln && runLnd {
		log.Fatalf("One of RUN_CLN or RUN_LND must be true, not both.")
	}

	if !runCln && !runLnd {
		log.Fatalf("Either RUN_CLN or RUN_LND must be true.")
	}

	var interceptor HtlcInterceptor
	if runCln {
		interceptor = NewClnHtlcInterceptor()
	}

	if runLnd {
		interceptor = NewLndHtlcInterceptor()
	}

	s := NewGrpcServer()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		err := interceptor.Start()
		if err == nil {
			log.Printf("Interceptor stopped.")
		} else {
			log.Printf("FATAL. Interceptor stopped with error: %v", err)
		}
		s.Stop()
		wg.Done()
	}()

	client = interceptor.WaitStarted()
	info, err := client.GetInfo()
	if err != nil {
		log.Fatalf("client.GetInfo() error: %v", err)
	}
	log.Printf("Connected to node '%s', alias '%s'", info.Pubkey, info.Alias)

	if nodeName == "" {
		nodeName = info.Alias
	}
	if nodePubkey == "" {
		nodePubkey = info.Pubkey
	}

	go func() {
		err := s.Start()
		if err == nil {
			log.Printf("GRPC server stopped.")
		} else {
			log.Printf("FATAL. GRPC server stopped with error: %v", err)
		}

		interceptor.Stop()
		wg.Done()
	}()

	wg.Wait()
	log.Printf("lspd exited")
}
