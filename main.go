package main

import (
	"fmt"
	"log"
	"os"

	"github.com/btcsuite/btcd/btcec"
)

var ()

func main() {

	if len(os.Args) > 1 && os.Args[1] == "genkey" {
		p, err := btcec.NewPrivateKey(btcec.S256())
		if err != nil {
			log.Fatalf("btcec.NewPrivateKey() error: %v", err)
		}
		fmt.Printf("LSPD_PRIVATE_KEY=\"%x\"\n", p.Serialize())
		return
	}

	if os.Getenv("RUN_CLIGHTNING") == "true" {
		run_clightning()
	} else if os.Getenv("RUN_LND") == "true" {
		run_lnd()
	}

}
