package main

import (
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/urfave/cli"
)

var genKeyCommand = cli.Command{
	Name:   "genkey",
	Usage:  "Generate an lspd private key for the LSPD_PRIVATE_KEY environment variable.",
	Action: genKey,
}

func genKey(cliCtx *cli.Context) error {
	p, err := btcec.NewPrivateKey()
	if err != nil {
		return fmt.Errorf("btcec.NewPrivateKey() error: %w", err)
	}
	fmt.Printf("LSPD_PRIVATE_KEY=\"%x\"\n", p.Serialize())
	return nil
}
