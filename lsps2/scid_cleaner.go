package lsps2

import (
	"context"
	"log"
	"time"
)

type ScidCleaner struct {
	interval time.Duration
	store    ScidStore
}

func NewScidCleaner(
	store ScidStore,
	interval time.Duration,
) *ScidCleaner {
	return &ScidCleaner{
		interval: interval,
		store:    store,
	}
}

func (s *ScidCleaner) Start(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-ticker.C:
			err := s.store.RemoveExpired(ctx, t)
			if err != nil {
				log.Printf("Failed to remove expired scids: %v", err)
			}
		}
	}
}
