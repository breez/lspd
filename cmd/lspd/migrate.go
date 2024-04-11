package main

import (
	"fmt"

	"github.com/breez/lspd/postgresql"
	"github.com/urfave/cli"
)

var migrateCommand = cli.Command{
	Name:   "migrate",
	Usage:  "Migrate the lspd database to the latest version.",
	Action: migrate,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:     "database-url",
			Usage:    "Postgres database url. The configured user needs permissions to create/drop/modify tables.",
			Required: true,
		},
	},
}

func migrate(cliCtx *cli.Context) error {
	dbUrl := cliCtx.String("database-url")
	if dbUrl == "" {
		return fmt.Errorf("database-url is required")
	}

	return postgresql.Migrate(dbUrl)
}
