package postgresql

import (
	"context"
	"fmt"
	"strings"

	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps2"
	"github.com/breez/lspd/shared"
	"github.com/btcsuite/btcd/wire"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type Lsps2Store struct {
	pool *pgxpool.Pool
}

func NewLsps2Store(pool *pgxpool.Pool) *Lsps2Store {
	return &Lsps2Store{pool: pool}
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

	_, err = s.pool.Exec(
		ctx,
		`INSERT INTO lsps2.buy_registrations (
			lsp_id
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
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
	var db_id uint64
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
	var db_funding_tx_id []byte
	var db_funding_tx_outnum uint32
	var db_is_completed bool
	err := row.Scan(
		&db_id,
		&db_lsp_id,
		&db_peer_id,
		&db_scid,
		db_payment_size_msat,
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
	if db_funding_tx_id != nil {
		cp, err = lightning.NewOutPoint(db_funding_tx_id, db_funding_tx_outnum)
		if err != nil {
			return nil, fmt.Errorf("invalid funding tx id in db: %x", db_funding_tx_id)
		}
	}

	return &lsps2.BuyRegistration{
		Id:     db_id,
		LspId:  db_lsp_id,
		PeerId: db_peer_id,
		Scid:   lightning.ShortChannelID(uint64(db_scid)),
		OpeningFeeParams: shared.OpeningFeeParams{
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
		IsComplete:      db_is_completed,
	}, nil
}

func (s *Lsps2Store) SetChannelOpened(ctx context.Context, channelOpened *lsps2.ChannelOpened) error {
	_, err := s.pool.Exec(
		ctx,
		`INSERT INTO lsps2.bought_channels (
			registration_id,
			funding_tx_id,
			funding_tx_outnum,
			fee_msat,
			payment_size_msat,
			is_completed
		) VALUES ($1, $2, $3, $4, $5, false)`,
		channelOpened.RegistrationId,
		channelOpened.Outpoint.Hash[:],
		channelOpened.Outpoint.Index,
		int64(channelOpened.FeeMsat),
		int64(channelOpened.PaymentSizeMsat),
	)

	return err
}

func (s *Lsps2Store) SetCompleted(ctx context.Context, registrationId uint64) error {
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
