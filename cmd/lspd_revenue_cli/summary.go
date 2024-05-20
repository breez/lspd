package main

import (
	"github.com/urfave/cli"
)

var statsCommand = cli.Command{
	Name:   "summary",
	Usage:  "Get an lsp statistics summary for the specified time interval.",
	Flags:  timeRangeFlags,
	Action: stats,
}

func stats(ctx *cli.Context) error {
	timeRange, err := getTimeRange(ctx)
	if err != nil {
		return err
	}

	// amount of settled forwards
	// total amount forwarded
	// total fees
	// total channel opens
	// total channel opens where we didn't get any fees
	// total fees from channel opens
}
