package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	updateCatalogEnv  = "ASK_UPDATE_CATALOG"
	catalogSnapshotTo = "catalog_snapshot.json"
)

// TestCatalogSnapshot keeps the embedded models.dev snapshot valid. With
// ASK_UPDATE_CATALOG=1 it downloads the models.dev payload, keeps the Ask
// providers and the fields that the mapper reads, and writes the snapshot with
// its fetch time. Without it, it sends no request: it checks that the
// embedded snapshot parses and maps.
func TestCatalogSnapshot(t *testing.T) {
	if os.Getenv(updateCatalogEnv) == "1" {
		writeCatalogSnapshot(t)
		return
	}
	var meta struct {
		FetchedAt string `json:"fetchedAt"`
		Source    string `json:"source"`
	}
	require.NoError(t, json.Unmarshal(catalogSnapshot, &meta))
	_, err := time.Parse(time.RFC3339, meta.FetchedAt)
	require.NoError(t, err, "the snapshot records its fetch time")
	assert.Equal(t, modelsDevURL, meta.Source)

	rows, err := MapModelsDev(catalogSnapshot)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, m := range rows {
		require.NoError(t, Validate(m), "%s/%s", m.Provider, m.ID)
	}
}

func writeCatalogSnapshot(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// An own transport, closed at the end, so no idle connection outlives the
	// test and trips the goroutine leak check of the package.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	// The snapshot uses the run-time fetch, so both have the same filter and
	// the same body cap.
	payload, _, _, err := FetchModelsDev(ctx, &http.Client{Transport: transport}, "", modelsDevProviders)
	require.NoError(t, err)

	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload, &top))
	require.Len(t, top, len(modelsDevProviders), "every Ask provider is in the payload")
	fetched, err := json.Marshal(time.Now().UTC().Format(time.RFC3339))
	require.NoError(t, err)
	source, err := json.Marshal(modelsDevURL)
	require.NoError(t, err)
	top["fetchedAt"], top["source"] = fetched, source

	out, err := json.MarshalIndent(top, "", "  ")
	require.NoError(t, err)
	out = append(out, '\n')
	rows, err := MapModelsDev(out)
	require.NoError(t, err)
	require.NotEmpty(t, rows, "the new snapshot maps to rows")
	require.NoError(t, os.WriteFile(catalogSnapshotTo, out, 0o644))
	counts := map[string]int{}
	for _, m := range rows {
		counts[m.Provider]++
	}
	t.Logf("wrote %s: %d bytes, generated rows per provider %v", catalogSnapshotTo, len(out), counts)
}
