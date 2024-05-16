package postgresql

import (
	"context"
	"fmt"
	"time"

	"github.com/breez/lspd/history"
	"github.com/breez/lspd/lightning"
)

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

func (s *HistoryStore) ExportTokenForwardsForExternalNode(
	ctx context.Context,
	startNs uint64,
	endNs uint64,
	node []byte,
	externalNode []byte,
) ([]*history.ExportedForward, error) {
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

	result := make([]*history.ExportedForward, rows.CommandTag().RowsAffected())
	for rows.Next() {
		var direction string
		var token string
		var resolved_time int64
		var amt_msat int64
		err = rows.Scan(&direction, &token, &resolved_time, &amt_msat)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan err: %w", err)
		}

		result = append(result, &history.ExportedForward{
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

func (s *HistoryStore) sanityCheck(ctx context.Context, startNs, endNs uint64) error {
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

func (s *HistoryStore) GetOpenChannelHtlcs(
	ctx context.Context,
	startNs uint64,
	endNs uint64,
) ([]*history.OpenChannelHtlc, error) {
	// filter htlcs used for channel opens. Only include the ones that may have been actually settled.
	openChannelHtlcs, err := s.pool.Query(ctx, `
		SELECT htlc.nodeid
		,      htlc.peerid
		,      htlc.forward_amt_msat
		,      htlc.original_amt_msat
		,      htlc.incoming_amt_msat
		,      htlc.funding_tx_id
		,      htlc.funding_tx_outnum
		,      htlc.forward_time
		FROM open_channel_htlcs htlc
		INNER JOIN (
			SELECT DISTINCT h.nodeid, h.peerid, c.funding_tx_id, c.funding_tx_outnum
			FROM forwarding_history h
			INNER JOIN channels c
				ON h.nodeid = c.nodeid AND (h.chanid_out = c.confirmed_scid OR h.chanid_out = c.alias_scid)
			WHERE h.resolved_time >= $1 AND h.resolved_time < $2
		) a
			ON htlc.nodeid = a.nodeid
				AND htlc.peerid = a.peerid
				AND htlc.funding_tx_id = a.funding_tx_id
				AND htlc.funding_tx_outnum = a.funding_tx_outnum
		ORDER BY htlc.nodeid, htlc.peerid, htlc.funding_tx_id, htlc.funding_tx_outnum, htlc.forward_time
	`, startNs, endNs)
	if err != nil {
		return nil, fmt.Errorf("failed to query open channel htlcs: %w", err)
	}
	defer openChannelHtlcs.Close()

	var result []*history.OpenChannelHtlc
	for openChannelHtlcs.Next() {
		var nodeid []byte
		var peerid []byte
		var forward_amt_msat int64
		var original_amt_msat int64
		var incoming_amt_msat int64
		var funding_tx_id []byte
		var funding_tx_outnum uint32
		var forward_time int64
		err = openChannelHtlcs.Scan(
			&nodeid,
			&peerid,
			&forward_amt_msat,
			&original_amt_msat,
			&incoming_amt_msat,
			&funding_tx_id,
			&funding_tx_outnum,
			&forward_time,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan open channel htlc: %w", err)
		}

		cp, err := lightning.NewOutPoint(funding_tx_id, funding_tx_outnum)
		if err != nil {
			return nil, fmt.Errorf("invalid funding outpoint: %w", err)
		}

		result = append(result, &history.OpenChannelHtlc{
			NodeId:             nodeid,
			PeerId:             peerid,
			ForwardAmountMsat:  uint64(forward_amt_msat),
			OriginalAmountMsat: uint64(original_amt_msat),
			IncomingAmountMsat: uint64(incoming_amt_msat),
			ChannelPoint:       cp,
			ForwardTime:        time.Unix(0, forward_time),
		})
	}

	return result, nil
}

// Gets all settled forwards in the defined time range. Ordered by nodeid, peerid_in, amt_msat_in, resolved_time
func (s *HistoryStore) GetForwards(
	ctx context.Context,
	startNs uint64,
	endNs uint64,
) ([]*history.RevenueForward, error) {
	err := s.sanityCheck(ctx, startNs, endNs)
	if err != nil {
		return nil, err
	}

	ctxc, cancel := context.WithCancel(ctx)
	defer cancel()

	// Select all forwards, and include information about the channel and token used
	ourForwards, err := s.pool.Query(ctxc, tokenChannelsCte+`
		SELECT h.identifier
		,      h.nodeid
		,      h.peerid_in
		,      h.peerid_out
		,      h.amt_msat_in
		,      h.amt_msat_out
		,      h.resolved_time
		,      c_in.funding_tx_id AS funding_tx_id_in
		,      c_in.funding_tx_outnum AS funding_tx_outnum_in
		,      c_out.funding_tx_id AS funding_tx_id_out
		,      c_out.funding_tx_outnum AS funding_tx_outnum_out
		,      tc_in.token AS send_token
		,      tc_out.token AS receive_token
		FROM forwarding_history h
		INNER JOIN channels c_in
			ON h.nodeid = c_in.nodeid AND (h.chanid_in = c_in.confirmed_scid OR h.chanid_in = c_in.alias_scid)
		INNER JOIN channels c_out
			ON h.nodeid = c_out.nodeid AND (h.chanid_out = c_out.confirmed_scid OR h.chanid_out = c_out.alias_scid)
		LEFT JOIN token_channels tc_in
			ON c_in.nodeid = tc_in.nodeid AND c_in.funding_tx_id = tc_in.funding_tx_id AND c_in.funding_tx_outnum = tc_in.funding_tx_outnum
		LEFT JOIN token_channels tc_out
			ON c_out.nodeid = tc_out.nodeid AND c_out.funding_tx_id = tc_out.funding_tx_id AND c_out.funding_tx_outnum = tc_out.funding_tx_outnum
		WHERE h.resolved_time >= $1 AND h.resolved_time < $2
		ORDER BY h.nodeid, h.peerid_in, h.amt_msat_in, h.resolved_time
	`, startNs, endNs)
	if err != nil {
		return nil, fmt.Errorf("failed to query our forwards: %w", err)
	}

	var forwards []*history.RevenueForward
	for ourForwards.Next() {
		var identifier string
		var nodeid []byte
		var peerid_in []byte
		var peerid_out []byte
		var amt_msat_in int64
		var amt_msat_out int64
		var resolved_time int64
		var funding_tx_id_in []byte
		var funding_tx_outnum_in uint32
		var funding_tx_id_out []byte
		var funding_tx_outnum_out uint32
		var send_token *string
		var receive_token *string
		err = ourForwards.Scan(
			&identifier,
			&nodeid,
			&peerid_in,
			&peerid_out,
			&amt_msat_in,
			&amt_msat_out,
			&resolved_time,
			&funding_tx_id_in,
			&funding_tx_outnum_in,
			&funding_tx_id_out,
			&funding_tx_outnum_out,
			&send_token,
			&receive_token,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan our forward: %w", err)
		}

		cpIn, err := lightning.NewOutPoint(funding_tx_id_in, funding_tx_outnum_in)
		if err != nil {
			return nil, fmt.Errorf("invalid funding outpoint: %w", err)
		}
		cpOut, err := lightning.NewOutPoint(funding_tx_id_out, funding_tx_outnum_out)
		if err != nil {
			return nil, fmt.Errorf("invalid funding outpoint: %w", err)
		}
		forwards = append(forwards, &history.RevenueForward{
			Identifier:      identifier,
			Nodeid:          nodeid,
			PeeridIn:        peerid_in,
			PeeridOut:       peerid_out,
			AmtMsatIn:       uint64(amt_msat_in),
			AmtMsatOut:      uint64(amt_msat_out),
			ResolvedTime:    uint64(resolved_time),
			ChannelPointIn:  *cpIn,
			ChannelPointOut: *cpOut,
			SendToken:       send_token,
			ReceiveToken:    receive_token,
		})
	}

	return forwards, nil
}
