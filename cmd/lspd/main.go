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
	app.Action = func(ctx *cli.Context) error {
		return Main()
	}
	app.Commands = []cli.Command{
		genKeyCommand,
		migrateCommand,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
