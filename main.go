package main

import (
	"fmt"
	"log"
	"os"

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

	go interceptor.Start()

	go forwardingHistorySynchronize(client)
	go channelsSynchronize(client)

	s := NewGrpcServer()
	err = s.Start()
	if err != nil {
		log.Fatalf("%v", err)
	}

	log.Printf("lspd exited")
}
