package history

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/breez/lspd/common"
)

type ChannelSync struct {
	nodes []*common.Node
	store Store
}

func NewChannelSync(nodes []*common.Node, store Store) *ChannelSync {
	return &ChannelSync{
		nodes: nodes,
		store: store,
	}
}

var channelSyncInterval time.Duration = time.Minute * 5

func (s *ChannelSync) ChannelsSynchronize(ctx context.Context) {
	s.channelsSynchronizeOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(channelSyncInterval):
		}

		s.channelsSynchronizeOnce(ctx)
	}
}

func (s *ChannelSync) channelsSynchronizeOnce(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(len(s.nodes))
	for _, n := range s.nodes {
		go func(node *common.Node) {
			s.channelsSynchronizeNodeOnce(ctx, node)
			wg.Done()
		}(n)
	}
	wg.Wait()
}

func (s *ChannelSync) channelsSynchronizeNodeOnce(ctx context.Context, node *common.Node) {
	lastUpdate := time.Now()
	log.Printf("ChannelsSynchronizeNodeOnce(%x) - Begin %v", node.NodeId, lastUpdate)
	channels, err := node.Client.ListChannels()
	if err != nil {
		log.Printf("ChannelsSynchronizeNodeOnce(%x)- ListChannels error: %v", node.NodeId, err)
		return
	}

	updates := make([]*ChannelUpdate, len(channels))
	for i, c := range channels {
		if c == nil {
			continue
		}
		updates[i] = &ChannelUpdate{
			NodeID:        node.NodeId,
			PeerId:        c.PeerId,
			AliasScid:     c.AliasScid,
			ConfirmedScid: c.ConfirmedScid,
			ChannelPoint:  c.ChannelPoint,
			LastUpdate:    lastUpdate,
		}
	}
	err = s.store.UpdateChannels(ctx, updates)
	if err != nil {
		log.Printf("ChannelsSynchronizeNodeOnce(%x) - store.UpdateChannels error: %v", node.NodeId, err)
		return
	}

	log.Printf("ChannelsSynchronizeNodeOnce(%x) - Done %v", node.NodeId, lastUpdate)
}
