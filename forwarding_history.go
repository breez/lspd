package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/breez/lspd/interceptor"
	"github.com/breez/lspd/lnd"
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

type ForwardingHistorySync struct {
	client          *LndClient
	interceptStore  interceptor.InterceptStore
	forwardingStore lnd.ForwardingEventStore
}

func NewForwardingHistorySync(
	client *LndClient,
	interceptStore interceptor.InterceptStore,
	forwardingStore lnd.ForwardingEventStore,
) *ForwardingHistorySync {
	return &ForwardingHistorySync{
		client:          client,
		interceptStore:  interceptStore,
		forwardingStore: forwardingStore,
	}
}

func (s *ForwardingHistorySync) ChannelsSynchronize(ctx context.Context) {
	lastSync := time.Now().Add(-6 * time.Minute)
	for {
		if ctx.Err() != nil {
			return
		}

		stream, err := s.client.chainNotifierClient.RegisterBlockEpochNtfn(ctx, &chainrpc.BlockEpoch{})
		if err != nil {
			log.Printf("chainNotifierClient.RegisterBlockEpochNtfn(): %v", err)
			<-time.After(time.Second)
			continue
		}

		for {
			if ctx.Err() != nil {
				return
			}

			_, err := stream.Recv()
			if err != nil {
				log.Printf("stream.Recv: %v", err)
				<-time.After(time.Second)
				break
			}
			if lastSync.Add(5 * time.Minute).Before(time.Now()) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(1 * time.Minute):
				}
				err = s.ChannelsSynchronizeOnce()
				lastSync = time.Now()
				log.Printf("channelsSynchronizeOnce() err: %v", err)
			}
		}
	}
}

func (s *ForwardingHistorySync) ChannelsSynchronizeOnce() error {
	log.Printf("channelsSynchronizeOnce - begin")
	channels, err := s.client.client.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{PrivateOnly: true})
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
		err = s.interceptStore.InsertChannel(c.ChanId, confirmedChanId, c.ChannelPoint, nodeID, lastUpdate)
		if err != nil {
			log.Printf("insertChannel(%v, %v, %x) in channelsSynchronizeOnce error: %v", c.ChanId, c.ChannelPoint, nodeID, err)
			continue
		}
	}
	log.Printf("channelsSynchronizeOnce - done")

	return nil
}

func (s *ForwardingHistorySync) ForwardingHistorySynchronize(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		err := s.ForwardingHistorySynchronizeOnce()
		log.Printf("forwardingHistorySynchronizeOnce() err: %v", err)
		select {
		case <-time.After(1 * time.Minute):
		case <-ctx.Done():
		}
	}
}

func (s *ForwardingHistorySync) ForwardingHistorySynchronizeOnce() error {
	last, err := s.forwardingStore.LastForwardingEvent()
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
		forwardHistory, err := s.client.client.ForwardingHistory(context.Background(), &lnrpc.ForwardingHistoryRequest{
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
		err = s.forwardingStore.InsertForwardingEvents(&cfe)
		if err != nil {
			log.Printf("insertForwardingEvents() error: %v", err)
			return fmt.Errorf("insertForwardingEvents() error: %w", err)
		}
	}
	return nil
}
