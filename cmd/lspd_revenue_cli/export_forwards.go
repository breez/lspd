package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli"
)

var exportForwardsCommand = cli.Command{
	Name:  "export-forwards",
	Usage: "Export forwards with a given peer correlated to an api key for a given time period.",
	Flags: append(
		[]cli.Flag{
			cli.StringFlag{
				Name:     "node",
				Required: true,
				Usage:    "The public key of your own lightning node to export forwards for.",
			},
			cli.StringFlag{
				Name:     "peer",
				Required: true,
				Usage:    "The public key of the peer to export the forwards for.",
			},
		},
		timeRangeFlags...,
	),
	Action: exportForwards,
}

func exportForwards(ctx *cli.Context) error {
	node := ctx.String("node")
	if node == "" {
		return fmt.Errorf("node is required")
	}
	nodeId, err := hex.DecodeString(node)
	if err != nil || len(nodeId) != 33 {
		return fmt.Errorf("node is not a pubkey")
	}

	peer := ctx.String("peer")
	if peer == "" {
		return fmt.Errorf("peer is required")
	}
	peerId, err := hex.DecodeString(peer)
	if err != nil || len(peerId) != 33 {
		return fmt.Errorf("peer is not a pubkey")
	}

	timeRange, err := getTimeRange(ctx)
	if err != nil {
		return err
	}

	store, err := getStore(ctx)
	if err != nil {
		return err
	}
	result, err := store.ExportTokenForwardsForExternalNode(context.Background(), timeRange, nodeId, peerId)
	if err != nil {
		return err
	}

	j, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}

	_, err = os.Stdout.Write(j)
	return err
}
