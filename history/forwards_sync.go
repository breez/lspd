package history

import (
	"context"
	"log"
	"time"
)

type ForwardChannelSync struct {
	store Store
}

func NewForwardChannelSync(store Store) *ForwardChannelSync {
	return &ForwardChannelSync{
		store: store,
	}
}

var forwardChannelSyncInterval time.Duration = time.Minute

func (s *ForwardChannelSync) ForwardsSynchronize(ctx context.Context) {
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

func (s *ForwardChannelSync) forwardsSynchronizeOnce(ctx context.Context) {
	err := s.store.MatchForwardsAndChannels(ctx)
	if err != nil {
		log.Printf("forwardsSynchronizeNodeOnce() - store.MatchForwardsAndChannels error: %v", err)
		return
	}
}
