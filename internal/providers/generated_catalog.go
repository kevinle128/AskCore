package providers

import (
	_ "embed"
	"fmt"
	"slices"
)

// catalogSnapshot is the models.dev subset that TestCatalogSnapshot writes
// with ASK_UPDATE_CATALOG=1. Its top-level fetchedAt field records the fetch
// time.
//
//go:embed catalog_snapshot.json
var catalogSnapshot []byte

// GeneratedCatalog returns the rows mapped from the embedded models.dev
// snapshot, merged with the hand-written rows of the compiled catalog. A
// hand-written row wins over a generated row with the same provider, id and
// API. One field is the exception: a hand-written row with no thinking map
// takes the map of that generated row. Without a map the Responses adapter
// sends no effort word, so the thinking level of the row would have no effect
// on the wire. The rows are sorted by provider, id and API, and each call
// returns independent rows.
//
// The compiled catalog functions (AvailableModels, Find, RecommendedModel)
// keep their contract and do not use these rows. The runtime model list
// merges them in NewModelSet.
func GeneratedCatalog() []Model {
	generated, err := MapModelsDev(catalogSnapshot)
	if err != nil {
		// The snapshot is part of the binary and TestCatalogSnapshot maps it,
		// so this is a defect of the release, not a run-time condition.
		panic(fmt.Sprintf("embedded model catalog snapshot: %v", err))
	}
	out := catalog()
	hand := len(out)
	for _, m := range generated {
		i := slices.IndexFunc(out[:hand], func(h Model) bool {
			return h.Provider == m.Provider && h.ID == m.ID && h.API == m.API
		})
		switch {
		case i < 0:
			out = append(out, m)
		case out[i].ThinkingLevelMap == nil:
			out[i].ThinkingLevelMap = m.ThinkingLevelMap
		}
	}
	slices.SortFunc(out, compareModels)
	return out
}
