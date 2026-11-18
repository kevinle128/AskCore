package sessions_test

import (
	"regexp"
	"sort"
	"sync"
	"testing"

	"github.com/bwmarrin/snowflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
)

func TestNewIDFormat(t *testing.T) {
	assert.Regexp(t, regexp.MustCompile(`^[1-9][0-9]{18}$`), sessions.NewID())
}

func TestNewIDOrderMatchesStringOrder(t *testing.T) {
	ids := make([]string, 5000)
	for i := range ids {
		ids[i] = sessions.NewID()
	}
	assert.True(t, sort.StringsAreSorted(ids), "ids made in a row sort as strings in creation order")
}

func TestNewIDUniqueAcrossGoroutines(t *testing.T) {
	const workers, each = 8, 2000
	var wg sync.WaitGroup
	out := make([][]string, workers)
	for w := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				out[w] = append(out[w], sessions.NewID())
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, ids := range out {
		for _, id := range ids {
			require.False(t, seen[id], "duplicate id %s", id)
			seen[id] = true
		}
	}
	assert.Len(t, seen, workers*each)
}

func TestNewIDParseRoundTrip(t *testing.T) {
	id := sessions.NewID()
	parsed, err := snowflake.ParseString(id)
	require.NoError(t, err)
	assert.Equal(t, id, parsed.String())
}
