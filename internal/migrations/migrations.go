package migrations

import (
	"errors"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	sqlfiles "AskCore/migrations"
)

// Up applies all pending migrations to the SQLite database at dsn.
// The dsn has the same form as the one given to gormstore.Open.
func Up(dsn string) (err error) {
	source, err := iofs.New(sqlfiles.FS, ".")
	if err != nil {
		return err
	}
	migration, err := migrate.NewWithSourceInstance("iofs", source, "sqlite3://"+dsn)
	if err != nil {
		return err
	}
	defer func() {
		sourceErr, dbErr := migration.Close()
		err = errors.Join(err, sourceErr, dbErr)
	}()

	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
