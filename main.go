package main

import (
	"fmt"
	"log"
	"os"

	"github.com/btcsuite/btcd/btcec"
)

var ()

func main() {
	conf_flag := "clightning"

	if len(os.Args) > 1 && os.Args[1] == "genkey" {
		p, err := btcec.NewPrivateKey(btcec.S256())
		if err != nil {
			log.Fatalf("btcec.NewPrivateKey() error: %v", err)
		}
		fmt.Printf("LSPD_PRIVATE_KEY=\"%x\"\n", p.Serialize())
		return
	}

	if conf_flag == "clightning" {
		run_clightning()
	} else {
		run_lnd()
	}

}
