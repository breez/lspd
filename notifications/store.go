package notifications

import (
	"context"
	"time"
)

type Store interface {
	Register(ctx context.Context, pubkey string, url string) error
	GetRegistrations(ctx context.Context, pubkey string) ([]string, error)
	Unsubscribe(ctx context.Context, pubkey string, url string) error
	RemoveExpired(ctx context.Context, before time.Time) error
}
