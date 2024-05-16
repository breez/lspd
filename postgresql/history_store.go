package postgresql

import (
	"github.com/GoWebProd/uuid7"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
