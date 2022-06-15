package main

import (
	"fmt"
	"log"
	"os"

	"github.com/btcsuite/btcd/btcec"
)

const (
	publicChannelAmount       = 1_000_183
	targetConf                = 6
	minHtlcMsat               = 600
	baseFeeMsat               = 1000
	feeRate                   = 0.000001
	timeLockDelta             = 144
	channelFeePermyriad       = 40
	channelMinimumFeeMsat     = 2_000_000
	additionalChannelCapacity = 100_000
	maxInactiveDuration       = 45 * 24 * 3600
)

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
