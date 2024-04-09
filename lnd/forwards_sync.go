package lnd

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/breez/lspd/history"
	"github.com/breez/lspd/lightning"
	"github.com/lightningnetwork/lnd/lnrpc"
)

type ForwardSync struct {
	nodeid []byte
	client *LndClient
	store  history.Store
}

func NewForwardSync(
	nodeid []byte,
	client *LndClient,
	store history.Store,
) *ForwardSync {
	return &ForwardSync{
		nodeid: nodeid,
		client: client,
		store:  store,
	}
}

var forwardChannelSyncInterval time.Duration = time.Minute * 5

const maxEvents = 10_000

func (s *ForwardSync) ForwardsSynchronize(ctx context.Context) {
	s.forwardsSynchronizeOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(forwardChannelSyncInterval):
		}

		s.forwardsSynchronizeOnce(ctx)
	}
}

func (s *ForwardSync) forwardsSynchronizeOnce(ctx context.Context) {
	last, err := s.store.FetchLndForwardOffset(ctx, s.nodeid)
	if err != nil {
		log.Printf("forwardsSynchronizeOnce(%x) - FetchLndForwardOffset err: %v", s.nodeid, err)
		return
	}

	var startTime uint64
	if last != nil {
		startTime = uint64(last.UnixNano())
	}

	for {
		forwardHistory, err := s.client.client.ForwardingHistory(context.Background(), &lnrpc.ForwardingHistoryRequest{
			StartTime:    startTime / 1_000_000_000,
			NumMaxEvents: maxEvents,
		})
		if err != nil {
			log.Printf("forwardsSynchronizeOnce(%x) - ForwardingHistory error: %v", s.nodeid, err)
			return
		}

		log.Printf("forwardsSynchronizeOnce(%x) - startTime: %v, Events: %v", s.nodeid, startTime, len(forwardHistory.ForwardingEvents))
		if len(forwardHistory.ForwardingEvents) == 0 {
			break
		}

		forwards := make([]*history.Forward, len(forwardHistory.ForwardingEvents))
		for i, f := range forwardHistory.ForwardingEvents {
			forwards[i] = &history.Forward{
				Identifier:   strconv.FormatUint(f.TimestampNs, 10) + "|" + strconv.FormatUint(f.AmtInMsat, 10),
				InChannel:    lightning.ShortChannelID(f.ChanIdIn),
				OutChannel:   lightning.ShortChannelID(f.ChanIdOut),
				InMsat:       f.AmtInMsat,
				OutMsat:      f.AmtOutMsat,
				ResolvedTime: time.Unix(0, int64(f.TimestampNs)),
			}
			startTime = f.TimestampNs
		}
		err = s.store.InsertForwards(ctx, forwards, s.nodeid)
		if err != nil {
			log.Printf("forwardsSynchronizeOnce(%x) - store.InsertForwards() error: %v", s.nodeid, err)
			return
		}

		if len(forwardHistory.ForwardingEvents) < maxEvents {
			break
		}
	}
}
