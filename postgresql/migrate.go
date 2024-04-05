package postgresql

import (
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var fs embed.FS

func Migrate(databaseUrl string) error {
	d, err := iofs.New(fs, "migrations")
	if err != nil {
		return fmt.Errorf("failed to find embedded migrations folder: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", d, databaseUrl)
	if err != nil {
		return fmt.Errorf("could not connect to db for migrations: %w", err)
	}
	err = m.Up()
	if err != nil {
		return fmt.Errorf("failed to apply database migrations: %w", err)
	}

	return nil
}
