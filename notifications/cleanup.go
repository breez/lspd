package notifications

import (
	"context"
	"log"
	"time"
)

type CleanupService struct {
	store Store
}

// The interval to clean unused promises and buy registrations.
var CleanupInterval time.Duration = time.Hour

// The expiry duration is the time until a non-refreshed webhook url expires.
// Currently set to 4 weeks.
var ExpiryDuration time.Duration = time.Hour * 24 * 28

func NewCleanupService(store Store) *CleanupService {
	return &CleanupService{
		store: store,
	}
}

// Periodically cleans up expired webhook urls.
func (c *CleanupService) Start(ctx context.Context) {
	for {
		before := time.Now().Add(-ExpiryDuration)
		err := c.store.RemoveExpired(ctx, before)
		if err != nil {
			log.Printf("Failed to remove expired webhook urls before %v: %v", before, err)
		}
		select {
		case <-time.After(CleanupInterval):
			continue
		case <-ctx.Done():
			return
		}
	}
}
