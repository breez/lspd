package postgresql

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

type NotificationsStore struct {
	pool *pgxpool.Pool
}

func NewNotificationsStore(pool *pgxpool.Pool) *NotificationsStore {
	return &NotificationsStore{pool: pool}
}

func (s *NotificationsStore) Register(
	ctx context.Context,
	pubkey string,
	url string,
) error {
	pk, err := hex.DecodeString(pubkey)
	if err != nil {
		return err
	}

	now := time.Now().UnixMicro()
	_, err = s.pool.Exec(
		ctx,
		`INSERT INTO public.notification_subscriptions (pubkey, url, created_at, refreshed_at)
		 values ($1, $2, $3, $4)
		 ON CONFLICT (pubkey, url) DO UPDATE SET refreshed_at = $4`,
		pk,
		url,
		now,
		now,
	)

	return err
}

func (s *NotificationsStore) GetRegistrations(
	ctx context.Context,
	pubkey string,
) ([]string, error) {
	pk, err := hex.DecodeString(pubkey)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(
		ctx,
		`SELECT url 
		 FROM public.notification_subscriptions
		 WHERE pubkey = $1`,
		pk,
	)
	if err != nil {
		return nil, err
	}

	var result []string
	for rows.Next() {
		var url string
		err = rows.Scan(&url)
		if err != nil {
			return nil, err
		}

		result = append(result, url)
	}

	return result, nil
}
