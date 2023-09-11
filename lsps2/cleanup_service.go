package lsps2

import (
	"context"
	"log"
	"time"
)

type CleanupService struct {
	store Lsps2Store
}

// The interval to clean unused promises and buy registrations.
var CleanupInterval time.Duration = time.Hour

// The relax period is a period where expired promises may still be valid, if
// the current chainfees are cheaper than the fees in the promise itself. It is
// set to ~two weeks.
var RelaxPeriod time.Duration = time.Hour * 24 * 14

func NewCleanupService(store Lsps2Store) *CleanupService {
	return &CleanupService{
		store: store,
	}
}

// Periodically cleans up unused buy registrations and promises that have
// expired before the relax interval.
func (c *CleanupService) Start(ctx context.Context) {
	for {
		before := time.Now().UTC().Add(-RelaxPeriod)
		err := c.store.RemoveUnusedExpired(ctx, before)
		if err != nil {
			log.Printf("Failed to remove unused expired registrations before %v: %v", before, err)
		}
		select {
		case <-time.After(CleanupInterval):
			continue
		case <-ctx.Done():
			return
		}
	}
}
