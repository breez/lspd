package postgresql

import (
	"context"
	"strings"

	"github.com/breez/lspd/lsps2"
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
	_, err := s.pool.Exec(
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
		 )
		 VALUES ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?`,
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
	)
	if err != nil {
		if strings.Contains(err.Error(), "idx_lsps2_buy_registrations_scid") {
			return lsps2.ErrScidExists
		}

		return err
	}

	return nil
}
