package migrations

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestUp_CreatesTablesAndIsIdempotent(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "test.db")

	require.NoError(t, Up(dsn))
	require.NoError(t, Up(dsn), "a second run must be a no-op")

	db, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, table := range []string{"users", "posts"} {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		require.NoError(t, err, "table %s", table)
	}
}
