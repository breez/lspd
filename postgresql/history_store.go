package postgresql

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/GoWebProd/uuid7"
	"github.com/breez/lspd/history"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NOTE: This query doesn't match on node id, because it is not available.
// This does not produce duplicates, because token channels are always with a
// remote node. If lspd were hosting both nodes, this would produce duplicates
// on funding tx.
const tokenChannelsCte = `
WITH token_channels AS (
	SELECT p.tag::json->>'apiKeyHash' AS token
	,      c.nodeid
	,      c.peerid
	,      c.funding_tx_id
	,      c.funding_tx_outnum
	,      p.incoming_amount_msat - p.outgoing_amount_msat AS channel_fee_msat
	FROM public.payments p
	INNER JOIN public.channels c 
		ON p.funding_tx_id = c.funding_tx_id 
		AND p.funding_tx_outnum = c.funding_tx_outnum
	WHERE p.tag IS NOT NULL
	UNION ALL
	SELECT r.token
	,      c.nodeid
	,      c.peerid
	,      c.funding_tx_id
	,      c.funding_tx_outnum
	,      b.fee_msat AS channel_fee_msat
	FROM lsps2.bought_channels b
	INNER JOIN lsps2.buy_registrations r
		ON b.registration_id = r.id
	INNER JOIN public.channels c 
		ON b.funding_tx_id = c.funding_tx_id 
		AND b.funding_tx_outnum = c.funding_tx_outnum
)`

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

type copyFromTokenForwards struct {
	generator *uuid7.Generator
	forwards  []*history.ExternalTokenForward
	idx       int
	err       error
}

func (cfe *copyFromTokenForwards) Next() bool {
	cfe.idx++
	return cfe.idx < len(cfe.forwards)
}

func (cfe *copyFromTokenForwards) Values() ([]interface{}, error) {
	forward := cfe.forwards[cfe.idx]
	var id [16]byte = cfe.generator.Next()
	values := []interface{}{
		id,
		forward.NodeId,
		forward.ExternalNodeId,
		forward.Token,
		forward.ResolvedTime.UnixNano(),
		forward.Direction,
		int64(forward.AmountMsat),
	}
	return values, nil
}

func (cfe *copyFromTokenForwards) Err() error {
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

func (s *HistoryStore) MatchForwardsAndChannels(ctx context.Context) error {
	upd, err := s.pool.Exec(ctx, `
	UPDATE forwarding_history h
	SET funding_tx_id_in = c.funding_tx_id,
	    funding_tx_outnum_in = c.funding_tx_outnum
	FROM channels c
	WHERE h.funding_tx_id_in IS NULL
	    AND h.nodeid = c.nodeid 
	    AND (h.chanid_in = c.alias_scid OR h.chanid_in = c.confirmed_scid)
	`)
	if err != nil {
		return fmt.Errorf("failed to update incoming side of forwards: %w", err)
	}
	log.Printf("Matched %v incoming forwards with their corresponding peers", upd.RowsAffected())

	upd, err = s.pool.Exec(ctx, `
	UPDATE forwarding_history h
	SET funding_tx_id_out = c.funding_tx_id,
	    funding_tx_outnum_out = c.funding_tx_outnum
	FROM channels c
	WHERE h.funding_tx_id_out IS NULL
	    AND h.nodeid = c.nodeid 
	    AND (h.chanid_out = c.alias_scid OR h.chanid_out = c.confirmed_scid)
	`)
	if err != nil {
		return fmt.Errorf("failed to update incoming side of forwards: %w", err)
	}
	log.Printf("Matched %v outgoing forwards with their corresponding peers", upd.RowsAffected())
	return nil
}

func (s *HistoryStore) ExportTokenForwardsForExternalNode(
	ctx context.Context,
	start time.Time,
	end time.Time,
	node []byte,
	externalNode []byte,
) ([]*history.ExternalTokenForward, error) {
	startNs := start.UnixNano()
	endNs := end.UnixNano()
	rows, err := s.pool.Query(
		ctx, tokenChannelsCte+`
		SELECT * FROM (
			SELECT 'send' AS direction
			,      c_in.token
			,      h.resolved_time
			,      h.amt_msat_out AS amt_msat
			FROM public.forwarding_history h
			INNER JOIN public.channels c_out
				ON h.nodeid = c_out.nodeid AND h.funding_tx_id_out = c_out.funding_tx_id AND h.funding_tx_outnum_out = c_out.funding_tx_outnum
			INNER JOIN token_channels c_in
				ON h.nodeid = c_in.nodeid AND h.funding_tx_id_in = c_in.funding_tx_id AND h.funding_tx_outnum_in = c_in.funding_tx_outnum
			WHERE h.nodeid = $1 AND c_out.peerid = $2 AND h.resolved_time >= $3 AND h.resolved_time < $4
			UNION ALL
			SELECT 'receive' AS direction
			,      c_out.token
			,      h.resolved_time
			,      h.amt_msat_in AS amt_msat
			FROM public.forwarding_history h
			INNER JOIN token_channels c_out
			    ON h.nodeid = c_out.nodeid AND h.funding_tx_id_out = c_out.funding_tx_id AND h.funding_tx_outnum_out = c_out.funding_tx_outnum
			INNER JOIN public.channels c_in
			    ON h.nodeid = c_in.nodeid AND h.funding_tx_id_in = c_in.funding_tx_id AND h.funding_tx_outnum_in = c_in.funding_tx_outnum
			WHERE h.nodeid = $1 AND c_in.peerid = $2 AND h.resolved_time >= $3 AND h.resolved_time < $4
		)
		ORDER BY resolved_time DESC
		`,
		node,
		externalNode,
		startNs,
		endNs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*history.ExternalTokenForward, rows.CommandTag().RowsAffected())
	for rows.Next() {
		var direction string
		var token string
		var resolved_time int64
		var amt_msat int64
		err = rows.Scan(&direction, &token, &resolved_time, &amt_msat)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan err: %w", err)
		}

		result = append(result, &history.ExternalTokenForward{
			Token:          token,
			NodeId:         node,
			ExternalNodeId: externalNode,
			ResolvedTime:   time.Unix(0, resolved_time),
			Direction:      direction,
			AmountMsat:     uint64(amt_msat),
		})
	}

	return result, nil
}

func (s *HistoryStore) ImportTokenForwards(ctx context.Context, forwards []*history.ExternalTokenForward) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgxPool.Begin() error: %w", err)
	}
	defer tx.Rollback(ctx)

	rowSrc := copyFromTokenForwards{
		forwards: forwards,
		idx:      -1,
	}

	_, err = tx.Exec(ctx, `
	CREATE TEMP TABLE tmp_table ON COMMIT DROP AS
		SELECT *
		FROM external_token_forwards
		WITH NO DATA;
	`)
	if err != nil {
		return fmt.Errorf("CREATE TEMP TABLE error: %w", err)
	}

	count, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"tmp_table"},
		[]string{"id", "nodeid", "external_nodeid", "token", "resolved_time", "direction", "amount_msat"},
		&rowSrc,
	)
	if err != nil {
		return fmt.Errorf("CopyFrom() error: %w", err)
	}
	log.Printf("ImportTokenForwards count1: %v", count)

	cmdTag, err := tx.Exec(ctx, `
	INSERT INTO external_token_forwards
		SELECT *
		FROM tmp_table
	ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("INSERT INTO external_token_forwards error: %w", err)
	}
	log.Printf("ImportTokenForwards count2: %v", cmdTag.RowsAffected())

	return tx.Commit(ctx)
}

func (s *HistoryStore) MatchInternalForwards(ctx context.Context, start time.Time, end time.Time) error {
	matches, err := s.getInternalMatches(ctx, start.UnixNano(), end.UnixNano())
	if err != nil {
		return err
	}

	log.Printf("MatchInternalForwards: inserting %d matches", len(matches))
	if len(matches) == 0 {
		return nil
	}

	return s.insertInternalMatches(ctx, matches)
}

func (s *HistoryStore) MatchExternalForwards(ctx context.Context, start time.Time, end time.Time) error {
	matches, err := s.getExternalMatches(ctx, start.UnixNano(), end.UnixNano())
	if err != nil {
		return err
	}

	log.Printf("MatchExternalForwards: inserting %d matches", len(matches))
	if len(matches) == 0 {
		return nil
	}

	return s.insertExternalMatches(ctx, matches)
}

func (s *HistoryStore) GetFirstAndLastMatchedForwardTimes(ctx context.Context, internal bool) (*time.Time, *time.Time, error) {
	result, err := s.pool.Query(ctx, `
		SELECT MAX(resolved_time), MIN(resolved_time)
		FROM forwarding_history
		WHERE forward_correlation_in IS NOT NULL OR forward_correlation_out IS NOT NULL`)
	if err != nil {
		return nil, nil, err
	}
	defer result.Close()

	if !result.Next() {
		return nil, nil, fmt.Errorf("could not get a resolved time")
	}

	var last_time_ns *int64
	var first_time_ns *int64
	err = result.Scan(&last_time_ns, &first_time_ns)
	if err != nil {
		return nil, nil, err
	}

	if last_time_ns == nil || first_time_ns == nil {
		return nil, nil, nil
	}

	tfirst := time.Unix(0, *first_time_ns)
	tlast := time.Unix(0, *last_time_ns)
	return &tfirst, &tlast, nil
}

func (s *HistoryStore) GetFirstForwardTime(ctx context.Context) (*time.Time, error) {
	result := s.pool.QueryRow(ctx, `
	SELECT MIN(resolved_time)
	FROM forwarding_history
	`)

	var t *int64
	err := result.Scan(&t)
	if err != nil {
		return nil, err
	}

	if t == nil {
		return nil, nil
	}

	tt := time.Unix(0, *t)
	return &tt, nil
}

func (s *HistoryStore) GetForwardsWithoutChannelCount(ctx context.Context) (int64, error) {
	result, err := s.pool.Query(ctx, `
	SELECT COUNT(*)
	FROM forwarding_history
	WHERE funding_tx_id_out IS NULL
		OR funding_tx_id_in IS NULL`)
	if err != nil {
		return 0, err
	}
	defer result.Close()

	if !result.Next() {
		return 0, fmt.Errorf("could not get a count")
	}

	var count int64
	err = result.Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (s *HistoryStore) insertInternalMatches(ctx context.Context, matches []*match) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, m := range matches {
		var localColumn string
		var remoteColumn string
		if m.localIncoming {
			localColumn = "forward_correlation_in"
			remoteColumn = "forward_correlation_out"
		} else {
			localColumn = "forward_correlation_out"
			remoteColumn = "forward_correlation_in"
		}
		correlationId := s.generator.Next()
		_, err := tx.Exec(ctx, `
			INSERT INTO forward_correlations (id, internal_remote_forward_nodeid, internal_remote_forward_identifier)
			VALUES ($1, $2, $3, $4, $5)`,
			correlationId[:],
			m.remoteNodeId,
			m.remoteIdentifier,
		)
		if err != nil {
			return fmt.Errorf("failed to insert forward correlation: %w", err)
		}

		query := fmt.Sprintf(`
			UPDATE forwarding_history
			SET %s = $1
			WHERE nodeid = $2 AND identifier = $3`,
			localColumn,
		)
		_, err = tx.Exec(
			ctx,
			query,
			correlationId[:],
			m.localNodeId,
			m.localIdentifier,
		)
		if err != nil {
			return fmt.Errorf("failed to update local forward correlation: %w", err)
		}

		query = fmt.Sprintf(`
			UPDATE forwarding_history
			SET %s = $1
			WHERE nodeid = $2 AND identifier = $3`,
			remoteColumn,
		)
		_, err = tx.Exec(
			ctx,
			query,
			correlationId[:],
			m.remoteNodeId,
			m.remoteIdentifier,
		)
		if err != nil {
			return fmt.Errorf("failed to update remote forward correlation: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (s *HistoryStore) insertExternalMatches(ctx context.Context, matches []*match) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, m := range matches {
		var localColumn string
		if m.localIncoming {
			localColumn = "forward_correlation_in"
		} else {
			localColumn = "forward_correlation_out"
		}
		correlationId := s.generator.Next()
		_, err := tx.Exec(ctx, `
			INSERT INTO forward_correlations (id, external_remote_forward_id)
			VALUES ($1, $2, $3, $4::uuid)`,
			correlationId[:],
			m.remoteIdentifier,
		)
		if err != nil {
			return fmt.Errorf("failed to insert forward correlation: %w", err)
		}

		query := fmt.Sprintf(`
			UPDATE forwarding_history
			SET %s = $1
			WHERE nodeid = $2 AND identifier = $3`,
			localColumn,
		)
		_, err = tx.Exec(
			ctx,
			query,
			correlationId[:],
			m.localNodeId,
			m.localIdentifier,
		)
		if err != nil {
			return fmt.Errorf("failed to update local forward correlation: %w", err)
		}

		_, err = tx.Exec(ctx, `
			UPDATE external_token_forwards
			SET forward_correlation = $1
			WHERE id = $2::uuid`,
			correlationId[:],
			m.remoteIdentifier,
		)
		if err != nil {
			return fmt.Errorf("failed to update external forward correlation: %w", err)
		}
	}

	return tx.Commit(ctx)
}

type fwd struct {
	identifier   string
	resolvedTime int64
	nodeid       []byte
	peerid       []byte
	amtMsat      int64
}

type match struct {
	localIdentifier  string
	localNodeId      []byte
	remoteIdentifier string
	remoteNodeId     []byte
	localIncoming    bool
}

const (
	LocalBehind        = "local"
	RemoteBehind       = "remote"
	MaxResolveTimeDiff = time.Hour
)

func (s *HistoryStore) getInternalMatches(ctx context.Context, start int64, end int64) ([]*match, error) {
	localIncoming, err := s.pool.Query(ctx, `
		SELECT h.identifier
		,      h.resolved_time
		,      h.nodeid
		,      c.peerid
		,      h.amt_msat_in AS amt_msat
		FROM forwarding_history h
		INNER JOIN channels c
		    ON c.nodeid = h.nodeid AND c.funding_tx_id = h.funding_tx_id_in AND c.funding_tx_outnum = h.funding_tx_outnum_in
		WHERE h.forward_correlation_in IS NULL AND h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY nodeid, peerid, amt_msat, resolved_time`,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer localIncoming.Close()

	remoteOutgoing, err := s.pool.Query(ctx, `
		SELECT h.identifier
		,      h.resolved_time
		,      h.nodeid
		,      c.peerid
		,      h.amt_msat_out AS amt_msat
		FROM forwarding_history h
		INNER JOIN channels c
		    ON c.nodeid = h.nodeid AND c.funding_tx_id = h.funding_tx_id_out AND c.funding_tx_outnum = h.funding_tx_outnum_out
		WHERE h.forward_correlation_out IS NULL AND h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY peerid, nodeid, amt_msat, resolved_time
		`,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer remoteOutgoing.Close()

	matches1, err := getMatchesFromSources(localIncoming, remoteOutgoing, true)
	if err != nil {
		return nil, err
	}

	localOutgoing, err := s.pool.Query(ctx, `
		SELECT h.identifier
		,      h.resolved_time
		,      h.nodeid
		,      c.peerid
		,      h.amt_msat_out AS amt_msat
		FROM forwarding_history h
		INNER JOIN channels c
		    ON c.nodeid = h.nodeid AND c.funding_tx_id = h.funding_tx_id_out AND c.funding_tx_outnum = h.funding_tx_outnum_out
		WHERE h.forward_correlation_out IS NULL AND h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY nodeid, peerid, amt_msat, resolved_time`,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer localOutgoing.Close()

	remoteIncoming, err := s.pool.Query(ctx, `
		SELECT h.identifier
		,      h.resolved_time
		,      h.nodeid
		,      c.peerid
		,      h.amt_msat_in AS amt_msat
		FROM forwarding_history h
		INNER JOIN channels c
		    ON c.nodeid = h.nodeid AND c.funding_tx_id = h.funding_tx_id_in AND c.funding_tx_outnum = h.funding_tx_outnum_in
		WHERE h.forward_correlation_in IS NULL AND h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY peerid, nodeid, amt_msat, resolved_time
		`,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer remoteIncoming.Close()

	// Forwards are matched on their outgoing and incoming amount and the node and peer id.
	// The ordering is the same for each set, so they can be walked teh same way.

	matches2, err := getMatchesFromSources(localOutgoing, remoteIncoming, false)
	if err != nil {
		return nil, err
	}

	return append(matches1, matches2...), nil
}

func (s *HistoryStore) getExternalMatches(ctx context.Context, start int64, end int64) ([]*match, error) {
	localIncoming, err := s.pool.Query(ctx, `
		SELECT h.identifier
		,      h.resolved_time
		,      h.nodeid
		,      c.peerid
		,      h.amt_msat_in AS amt_msat
		FROM forwarding_history h
		INNER JOIN channels c
		    ON c.nodeid = h.nodeid AND c.funding_tx_id = h.funding_tx_id_in AND c.funding_tx_outnum = h.funding_tx_outnum_in
		WHERE h.forward_correlation_in IS NULL AND h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY nodeid, peerid, amt_msat, resolved_time`,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer localIncoming.Close()

	remoteOutgoing, err := s.pool.Query(ctx, `
		SELECT f.id::varchar AS identifier
		,      f.resolved_time
		,      f.external_nodeid AS nodeid
		,      f.nodeid AS peerid
		,      f.amount_msat AS amt_msat
		FROM external_token_forwards f
		WHERE f.forward_correlation IS NULL AND f.direction = 'send' AND h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY peerid, nodeid, amt_msat, resolved_time
		`,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer remoteOutgoing.Close()

	matches1, err := getMatchesFromSources(localIncoming, remoteOutgoing, true)
	if err != nil {
		return nil, err
	}

	localOutgoing, err := s.pool.Query(ctx, `
		SELECT h.identifier
		,      h.resolved_time
		,      h.nodeid
		,      c.peerid
		,      h.amt_msat_out AS amt_msat
		FROM forwarding_history h
		INNER JOIN channels c
		    ON c.nodeid = h.nodeid AND c.funding_tx_id = h.funding_tx_id_out AND c.funding_tx_outnum = h.funding_tx_outnum_out
		WHERE h.forward_correlation_out IS NULL AND h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY nodeid, peerid, amt_msat, resolved_time`,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer localOutgoing.Close()

	remoteIncoming, err := s.pool.Query(ctx, `
		SELECT f.id::varchar AS identifier
		,      f.resolved_time
		,      f.external_nodeid AS nodeid
		,      f.nodeid AS peerid
		,      f.amount_msat AS amt_msat
		FROM external_token_forwards f
		WHERE f.forward_correlation IS NULL AND f.direction = 'receive' AND h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY peerid, nodeid, amt_msat, resolved_time
		`,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer remoteIncoming.Close()

	// Forwards are matched on their outgoing and incoming amount and the node and peer id.
	// The ordering is the same for each set, so they can be walked teh same way.

	matches2, err := getMatchesFromSources(localOutgoing, remoteIncoming, false)
	if err != nil {
		return nil, err
	}

	return append(matches1, matches2...), nil
}

func getMatchesFromSources(localSrc pgx.Rows, remoteSrc pgx.Rows, localIncoming bool) ([]*match, error) {
	var matches []*match
	var err error
	var currentLocalFwd *fwd
	var currentRemoteFwd *fwd
	scanLocal := true
	scanRemote := true

	for {
		// get the next record, either a new remote or local record, based on
		// the state of scanLocal and scanRemote.
		if scanLocal {
			currentLocalFwd, err = next(localSrc)
			if err != nil {
				return nil, err
			}
		}
		if scanRemote {
			currentRemoteFwd, err = next(remoteSrc)
			if err != nil {
				return nil, err
			}
		}
		// If either side doesn't have a forward anymore, there's nothing left to do.
		if currentLocalFwd == nil || currentRemoteFwd == nil {
			break
		}

		isMatch, behind := compare(currentLocalFwd, currentRemoteFwd)
		if !isMatch {
			if behind == LocalBehind {
				scanLocal = true
				scanRemote = false
			} else if behind == RemoteBehind {
				scanLocal = false
				scanRemote = true
			} else {
				return nil, fmt.Errorf("expected local or remote to be behind but was neither - bug")
			}
			continue
		}

		// We're currently at a match (same node, peer and amount)
		// Grab all the forwards for this node, peer and amount in resolved_time order
		// on both sides to see which ones are the best match.
		var localForwards []*fwd = []*fwd{currentLocalFwd}
		var remoteForwards []*fwd = []*fwd{currentRemoteFwd}
		var nextLocalFwd *fwd
		var nextRemoteFwd *fwd
		// Get all local forwards for the same node, peer, amount
		for {
			nextLocalFwd, err = next(localSrc)
			if err != nil {
				return nil, err
			}
			if nextLocalFwd == nil {
				break
			}
			isMatch, _ := compare(nextLocalFwd, currentRemoteFwd)
			if isMatch {
				localForwards = append(localForwards, nextLocalFwd)
			}
		}

		// Get all remote forwards for the same node, peer, amount
		for {
			nextRemoteFwd, err = next(remoteSrc)
			if err != nil {
				return nil, err
			}
			if nextRemoteFwd == nil {
				break
			}
			isMatch, _ := compare(currentLocalFwd, nextRemoteFwd)
			if isMatch {
				remoteForwards = append(remoteForwards, nextRemoteFwd)
			}
		}

		// Both sides may have forwards on the start or end that don't exist
		// on the other side, because they fell off the time interval. We'll
		// assume same length slices are equivalent forwards for simplicity.
		if len(localForwards) > len(remoteForwards) {
			diff := len(localForwards) - len(remoteForwards)
			if math.Abs(float64(localForwards[diff].resolvedTime-remoteForwards[0].resolvedTime)) >=
				math.Abs(float64(localForwards[0].resolvedTime-remoteForwards[diff].resolvedTime)) {
				localForwards = localForwards[:len(localForwards)-diff]
			} else {
				localForwards = localForwards[diff:]
			}
		}

		if len(remoteForwards) > len(localForwards) {
			diff := len(remoteForwards) - len(localForwards)
			if math.Abs(float64(remoteForwards[diff].resolvedTime-localForwards[0].resolvedTime)) >=
				math.Abs(float64(remoteForwards[0].resolvedTime-localForwards[diff].resolvedTime)) {
				remoteForwards = remoteForwards[:len(remoteForwards)-diff]
			} else {
				remoteForwards = remoteForwards[diff:]
			}
		}

		// TODO: This causes the algorithm to be N^2
		for i := 0; i < len(localForwards); i++ {
			localFwd := localForwards[i]
			remoteFwd := remoteForwards[i]
			matches = append(matches, &match{
				localIdentifier:  localFwd.identifier,
				localNodeId:      localFwd.nodeid,
				remoteIdentifier: remoteFwd.identifier,
				remoteNodeId:     remoteFwd.nodeid,
				localIncoming:    localIncoming,
			})
		}

		// The next records fetched are the first ones that didn't match. Continue
		// the outer loop with these forwards.
		currentLocalFwd = nextLocalFwd
		currentRemoteFwd = nextRemoteFwd
		scanLocal = false
		scanRemote = false
	}

	return matches, nil
}

func compare(localFwd *fwd, remoteFwd *fwd) (bool, string) {
	nodeCompare := bytes.Compare(localFwd.nodeid, remoteFwd.peerid)
	if nodeCompare > 0 {
		// different node id, local is ahead
		return false, RemoteBehind
	}
	if nodeCompare < 0 {
		// different node id, remote is ahead
		return false, LocalBehind
	}

	// same nodeid

	peerCompare := bytes.Compare(localFwd.peerid, remoteFwd.nodeid)
	if peerCompare > 0 {
		// different peer id, local is ahead
		return false, RemoteBehind
	}
	if peerCompare < 0 {
		// different peer id, remote is ahead
		return false, LocalBehind
	}

	// same nodeid and peerid

	if localFwd.amtMsat > remoteFwd.amtMsat {
		// different amount, local is ahead
		return false, RemoteBehind
	}
	if localFwd.amtMsat < remoteFwd.amtMsat {
		// different amount, remote is ahead
		return false, LocalBehind
	}

	// same nodeid and peerid and amount

	return true, ""
}

func next(src pgx.Rows) (*fwd, error) {
	if !src.Next() {
		return nil, nil
	}

	fwd := &fwd{}
	err := src.Scan(&fwd.identifier, &fwd.resolvedTime, &fwd.nodeid, &fwd.peerid, &fwd.amtMsat)
	if err != nil {
		return nil, err
	}

	return fwd, nil
}
