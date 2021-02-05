package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc/metadata"
)

type copyFromEvents struct {
	events []*lnrpc.ForwardingEvent
	idx    int
	err    error
}

func (cfe *copyFromEvents) Next() bool {
	cfe.idx++
	return cfe.idx < len(cfe.events)
}

func (cfe *copyFromEvents) Values() ([]interface{}, error) {
	event := cfe.events[cfe.idx]
	values := []interface{}{
		event.TimestampNs,
		event.ChanIdIn, event.ChanIdOut,
		event.AmtInMsat, event.AmtOutMsat}
	return values, nil
}

func (cfe *copyFromEvents) Err() error {
	return cfe.err
}

func forwardingHistorySynchronize() {
	for {
		err := forwardingHistorySynchronizeOnce()
		log.Printf("forwardingHistorySynchronizeOnce() err: %v", err)
		time.Sleep(1 * time.Minute)
	}
}

func forwardingHistorySynchronizeOnce() error {
	last, err := lastForwardingEvent()
	if err != nil {
		return fmt.Errorf("lastForwardingEvent() error: %w", err)
	}
	log.Printf("last1: %v", last)
	last = last/1_000_000_000 - 1*3600
	if last <= 0 {
		last = 1
	}
	log.Printf("last2: %v", last)
	now := time.Now()
	endTime := uint64(now.Add(time.Hour * 24).Unix())
	clientCtx := metadata.AppendToOutgoingContext(context.Background(), "macaroon", os.Getenv("LND_MACAROON_HEX"))
	indexOffset := uint32(0)
	for {
		forwardHistory, err := client.ForwardingHistory(clientCtx, &lnrpc.ForwardingHistoryRequest{
			StartTime:    uint64(last),
			EndTime:      endTime,
			NumMaxEvents: 10000,
			IndexOffset:  indexOffset,
		})
		if err != nil {
			log.Printf("ForwardingHistory error: %v", err)
			return fmt.Errorf("client.ForwardingHistory() error: %w", err)
		}
		log.Printf("Offset: %v, Events: %v", indexOffset, len(forwardHistory.ForwardingEvents))
		if len(forwardHistory.ForwardingEvents) == 0 {
			break
		}
		indexOffset = forwardHistory.LastOffsetIndex
		cfe := copyFromEvents{events: forwardHistory.ForwardingEvents, idx: -1}
		err = insertForwardingEvents(&cfe)
		if err != nil {
			log.Printf("insertForwardingEvents() error: %v", err)
			return fmt.Errorf("insertForwardingEvents() error: %w", err)
		}
	}
	return nil
}
