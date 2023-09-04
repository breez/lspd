package postgresql

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/breez/lspd/shared"
	"github.com/jackc/pgx/v4/pgxpool"
)

type extendedParams struct {
	Token  string                  `json:"token"`
	Params shared.OpeningFeeParams `json:"fees_params"`
}

type PostgresOpeningStore struct {
	pool *pgxpool.Pool
}

func NewPostgresOpeningStore(pool *pgxpool.Pool) *PostgresOpeningStore {
	return &PostgresOpeningStore{pool: pool}
}

func (s *PostgresOpeningStore) GetFeeParamsSettings(token string) ([]*shared.OpeningFeeParamsSetting, error) {
	rows, err := s.pool.Query(context.Background(), `SELECT validity, params FROM new_channel_params WHERE token=$1`, token)
	if err != nil {
		log.Printf("GetFeeParamsSettings(%v) error: %v", token, err)
		return nil, err
	}

	var settings []*shared.OpeningFeeParamsSetting
	for rows.Next() {
		var validity int64
		var param string
		err = rows.Scan(&validity, &param)
		if err != nil {
			return nil, err
		}

		var params *shared.OpeningFeeParams
		err := json.Unmarshal([]byte(param), &params)
		if err != nil {
			log.Printf("Failed to unmarshal fee param '%v': %v", param, err)
			return nil, err
		}

		duration := time.Second * time.Duration(validity)
		settings = append(settings, &shared.OpeningFeeParamsSetting{
			Validity: duration,
			Params:   params,
		})
	}

	return settings, nil
}
