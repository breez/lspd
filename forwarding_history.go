package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/lightningnetwork/lnd/htlcswitch/hop"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
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
		int64(event.ChanIdIn), int64(event.ChanIdOut),
		event.AmtInMsat, event.AmtOutMsat}
	return values, nil
}

func (cfe *copyFromEvents) Err() error {
	return cfe.err
}

func channelsSynchronize(client *LndClient) {
	lastSync := time.Now().Add(-6 * time.Minute)
	for {
		cancellableCtx, cancel := context.WithCancel(context.Background())
		stream, err := client.chainNotifierClient.RegisterBlockEpochNtfn(cancellableCtx, &chainrpc.BlockEpoch{})
		if err != nil {
			log.Printf("chainNotifierClient.RegisterBlockEpochNtfn(): %v", err)
			cancel()
			time.Sleep(1 * time.Second)
			continue
		}

		for {
			_, err := stream.Recv()
			if err != nil {
				log.Printf("stream.Recv: %v", err)
				break
			}
			if lastSync.Add(5 * time.Minute).Before(time.Now()) {
				time.Sleep(30 * time.Second)
				err = channelsSynchronizeOnce(client)
				lastSync = time.Now()
				log.Printf("channelsSynchronizeOnce() err: %v", err)
			}
		}
		cancel()
	}
}

func channelsSynchronizeOnce(client *LndClient) error {
	log.Printf("channelsSynchronizeOnce - begin")
	channels, err := client.client.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{PrivateOnly: true})
	if err != nil {
		log.Printf("ListChannels error: %v", err)
		return fmt.Errorf("client.ListChannels() error: %w", err)
	}
	log.Printf("channelsSynchronizeOnce - received channels")
	lastUpdate := time.Now()
	for _, c := range channels.Channels {
		nodeID, err := hex.DecodeString(c.RemotePubkey)
		if err != nil {
			log.Printf("hex.DecodeString in channelsSynchronizeOnce error: %v", err)
			continue
		}
		confirmedChanId := c.ChanId
		if c.ZeroConf {
			confirmedChanId = c.ZeroConfConfirmedScid
			if confirmedChanId == hop.Source.ToUint64() {
				confirmedChanId = 0
			}
		}
		err = insertChannel(c.ChanId, confirmedChanId, c.ChannelPoint, nodeID, lastUpdate)
		if err != nil {
			log.Printf("insertChannel(%v, %v, %x) in channelsSynchronizeOnce error: %v", c.ChanId, c.ChannelPoint, nodeID, err)
			continue
		}
	}
	log.Printf("channelsSynchronizeOnce - done")

	return nil
}

func forwardingHistorySynchronize(client *LndClient) {
	for {
		err := forwardingHistorySynchronizeOnce(client)
		log.Printf("forwardingHistorySynchronizeOnce() err: %v", err)
		time.Sleep(1 * time.Minute)
	}
}

func forwardingHistorySynchronizeOnce(client *LndClient) error {
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
	indexOffset := uint32(0)
	for {
		forwardHistory, err := client.client.ForwardingHistory(context.Background(), &lnrpc.ForwardingHistoryRequest{
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
