package postgresql

import (
	"context"
	"fmt"
	"log"

	"github.com/GoWebProd/uuid7"
	"github.com/breez/lspd/history"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type copyFromChanUpdates struct {
	channels []*history.ChannelUpdate
	idx      int
	err      error
}

func (cfe *copyFromChanUpdates) Next() bool {
	if len(cfe.channels) == 0 {
		return false
	}

	for {
		cfe.idx++
		if cfe.idx >= len(cfe.channels) {
			return false
		}

		if cfe.channels[cfe.idx] == nil {
			continue
		}

		return true
	}
}

func (cfe *copyFromChanUpdates) Values() ([]interface{}, error) {
	channel := cfe.channels[cfe.idx]
	var aliasScid *int64
	if channel.AliasScid != nil {
		tmp := uint64(*channel.AliasScid)
		tmp2 := int64(tmp)
		aliasScid = &tmp2
	}
	var confirmedScid *int64
	if channel.ConfirmedScid != nil {
		tmp := uint64(*channel.ConfirmedScid)
		tmp2 := int64(tmp)
		confirmedScid = &tmp2
	}
	values := []interface{}{
		channel.NodeID,
		channel.PeerId,
		aliasScid,
		confirmedScid,
		channel.ChannelPoint.Hash[:],
		channel.ChannelPoint.Index,
		channel.LastUpdate,
		channel.LastUpdate,
	}
	return values, nil
}

func (cfe *copyFromChanUpdates) Err() error {
	return cfe.err
}

type HistoryStore struct {
	pool      *pgxpool.Pool
	generator *uuid7.Generator
}

func NewHistoryStore(pool *pgxpool.Pool) *HistoryStore {
	return &HistoryStore{
		pool:      pool,
		generator: uuid7.New(),
	}
}

func (s *HistoryStore) UpdateChannels(
	ctx context.Context,
	updates []*history.ChannelUpdate,
) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgxPool.Begin() error: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
	CREATE TEMP TABLE tmp_table ON COMMIT DROP AS
	SELECT *
	FROM channels
	WITH NO DATA;
	`)
	if err != nil {
		return fmt.Errorf("CREATE TEMP TABLE error: %w", err)
	}

	rowSrc := &copyFromChanUpdates{channels: updates, idx: -1}
	count, err := tx.CopyFrom(ctx,
		pgx.Identifier{"tmp_table"},
		[]string{"nodeid", "peerid", "alias_scid", "confirmed_scid", "funding_tx_id", "funding_tx_outnum", "first_seen", "last_update"},
		rowSrc)
	if err != nil {
		return fmt.Errorf("CopyFrom() error: %w", err)
	}
	log.Printf("UpdateChannels - count1: %v", count)

	cmdTag, err := tx.Exec(ctx, `
	INSERT INTO channels
	SELECT *
	FROM tmp_table
	ON CONFLICT (nodeid, funding_tx_id, funding_tx_outnum) DO UPDATE SET
		alias_scid = EXCLUDED.alias_scid,
		confirmed_scid = EXCLUDED.confirmed_scid,
		last_update = EXCLUDED.last_update
	`)
	if err != nil {
		return fmt.Errorf("INSERT INTO channels error: %w", err)
	}
	log.Printf("UpdateChannels - count2: %v", cmdTag.RowsAffected())

	return tx.Commit(ctx)
}
