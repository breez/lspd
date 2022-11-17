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

	client = NewLndClient()
	interceptor := NewLndHtlcInterceptor(client)
	s := NewGrpcServer()

	info, err := client.GetInfo()
	if err != nil {
		log.Fatalf("client.GetInfo() error: %v", err)
	}
	if nodeName == "" {
		nodeName = info.Alias
	}
	if nodePubkey == "" {
		nodePubkey = info.Pubkey
	}

	go forwardingHistorySynchronize(client)
	go channelsSynchronize(client)

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
