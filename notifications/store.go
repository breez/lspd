package notifications

import (
	"context"
)

type Store interface {
	Register(ctx context.Context, pubkey string, url string) error
	GetRegistrations(ctx context.Context, pubkey string) ([]string, error)
}
