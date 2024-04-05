package postgresql

import (
	"embed"
	"fmt"
	"log"

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
	m.Log = &MigrationLogger{}
	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("could not get current database migration version: %w", err)
	}
	log.Printf("Current database version is %d, dirty = %t", version, dirty)
	err = m.Up()
	if err != nil {
		return fmt.Errorf("failed to apply database migrations: %w", err)
	}

	return nil
}

type MigrationLogger struct {
}

func (l *MigrationLogger) Printf(format string, v ...interface{}) {
	log.Printf("Applied migration "+format, v...)
}

// Verbose should return true when verbose logging output is wanted
func (l *MigrationLogger) Verbose() bool {
	return false
}
