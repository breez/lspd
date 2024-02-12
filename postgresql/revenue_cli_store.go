package postgresql

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RevenueCliStore struct {
	pool *pgxpool.Pool
}

func NewCliStore(pool *pgxpool.Pool) *RevenueCliStore {
	return &RevenueCliStore{
		pool: pool,
	}
}

// This CTE selects all channels that were opened associated to a token.
const tokenChannelsCte = `
WITH token_channels AS (
	SELECT p.tag::json->>'apiKeyHash' AS token
	,      c.nodeid
	,      c.peerid
	,      c.funding_tx_id
	,      c.funding_tx_outnum
	,      c.alias_scid
	,      c.confirmed_scid
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
	,      c.alias_scid
	,      c.confirmed_scid
	,      b.fee_msat AS channel_fee_msat
	FROM lsps2.bought_channels b
	INNER JOIN lsps2.buy_registrations r
		ON b.registration_id = r.id
	INNER JOIN public.channels c 
		ON b.funding_tx_id = c.funding_tx_id 
		AND b.funding_tx_outnum = c.funding_tx_outnum
)`

type ExportedForward struct {
	Token          string
	NodeId         []byte
	ExternalNodeId []byte
	ResolvedTime   time.Time
	// Direction is 'send' if the client associated to the token sent a payment.
	// Direction is 'receive' if the client associated to the token sent a payment.
	Direction string
	// The amount forwarded to/from the external node
	AmountMsat uint64
}

func (s *RevenueCliStore) ExportTokenForwardsForExternalNode(
	ctx context.Context,
	startNs uint64,
	endNs uint64,
	node []byte,
	externalNode []byte,
) ([]*ExportedForward, error) {
	err := s.sanityCheck(ctx, startNs, endNs)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(
		ctx, tokenChannelsCte+`
		SELECT * FROM (
			SELECT 'send' AS direction
			,      c_in.token
			,      h.resolved_time
			,      h.amt_msat_out AS amt_msat
			FROM public.forwarding_history h
			INNER JOIN public.channels c_out
				ON h.nodeid = c_out.nodeid 
					AND (h.chanid_out = c_out.confirmed_scid OR h.chanid_out = c_out.alias_scid)
			INNER JOIN token_channels c_in
				ON h.nodeid = c_in.nodeid 
					AND (h.chanid_in = c_in.confirmed_scid OR h.chanid_in = c_in.alias_scid)
			WHERE h.nodeid = $1 AND c_out.peerid = $2 AND h.resolved_time >= $3 AND h.resolved_time < $4
			UNION ALL
			SELECT 'receive' AS direction
			,      c_out.token
			,      h.resolved_time
			,      h.amt_msat_in AS amt_msat
			FROM public.forwarding_history h
			INNER JOIN token_channels c_out
			    ON h.nodeid = c_out.nodeid 
					AND (h.chanid_out = c_out.confirmed_scid OR h.chanid_out = c_out.alias_scid)
			INNER JOIN public.channels c_in
			    ON h.nodeid = c_in.nodeid 
					AND (h.chanid_in = c_in.confirmed_scid OR h.chanid_in = c_in.alias_scid)
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

	result := make([]*ExportedForward, rows.CommandTag().RowsAffected())
	for rows.Next() {
		var direction string
		var token string
		var resolved_time int64
		var amt_msat int64
		err = rows.Scan(&direction, &token, &resolved_time, &amt_msat)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan err: %w", err)
		}

		result = append(result, &ExportedForward{
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

func (s *RevenueCliStore) sanityCheck(ctx context.Context, startNs, endNs uint64) error {
	// Sanity check, does forward/channel sync work? Can all forwards be associated to a channel?
	row := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM forwarding_history h
		LEFT JOIN channels c_in
			ON h.nodeid = c_in.nodeid AND (h.chanid_in = c_in.confirmed_scid OR h.chanid_in = c_in.alias_scid)
		LEFT JOIN channels c_out
			ON h.nodeid = c_out.nodeid AND (h.chanid_out = c_out.confirmed_scid OR h.chanid_out = c_out.alias_scid)
		WHERE h.resolved_time >= $1 AND h.resolved_time < $2
			AND (c_in.nodeid IS NULL OR c_out.nodeid IS NULL)
	`, startNs, endNs)
	var count int64
	err := row.Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to do sanity check: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("%d local forwards in the selected time range could not be associated to their channels. Is forward/channel sync working?", count)
	}

	return nil
}

