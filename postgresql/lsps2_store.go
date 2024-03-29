package postgresql

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/GoWebProd/uuid7"
	"github.com/breez/lspd/common"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/lsps2"
	"github.com/btcsuite/btcd/wire"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Lsps2Store struct {
	pool      *pgxpool.Pool
	generator *uuid7.Generator
}

func NewLsps2Store(pool *pgxpool.Pool) *Lsps2Store {
	generator := uuid7.New()
	return &Lsps2Store{pool: pool, generator: generator}
}

func (s *Lsps2Store) RegisterBuy(
	ctx context.Context,
	req *lsps2.RegisterBuy,
) error {
	row := s.pool.QueryRow(
		ctx,
		`SELECT token
		 FROM lsps2.promises
		 WHERE promise = $1`,
		req.OpeningFeeParams.Promise,
	)
	var token string
	err := row.Scan(&token)
	if err != nil {
		return fmt.Errorf("promise does not have matching token")
	}

	var uuid [16]byte = s.generator.Next()
	_, err = s.pool.Exec(
		ctx,
		`INSERT INTO lsps2.buy_registrations (
			id
		 ,	lsp_id
		 ,  peer_id
		 ,  scid
		 ,  mode
		 ,  payment_size_msat
		 ,  params_min_fee_msat
		 ,  params_proportional
		 ,  params_valid_until
		 ,  params_min_lifetime
		 ,  params_max_client_to_self_delay
		 ,  params_promise
		 ,  token
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		uuid,
		req.LspId,
		req.PeerId,
		int64(uint64(req.Scid)),
		int(req.Mode),
		req.PaymentSizeMsat,
		int64(req.OpeningFeeParams.MinFeeMsat),
		req.OpeningFeeParams.Proportional,
		req.OpeningFeeParams.ValidUntil,
		req.OpeningFeeParams.MinLifetime,
		req.OpeningFeeParams.MaxClientToSelfDelay,
		req.OpeningFeeParams.Promise,
		token,
	)
	if err != nil {
		if strings.Contains(err.Error(), "idx_lsps2_buy_registrations_scid") {
			return lsps2.ErrScidExists
		}

		return err
	}

	return nil
}

func (s *Lsps2Store) GetBuyRegistration(ctx context.Context, scid lightning.ShortChannelID) (*lsps2.BuyRegistration, error) {
	row := s.pool.QueryRow(
		ctx,
		`SELECT r.id
		,       r.lsp_id
		,       r.peer_id
		,       r.scid
		,       r.mode
		,       r.payment_size_msat
		,       r.params_min_fee_msat
		,       r.params_proportional
		,       r.params_valid_until
		,       r.params_min_lifetime
		,       r.params_max_client_to_self_delay
		,       r.params_promise
		,       r.token
		,       c.funding_tx_id
		,       c.funding_tx_outnum
		,       c.is_completed
		 FROM lsps2.buy_registrations r
		 LEFT JOIN lsps2.bought_channels c ON r.id = c.registration_id
		 WHERE r.scid = $1`,
		int64(uint64(scid)),
	)
	var db_id [16]byte
	var db_lsp_id string
	var db_peer_id string
	var db_scid int64
	var db_mode int
	var db_payment_size_msat *int64
	var db_params_min_fee_msat int64
	var db_params_proportional uint32
	var db_params_valid_until string
	var db_params_min_lifetime uint32
	var db_params_max_client_to_self_delay uint32
	var db_params_promise string
	var db_token string
	var db_funding_tx_id *[]byte
	var db_funding_tx_outnum *uint32
	var db_is_completed *bool
	err := row.Scan(
		&db_id,
		&db_lsp_id,
		&db_peer_id,
		&db_scid,
		&db_mode,
		&db_payment_size_msat,
		&db_params_min_fee_msat,
		&db_params_proportional,
		&db_params_valid_until,
		&db_params_min_lifetime,
		&db_params_max_client_to_self_delay,
		&db_params_promise,
		&db_token,
		&db_funding_tx_id,
		&db_funding_tx_outnum,
		&db_is_completed,
	)
	if err == pgx.ErrNoRows {
		return nil, lsps2.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var paymentSizeMsat *uint64
	if db_payment_size_msat != nil {
		p := uint64(*db_payment_size_msat)
		paymentSizeMsat = &p
	}

	var cp *wire.OutPoint
	if db_funding_tx_id != nil && db_funding_tx_outnum != nil {
		cp, err = lightning.NewOutPoint(*db_funding_tx_id, *db_funding_tx_outnum)
		if err != nil {
			return nil, fmt.Errorf("invalid funding tx id in db: %x", db_funding_tx_id)
		}
	}

	isCompleted := false
	if db_is_completed != nil {
		isCompleted = *db_is_completed
	}

	return &lsps2.BuyRegistration{
		Id:     db_id,
		LspId:  db_lsp_id,
		PeerId: db_peer_id,
		Scid:   lightning.ShortChannelID(uint64(db_scid)),
		OpeningFeeParams: common.OpeningFeeParams{
			MinFeeMsat:           uint64(db_params_min_fee_msat),
			Proportional:         db_params_proportional,
			ValidUntil:           db_params_valid_until,
			MinLifetime:          db_params_min_lifetime,
			MaxClientToSelfDelay: db_params_max_client_to_self_delay,
			Promise:              db_params_promise,
		},
		PaymentSizeMsat: paymentSizeMsat,
		Mode:            lsps2.OpeningMode(db_mode),
		ChannelPoint:    cp,
		IsComplete:      isCompleted,
	}, nil
}

func (s *Lsps2Store) SetChannelOpened(ctx context.Context, channelOpened *lsps2.ChannelOpened) error {
	var uuid [16]byte = s.generator.Next()
	var registrationId [16]byte = channelOpened.RegistrationId
	_, err := s.pool.Exec(
		ctx,
		`INSERT INTO lsps2.bought_channels (
			id,
			registration_id,
			funding_tx_id,
			funding_tx_outnum,
			fee_msat,
			payment_size_msat,
			is_completed
		) VALUES ($1, $2, $3, $4, $5, $6, false)`,
		uuid,
		registrationId,
		channelOpened.Outpoint.Hash[:],
		channelOpened.Outpoint.Index,
		int64(channelOpened.FeeMsat),
		int64(channelOpened.PaymentSizeMsat),
	)

	return err
}

func (s *Lsps2Store) SetCompleted(ctx context.Context, registrationId uuid7.UUID) error {
	rows, err := s.pool.Exec(
		ctx,
		`UPDATE lsps2.bought_channels
		 SET is_completed = true
		 WHERE registration_id = $1`,
		registrationId,
	)
	if err != nil {
		return err
	}

	if rows.RowsAffected() == 0 {
		return fmt.Errorf("no rows were updated")
	}

	return nil
}

func (s *Lsps2Store) SavePromises(
	ctx context.Context,
	req *lsps2.SavePromises,
) error {
	if len(req.Menu) == 0 {
		return nil
	}

	rows := [][]interface{}{}
	for _, p := range req.Menu {
		rows = append(rows, []interface{}{p.Promise, req.Token, p.ValidUntil})
	}
	_, err := s.pool.CopyFrom(
		ctx,
		pgx.Identifier{"lsps2", "promises"},
		[]string{"promise", "token", "valid_until"},
		pgx.CopyFromRows(rows),
	)
	return err
}

func (s *Lsps2Store) RemoveUnusedExpired(
	ctx context.Context,
	before time.Time,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	// Rollback will not do anything if Commit() has already been called.
	defer tx.Rollback(ctx)

	timestamp := before.Format(lsps0.TIME_FORMAT)

	// Promises can be deleted without issue.
	tag, err := tx.Exec(
		ctx,
		`DELETE FROM lsps2.buy_registrations r
		 WHERE r.id IN (
			SELECT sr.id
			FROM lsps2.buy_registrations sr
			LEFT JOIN lsps2.bought_channels sb ON sr.id = sb.registration_id
			WHERE sb.registration_id IS NULL
				AND sr.params_valid_until < $1
		 )`,
		timestamp,
	)
	if err != nil {
		return err
	}
	rowsAffected := tag.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Deleted %d expired buy registrations before %s", rowsAffected, timestamp)
	}

	tag, err = tx.Exec(
		ctx,
		`DELETE FROM lsps2.promises
		 WHERE valid_until < $1`,
		timestamp,
	)
	if err != nil {
		return err
	}
	rowsAffected = tag.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Deleted %d expired promises before %s", rowsAffected, timestamp)
	}

	return tx.Commit(ctx)
}
