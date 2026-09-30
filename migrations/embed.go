// Package migrations embeds the SQL migration files, so the server binary can
// apply them from any working directory.
//
// Add a new migration as NNNNNN_<slug>.up.sql and NNNNNN_<slug>.down.sql.
// The schema changes only through these files (no GORM AutoMigrate).
package migrations

import "embed"

// FS holds the *.sql files of this folder.
//
//go:embed *.sql
var FS embed.FS
