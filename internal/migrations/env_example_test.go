package migrations

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUp_WithEnvExampleDatabaseURL runs the migrations with the DATABASE_URL
// value from .env.example, so the Getting Started flow keeps working.
func TestUp_WithEnvExampleDatabaseURL(t *testing.T) {
	file, err := os.Open("../../.env.example")
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	var dsn string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "DATABASE_URL="); ok {
			dsn = value
		}
	}
	require.NoError(t, scanner.Err())
	require.NotEmpty(t, dsn, ".env.example must set DATABASE_URL")

	t.Chdir(t.TempDir()) // a relative path must stay inside the test folder
	require.NoError(t, Up(dsn))
}
