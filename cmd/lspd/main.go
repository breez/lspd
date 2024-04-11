package main

import (
	"log"
	"os"

	"github.com/breez/lspd/build"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Name = "lspd"
	app.Version = build.GetTag() + " commit=" + build.GetRevision()
	app.Usage = "LSP implementation for LND and CLN"
	app.Action = runLspd
	app.Commands = []cli.Command{
		genKeyCommand,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
