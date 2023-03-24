package postgresql

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/breez/lspd/basetypes"
	"github.com/breez/lspd/lsps2"
	"github.com/jackc/pgx/v4/pgxpool"
)

type ScidStore struct {
	pool *pgxpool.Pool
}

func NewScidStore(pool *pgxpool.Pool) *ScidStore {
	return &ScidStore{
		pool: pool,
	}
}

func (s *ScidStore) AddScid(
	ctx context.Context,
	lspId []byte,
	scid basetypes.ShortChannelID,
	expiry time.Time,
) error {
	_, err := s.pool.Exec(
		ctx,
		`INSERT INTO public.scid_reservations (lspid, scid, expiry)
		VALUES (?, ?, ?)`,
		lspId,
		int64(uint64(scid)), // store the scid as int64
		expiry.Unix(),
	)

	if err != nil && strings.Contains(err.Error(), "already exists") {
		return lsps2.ErrScidExists
	}

	return err
}

func (s *ScidStore) RemoveScid(
	ctx context.Context,
	lspId []byte,
	scid basetypes.ShortChannelID,
) (bool, error) {
	res, err := s.pool.Exec(
		ctx,
		`DELETE FROM public.scid_reservations
		WHERE lspid = ? AND scid = ?`,
		lspId,
		int64(uint64(scid)), // convert scid to int64
	)

	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, nil
}

func (s *ScidStore) RemoveExpired(ctx context.Context, before time.Time) error {
	rows, err := s.pool.Exec(
		ctx,
		`DELETE FROM public.scid_reservations
		 WHERE expiry < ?`,
		before.Unix(),
	)

	if err != nil {
		return err
	}

	rowsAffected := rows.RowsAffected()
	if rowsAffected > 0 {
		log.Printf(
			"Deleted %d scids from scid_reservations that expired before %s",
			rowsAffected,
			before.String(),
		)
	}

	return nil
}
