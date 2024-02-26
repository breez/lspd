package postgresql

import (
	"context"
	"log"
	"time"

	"github.com/breez/lspd/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresOpeningStore struct {
	pool *pgxpool.Pool
}

func NewPostgresOpeningStore(pool *pgxpool.Pool) *PostgresOpeningStore {
	return &PostgresOpeningStore{pool: pool}
}

func (s *PostgresOpeningStore) GetFeeParamsSettings(token string) ([]*common.OpeningFeeParamsSetting, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT validity
		 ,      min_fee_msat 
		 ,      proportional
		 ,      min_lifetime
		 ,      max_client_to_self_delay
		 ,      min_payment_size_msat
		 ,      max_payment_size_msat
		 FROM new_channel_params WHERE token=$1 AND validity>0`, token)
	if err != nil {
		log.Printf("GetFeeParamsSettings(%v) error: %v", token, err)
		return nil, err
	}
	defer rows.Close()

	var settings []*common.OpeningFeeParamsSetting
	for rows.Next() {
		var validity int64
		var min_fee_msat int64
		var proportional uint32
		var min_lifetime uint32
		var max_client_to_self_delay uint32
		var min_payment_size_msat int64
		var max_payment_size_msat int64
		err = rows.Scan(
			&validity,
			&min_fee_msat,
			&proportional,
			&min_lifetime,
			&max_client_to_self_delay,
			&min_payment_size_msat,
			&max_payment_size_msat,
		)
		if err != nil {
			return nil, err
		}

		duration := time.Second * time.Duration(validity)
		settings = append(settings, &common.OpeningFeeParamsSetting{
			Validity:             duration,
			MinFeeMsat:           uint64(min_fee_msat),
			Proportional:         proportional,
			MinLifetime:          min_lifetime,
			MaxClientToSelfDelay: max_client_to_self_delay,
			MinPaymentSizeMsat:   uint64(min_payment_size_msat),
			MaxPaymentSizeMsat:   uint64(max_payment_size_msat),
		})
	}

	return settings, nil
}
