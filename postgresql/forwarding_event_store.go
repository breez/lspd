package postgresql

import (
	"context"
	"fmt"
	"log"

	"github.com/breez/lspd/lnd"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type ForwardingEventStore struct {
	pool *pgxpool.Pool
}

func NewForwardingEventStore(pool *pgxpool.Pool) *ForwardingEventStore {
	return &ForwardingEventStore{pool: pool}
}

func (s *ForwardingEventStore) LastForwardingEvent() (int64, error) {
	var last int64
	err := s.pool.QueryRow(context.Background(),
		`SELECT coalesce(MAX("timestamp"), 0) AS last FROM forwarding_history`).Scan(&last)
	if err != nil {
		return 0, err
	}
	return last, nil
}

func (s *ForwardingEventStore) InsertForwardingEvents(rowSrc lnd.CopyFromSource) error {

	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("pgxPool.Begin() error: %w", err)
	}
	defer tx.Rollback(context.Background())

	_, err = tx.Exec(context.Background(), `
	CREATE TEMP TABLE tmp_table ON COMMIT DROP AS
		SELECT *
		FROM forwarding_history
		WITH NO DATA;
	`)
	if err != nil {
		return fmt.Errorf("CREATE TEMP TABLE error: %w", err)
	}

	count, err := tx.CopyFrom(context.Background(),
		pgx.Identifier{"tmp_table"},
		[]string{"timestamp", "chanid_in", "chanid_out", "amt_msat_in", "amt_msat_out"}, rowSrc)
	if err != nil {
		return fmt.Errorf("CopyFrom() error: %w", err)
	}
	log.Printf("count1: %v", count)

	cmdTag, err := tx.Exec(context.Background(), `
	INSERT INTO forwarding_history
		SELECT *
		FROM tmp_table
	ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("INSERT INTO forwarding_history error: %w", err)
	}
	log.Printf("count2: %v", cmdTag.RowsAffected())
	return tx.Commit(context.Background())
}
