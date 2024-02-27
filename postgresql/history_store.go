package postgresql

import (
	"context"
	"fmt"
	"log"
	"time"

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

type copyFromForwards struct {
	forwards []*history.Forward
	nodeid   []byte
	idx      int
	err      error
}

func (cfe *copyFromForwards) Next() bool {
	cfe.idx++
	return cfe.idx < len(cfe.forwards)
}

func (cfe *copyFromForwards) Values() ([]interface{}, error) {
	forward := cfe.forwards[cfe.idx]
	values := []interface{}{
		forward.Identifier,
		forward.ResolvedTime.UnixNano(),
		cfe.nodeid,
		int64(uint64(forward.InChannel)),
		int64(uint64(forward.OutChannel)),
		int64(forward.InMsat),
		int64(forward.OutMsat),
	}
	return values, nil
}

func (cfe *copyFromForwards) Err() error {
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

func (s *HistoryStore) InsertForwards(
	ctx context.Context,
	forwards []*history.Forward,
	nodeid []byte,
) error {
	if len(forwards) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgxPool.Begin() error: %w", err)
	}
	defer tx.Rollback(ctx)

	rowSrc := copyFromForwards{
		forwards: forwards,
		nodeid:   nodeid,
		idx:      -1,
	}

	_, err = tx.Exec(ctx, `
	CREATE TEMP TABLE tmp_table ON COMMIT DROP AS
		SELECT *
		FROM forwarding_history
		WITH NO DATA;
	`)
	if err != nil {
		return fmt.Errorf("CREATE TEMP TABLE error: %w", err)
	}

	count, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"tmp_table"},
		[]string{"identifier", "resolved_time", "nodeid", "chanid_in", "chanid_out", "amt_msat_in", "amt_msat_out"},
		&rowSrc,
	)
	if err != nil {
		return fmt.Errorf("CopyFrom() error: %w", err)
	}
	log.Printf("InsertForwards node %x count1: %v", nodeid, count)

	cmdTag, err := tx.Exec(ctx, `
	INSERT INTO forwarding_history
		SELECT *
		FROM tmp_table
	ON CONFLICT (nodeid, identifier) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("INSERT INTO forwarding_history error: %w", err)
	}
	log.Printf("InsertForwards node %x count2: %v", nodeid, cmdTag.RowsAffected())

	return tx.Commit(ctx)
}

func (s *HistoryStore) UpdateForwards(
	ctx context.Context,
	forwards []*history.Forward,
	nodeid []byte,
) error {
	if len(forwards) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgxPool.Begin() error: %w", err)
	}
	defer tx.Rollback(ctx)

	rowSrc := copyFromForwards{
		forwards: forwards,
		nodeid:   nodeid,
		idx:      -1,
	}

	_, err = tx.Exec(ctx, `
	CREATE TEMP TABLE tmp_table ON COMMIT DROP AS
		SELECT *
		FROM forwarding_history
		WITH NO DATA;
	`)
	if err != nil {
		return fmt.Errorf("CREATE TEMP TABLE error: %w", err)
	}

	count, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"tmp_table"},
		[]string{"identifier", "resolved_time", "nodeid", "chanid_in", "chanid_out", "amt_msat_in", "amt_msat_out"},
		&rowSrc,
	)
	if err != nil {
		return fmt.Errorf("CopyFrom() error: %w", err)
	}
	log.Printf("UpdateForwards node %x count1: %v", nodeid, count)

	cmdTag, err := tx.Exec(ctx, `
	INSERT INTO forwarding_history
		SELECT *
		FROM tmp_table
	ON CONFLICT (nodeid, identifier) DO UPDATE SET
		resolved_time = EXCLUDED.resolved_time,
		chanid_in = EXCLUDED.chanid_in,
		chanid_out = EXCLUDED.chanid_out,
		amt_msat_in = EXCLUDED.amt_msat_in,
		amt_msat_out = EXCLUDED.amt_msat_out
	`)
	if err != nil {
		return fmt.Errorf("INSERT INTO forwarding_history error: %w", err)
	}
	log.Printf("UpdateForwards node %x count2: %v", nodeid, cmdTag.RowsAffected())

	return tx.Commit(ctx)
}

func (s *HistoryStore) FetchClnForwardOffsets(
	ctx context.Context,
	nodeId []byte,
) (uint64, uint64, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT last_created_index, last_updated_index
		FROM public.cln_forwarding_history_offsets
		WHERE nodeid = $1
		`,
		nodeId)

	var created int64
	var updated int64
	err := row.Scan(&created, &updated)
	if err == pgx.ErrNoRows {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}

	return uint64(created), uint64(updated), nil
}

func (s *HistoryStore) FetchLndForwardOffset(
	ctx context.Context,
	nodeId []byte,
) (*time.Time, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT MAX(resolved_time)
		FROM forwarding_history
		WHERE nodeid = $1
		`,
		nodeId)
	var t *int64
	err := row.Scan(&t)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}

	tt := time.Unix(0, *t)
	return &tt, nil
}

func (s *HistoryStore) SetClnForwardOffsets(
	ctx context.Context,
	nodeId []byte,
	created uint64,
	updated uint64,
) error {
	_, err := s.pool.Exec(ctx, `
	INSERT INTO public.cln_forwarding_history_offsets (nodeid, last_created_index, last_updated_index)
	VALUES($1, $2, $3)
	ON CONFLICT (nodeid) DO UPDATE SET last_created_index = EXCLUDED.last_created_index, last_updated_index = EXCLUDED.last_updated_index
	`,
		nodeId,
		int64(created),
		int64(updated),
	)
	return err
}

func (s *HistoryStore) AddOpenChannelHtlc(ctx context.Context, htlc *history.OpenChannelHtlc) error {
	// TODO: Find an identifier equal to the forwarding_history identifier.
	_, err := s.pool.Exec(ctx, `
	INSERT INTO open_channel_htlcs (
		nodeid
	,	peerid
	,	funding_tx_id
	,	funding_tx_outnum
	,	forward_amt_msat
	,	original_amt_msat
	,   incoming_amt_msat
	,   forward_time
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		htlc.NodeId,
		htlc.PeerId,
		htlc.ChannelPoint.Hash[:],
		htlc.ChannelPoint.Index,
		int64(htlc.ForwardAmountMsat),
		int64(htlc.OriginalAmountMsat),
		int64(htlc.IncomingAmountMsat),
		htlc.ForwardTime.UnixNano(),
	)
	return err
}
