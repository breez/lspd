package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/breez/lspd/postgresql"
	"github.com/urfave/cli"
)

const (
	FirstBehind  = -1
	SecondBehind = 1
)

var revenueCommand = cli.Command{
	Name:  "revenue",
	Usage: "Get a revenue report.",
	Flags: []cli.Flag{
		cli.Uint64Flag{
			Name:     "start",
			Required: true,
			Usage:    "Start time of forwards taken into account as a UTC unix timestamp in seconds.",
		},
		cli.Uint64Flag{
			Name:     "end",
			Required: true,
			Usage:    "End time of forwards taken into account as a UTC unix timestamp in seconds.",
		},
		cli.StringFlag{
			Name:     "import",
			Required: false,
			Usage:    "Optional imports to consider when generating the revenue report. Imports are files generated from the export-forwards command on other nodes. Used to correlate local forwards to api keys. Can be a glob pattern.",
		},
	},
	Action: revenue,
}

func revenue(ctx *cli.Context) error {
	start := ctx.Uint64("start")
	if start == 0 {
		return fmt.Errorf("start is required")
	}

	end := ctx.Uint64("end")
	if end == 0 {
		return fmt.Errorf("end is required")
	}

	startNs := start * 1_000_000_000
	endNs := end * 1_000_000_000
	if startNs > endNs {
		return fmt.Errorf("start cannot be after end")
	}

	importedForwards, err := getImportedForwards(ctx, startNs, endNs)
	if err != nil {
		return err
	}

	store, err := getStore(ctx)
	if err != nil {
		return err
	}

	forwardsSortedIn, err := store.GetForwards(context.Background(), startNs, endNs)
	if err != nil {
		return err
	}

	forwardsSortedOut := append([]*postgresql.RevenueForward(nil), forwardsSortedIn...)
	sort.SliceStable(forwardsSortedOut, func(i, j int) bool {
		first := forwardsSortedOut[i]
		second := forwardsSortedOut[j]
		nodeCompare := bytes.Compare(first.Nodeid, second.Nodeid)
		if nodeCompare < 0 {
			return true
		}
		if nodeCompare > 0 {
			return false
		}

		peerCompare := bytes.Compare(first.PeeridOut, second.PeeridOut)
		if peerCompare < 0 {
			return true
		}
		if peerCompare > 0 {
			return false
		}

		if first.AmtMsatOut < second.AmtMsatOut {
			return true
		}
		if first.AmtMsatOut > second.AmtMsatOut {
			return false
		}

		if first.ResolvedTime < second.ResolvedTime {
			return true
		}
		if first.ResolvedTime > second.ResolvedTime {
			return false
		}

		return false
	})

	// Imported forwards help correlate forwards to tokens
	matchImportedForwardsSend(forwardsSortedIn, importedForwards)
	matchImportedForwardsReceive(forwardsSortedOut, importedForwards)

	// Match forwards from our own nodes multiple times, each iteration represents a 'hop'.
	// Moving information about the token used in the route one hop further.
	for i := 0; i < 3; i++ {
		matchInternalForwards(forwardsSortedIn, forwardsSortedOut)
	}

	openChannelHtlcs, err := store.GetOpenChannelHtlcs(context.Background(), startNs, endNs)
	if err != nil {
		return err
	}

	// Some htlcs were used for channel opens. These are matched to actual settled forwards
	// to know the part of the fees that were made by channel opens rather than regular forwarding.
	matchOpenChannelHtlcs(forwardsSortedOut, openChannelHtlcs)

	revenue := calculateRevenue(forwardsSortedIn)
	j, err := json.Marshal(revenue)
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}

	log.Println(j)
	return nil
}

func getImportedForwards(ctx *cli.Context, startNs, endNs uint64) ([]*importedForward, error) {
	var importedForwards []*importedForward
	importFiles := ctx.String("import")
	if importFiles != "" {
		matches, err := filepath.Glob(importFiles)
		if err != nil {
			return nil, fmt.Errorf("failed to read import files: %w", err)
		}

		for _, match := range matches {
			forwards, err := readForwards(match, startNs, endNs)
			if err != nil {
				return nil, err
			}

			importedForwards = append(importedForwards, forwards...)
		}
	}

	sort.SliceStable(importedForwards, func(i, j int) bool {
		first := importedForwards[i]
		second := importedForwards[j]
		nodeCompare := bytes.Compare(first.nodeid, second.nodeid)
		if nodeCompare < 0 {
			return true
		}
		if nodeCompare > 0 {
			return false
		}

		peerCompare := bytes.Compare(first.peerid, second.peerid)
		if peerCompare < 0 {
			return true
		}
		if peerCompare > 0 {
			return false
		}

		if first.amountMsat < second.amountMsat {
			return true
		}
		if first.amountMsat > second.amountMsat {
			return false
		}

		if first.resolvedTime < second.resolvedTime {
			return true
		}
		if first.resolvedTime > second.resolvedTime {
			return false
		}
		return false
	})

	return importedForwards, nil
}

func readForwards(fileName string, startNs, endNs uint64) ([]*importedForward, error) {
	f, err := os.Open(fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", fileName, err)
	}

	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", fileName, err)
	}

	var exported []*postgresql.ExportedForward
	err = json.Unmarshal(b, &exported)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s: %w", fileName, err)
	}

	var result []*importedForward
	for _, imp := range exported {
		if imp.Token == "" {
			continue
		}

		// Filter out resolved on times outside our range too (since this is external data).
		resolvedTime := uint64(imp.ResolvedTime.UnixNano())
		if resolvedTime < startNs || resolvedTime >= endNs {
			continue
		}

		result = append(result, &importedForward{
			nodeid:       imp.ExternalNodeId,
			peerid:       imp.NodeId,
			amountMsat:   imp.AmountMsat,
			isCorrelated: false,
			token:        imp.Token,
			resolvedTime: resolvedTime,
			direction:    imp.Direction,
		})
	}

	return result, nil
}

// Matches imported forwards to local forwards, in order to isolate the token used. The token is then set on the
// corresponding local forward. This function sets the token used by the sender.
func matchImportedForwardsSend(forwardsSortedIn []*postgresql.RevenueForward, importedForwards []*importedForward) {
	forwardIndex := 0
	importedIndex := 0
	for {
		if forwardIndex >= len(forwardsSortedIn) {
			break
		}

		if importedIndex >= len(importedForwards) {
			break
		}

		importedForward := importedForwards[importedIndex]
		if importedForward.direction != "send" {
			importedIndex++
			continue
		}
		forward := forwardsSortedIn[forwardIndex]
		behind := compare(forward, importedForward)
		if behind == FirstBehind {
			forwardIndex++
			continue
		}
		if behind == SecondBehind {
			importedIndex++
			continue
		}

		// same node, same peer, same amount
		// if the forward is already correlated, go to the next
		if forward.SendToken != nil {
			forwardIndex++
			continue
		}
		if importedForward.isCorrelated {
			importedIndex++
			continue
		}

		// TODO: It would be better to find the best overlap in time range for these forwards
		importedForward.isCorrelated = true
		forward.SendToken = &importedForward.token
		forwardIndex++
		importedIndex++
	}
}

// Matches imported forwards to local forwards, in order to isolate the token used. The token is then set on the
// corresponding local forward. This function sets the token used by the recipient.
func matchImportedForwardsReceive(forwardsSortedOut []*postgresql.RevenueForward, importedForwards []*importedForward) {
	forwardIndex := 0
	importedIndex := 0
	for {
		if forwardIndex >= len(forwardsSortedOut) {
			break
		}

		if importedIndex >= len(importedForwards) {
			break
		}

		importedForward := importedForwards[importedIndex]
		if importedForward.direction != "receive" {
			importedIndex++
			continue
		}
		forward := forwardsSortedOut[forwardIndex]
		behind := compare(forward, importedForward)
		if behind == FirstBehind {
			forwardIndex++
			continue
		}
		if behind == SecondBehind {
			importedIndex++
			continue
		}

		// same node, same peer, same amount
		// if the forward is already correlated, go to the next
		if forward.ReceiveToken != nil {
			forwardIndex++
			continue
		}
		if importedForward.isCorrelated {
			importedIndex++
			continue
		}

		// TODO: It would be better to find the best overlap in time range for these forwards
		importedForward.isCorrelated = true
		forward.ReceiveToken = &importedForward.token
		forwardIndex++
		importedIndex++
	}
}

func compare(forward *postgresql.RevenueForward, importedForward *importedForward) int {
	nodeCompare := bytes.Compare(importedForward.nodeid, forward.Nodeid)
	if nodeCompare > 0 {
		return FirstBehind
	}
	if nodeCompare < 0 {
		return SecondBehind
	}

	peerCompare := bytes.Compare(importedForward.peerid, forward.PeeridIn)
	if peerCompare > 0 {
		return FirstBehind
	}
	if peerCompare < 0 {
		return SecondBehind
	}

	if importedForward.amountMsat > forward.AmtMsatIn {
		return FirstBehind
	}
	if importedForward.amountMsat < forward.AmtMsatIn {
		return SecondBehind
	}

	return 0
}

// Matches forwards from internal nodes in order to isolate the token used for sending/receiving.
// This function will match forwards for a single outgoing forward to a single incoming forward from
// the other node.
func matchInternalForwards(forwardsSortedIn, forwardsSortedOut []*postgresql.RevenueForward) {
	outIndex := 0
	inIndex := 0

	for {
		if outIndex >= len(forwardsSortedOut) {
			break
		}

		if inIndex >= len(forwardsSortedIn) {
			break
		}

		inForward := forwardsSortedIn[inIndex]
		outForward := forwardsSortedOut[outIndex]
		behind := compare(outForward, &importedForward{
			nodeid:       inForward.PeeridIn,
			peerid:       inForward.Nodeid,
			amountMsat:   inForward.AmtMsatIn,
			resolvedTime: inForward.ResolvedTime,
		})
		if behind == FirstBehind {
			outIndex++
			continue
		}
		if behind == SecondBehind {
			inIndex++
			continue
		}

		// same node, same peer, same amount
		// if the forward is already correlated, go to the next
		if outForward.ReceiveToken != nil {
			outIndex++
			continue
		}
		if inForward.SendToken != nil {
			inIndex++
			continue
		}

		// TODO: It would be better to find the best overlap in time range for these forwards
		inForward.SendToken = outForward.SendToken
		outForward.ReceiveToken = inForward.SendToken
		inIndex++
		outIndex++
	}
}

func matchOpenChannelHtlcs(forwardsSortedOut []*postgresql.RevenueForward, openChannelHtlcs []*postgresql.OpenChannelHtlc) {
	forwards := append([]*postgresql.RevenueForward(nil), forwardsSortedOut...)
	sort.SliceStable(forwards, func(i, j int) bool {
		first := forwards[i]
		second := forwards[j]
		nodeCompare := bytes.Compare(first.Nodeid, second.Nodeid)
		if nodeCompare < 0 {
			return true
		}
		if nodeCompare > 0 {
			return false
		}

		peerCompare := bytes.Compare(first.PeeridOut, second.PeeridOut)
		if peerCompare < 0 {
			return true
		}
		if peerCompare > 0 {
			return false
		}

		if first.AmtMsatOut < second.AmtMsatOut {
			return true
		}
		if first.AmtMsatOut > second.AmtMsatOut {
			return false
		}

		cphCompare := bytes.Compare(first.ChannelPointOut.Hash[:], second.ChannelPointOut.Hash[:])
		if cphCompare < 0 {
			return true
		}
		if cphCompare > 0 {
			return false
		}

		if first.ChannelPointOut.Index < second.ChannelPointOut.Index {
			return true
		}
		if first.ChannelPointOut.Index > second.ChannelPointOut.Index {
			return false
		}

		if first.AmtMsatIn < second.AmtMsatIn {
			return true
		}
		if first.AmtMsatIn > second.AmtMsatIn {
			return false
		}

		if first.ResolvedTime < second.ResolvedTime {
			return true
		}
		if first.ResolvedTime > second.ResolvedTime {
			return false
		}

		return false
	})

	htlcIndex := 0
	forwardIndex := 0
	for {
		if htlcIndex >= len(openChannelHtlcs) {
			break
		}

		if forwardIndex >= len(forwardsSortedOut) {
			break
		}

		htlc := openChannelHtlcs[htlcIndex]
		forward := forwardsSortedOut[forwardIndex]
		behind := compare(forward, &importedForward{
			nodeid:     htlc.Nodeid,
			peerid:     htlc.Peerid,
			amountMsat: htlc.ForwardAmountMsat,
		})
		if behind == FirstBehind {
			forwardIndex++
			continue
		}
		if behind == SecondBehind {
			htlcIndex++
			continue
		}

		cphCompare := bytes.Compare(forward.ChannelPointOut.Hash[:], htlc.ChannelPoint.Hash[:])
		if cphCompare < 0 {
			forwardIndex++
			continue
		}
		if cphCompare > 0 {
			htlcIndex++
			continue
		}

		if forward.ChannelPointOut.Index < htlc.ChannelPoint.Index {
			forwardIndex++
			continue
		}
		if forward.ChannelPointOut.Index > htlc.ChannelPoint.Index {
			htlcIndex++
			continue
		}

		if forward.AmtMsatIn < htlc.IncomingAmountMsat {
			forwardIndex++
			continue
		}
		if forward.AmtMsatIn > htlc.IncomingAmountMsat {
			htlcIndex++
			continue
		}

		if forward.OpenChannelHtlc != nil {
			forwardIndex++
			continue
		}

		forward.OpenChannelHtlc = htlc
		htlcIndex++
		forwardIndex++
	}
}

func calculateRevenue(forwards []*postgresql.RevenueForward) *RevenueResponse {
	result := &RevenueResponse{
		Nodes: make([]*NodeRevenue, 0),
	}

	var currentNode *NodeRevenue = nil
	for _, forward := range forwards {
		if currentNode == nil || !bytes.Equal(currentNode.Nodeid, forward.Nodeid) {
			currentNode = &NodeRevenue{
				Nodeid: forward.Nodeid,
				Tokens: make(map[string]*TokenRevenue, 0),
			}
			result.Nodes = append(result.Nodes, currentNode)
		}

		currentNode.TotalFeesMsat += forward.AmtMsatIn - forward.AmtMsatOut
		currentNode.TotalForwardCountMsat++
		if forward.SendToken != nil {
			sendToken, ok := currentNode.Tokens[*forward.SendToken]
			if !ok {
				sendToken = &TokenRevenue{
					Token: *forward.SendToken,
				}
				currentNode.Tokens[*forward.SendToken] = sendToken
			}

			feesMsat := (forward.AmtMsatIn - forward.AmtMsatOut) / 2
			sendToken.TotalFeesMsatSend += feesMsat
			sendToken.TotalForwardsSend++
			currentNode.TotalTokenFeesMsat = feesMsat
		}
		if forward.ReceiveToken != nil {
			receiveToken, ok := currentNode.Tokens[*forward.ReceiveToken]
			if !ok {
				receiveToken = &TokenRevenue{
					Token: *forward.ReceiveToken,
				}
				currentNode.Tokens[*forward.ReceiveToken] = receiveToken
			}

			feesMsat := (forward.AmtMsatIn - forward.AmtMsatOut) / 2
			var openFeesMsat uint64
			if forward.OpenChannelHtlc != nil {
				openFeesMsat = forward.OpenChannelHtlc.OriginalAmountMsat - forward.OpenChannelHtlc.ForwardAmountMsat
			}

			receiveToken.TotalChannelOpenFees += openFeesMsat
			receiveToken.TotalFeesMsatReceive += feesMsat
			receiveToken.TotalForwardsReceive++
			currentNode.TotalTokenFeesMsat = feesMsat
			currentNode.TotalChannelFeesMsat += openFeesMsat
		}
	}

	return result
}

type RevenueResponse struct {
	Nodes []*NodeRevenue
}
type NodeRevenue struct {
	Nodeid []byte
	Tokens map[string]*TokenRevenue
	// amt_msat_in - amt_msat_out for every forward by this node
	TotalFeesMsat uint64

	// counting all forwards
	TotalForwardCountMsat uint64

	TotalTokenFeesMsat uint64

	TotalChannelFeesMsat uint64
}

type TokenRevenue struct {
	Token string

	// Total forwards on this node where the token was associated for the send side.
	TotalForwardsSend uint64

	// Total forwards on this node where the token was associated for the receive side.
	TotalForwardsReceive uint64

	// Total fees on this node associated with the token for the send side.
	TotalFeesMsatSend uint64

	// Total fees on this node associated with the token for the receive side.
	TotalFeesMsatReceive uint64

	// Total fees associated to channel opens.
	TotalChannelOpenFees uint64
}

type importedForward struct {
	nodeid       []byte
	peerid       []byte
	amountMsat   uint64
	isCorrelated bool
	token        string
	resolvedTime uint64
	direction    string
}
