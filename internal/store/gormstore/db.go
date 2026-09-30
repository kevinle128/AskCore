package gormstore

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Open opens the SQLite database at dsn.
//
// The schema comes only from the SQL files in migrations/. Open does not run
// GORM AutoMigrate.
func Open(dsn string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
}
