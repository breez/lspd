package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/breez/lspd"
	"github.com/breez/lspd/build"
	"github.com/breez/lspd/config"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Name = "lspd"
	app.Version = build.GetTag() + " commit=" + build.GetRevision()
	app.Usage = "LSP implementation for LND and CLN"
	app.Action = func(cliCtx *cli.Context) error {
		ctx, cancel := context.WithCancel(context.Background())
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-c
			log.Printf("Received stop signal %v. Stopping.", sig)
			cancel()
		}()

		config, err := config.LoadConfig()
		if err != nil {
			return err
		}

		return lspd.Main(ctx, config)
	}
	app.Commands = []cli.Command{
		genKeyCommand,
		migrateCommand,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
