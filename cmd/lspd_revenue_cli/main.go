package main

import (
	"log"
	"os"

	"github.com/breez/lspd/build"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Name = "lspd_revenue_cli"
	app.Version = build.GetTag() + " commit=" + build.GetRevision()
	app.Usage = "get revenue data from lspd"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:     "database-url",
			Usage:    "postgres database url for lspd",
			Required: true,
		},
	}
	app.Commands = []cli.Command{
		exportForwardsCommand,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
