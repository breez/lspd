package postgresql

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func PgConnect(databaseUrl string) (*pgxpool.Pool, error) {
	var err error
	pgxPool, err := pgxpool.New(context.Background(), databaseUrl)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.Connect(%v): %w", databaseUrl, err)
	}
	return pgxPool, nil
}
