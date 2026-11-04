package bootstrap

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFit(t *testing.T) {
	t.Parallel()
	mk := func(path string, n int) loaded {
		return loaded{path: path, text: strings.Repeat("a", n), bytes: n}
	}
	tests := []struct {
		name      string
		files     []loaded
		max       int
		wantPaths []string
		wantSkips []SkipReason
	}{
		{name: "fits", files: []loaded{mk("/a", 10), mk("/b", 10)}, max: 1000, wantPaths: []string{"/a", "/b"}},
		{name: "drops the broadest first", files: []loaded{mk("/a", 400), mk("/b", 400), mk("/c", 400)}, max: 1300, wantPaths: []string{"/b", "/c"}, wantSkips: []SkipReason{SkipBudget}},
		{name: "truncates the most specific", files: []loaded{mk("/a", 400), mk("/b", 2000)}, max: 1000, wantPaths: []string{"/b"}, wantSkips: []SkipReason{SkipBudget, SkipTruncated}},
		{name: "nothing fits", files: []loaded{mk("/a", 10)}, max: 10, wantPaths: nil, wantSkips: []SkipReason{SkipBudget}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kept, skips := fit(tt.files, tt.max)

			var gotPaths []string
			for _, f := range kept {
				gotPaths = append(gotPaths, f.path)
			}
			var gotSkips []SkipReason
			for _, s := range skips {
				gotSkips = append(gotSkips, s.Reason)
			}
			assert.Equal(t, tt.wantPaths, gotPaths)
			assert.Equal(t, tt.wantSkips, gotSkips)
			if len(kept) > 0 {
				assert.LessOrEqual(t, build(kept, skips).Len(), tt.max)
			}
		})
	}
}

func TestFit_TruncationIsTightAndOnRuneBoundary(t *testing.T) {
	t.Parallel()
	for _, unit := range []string{"a", "é", "€", "😀"} {
		text := strings.Repeat(unit, 5000)
		kept, skips := fit([]loaded{{path: "/p", text: text, bytes: len(text)}}, 4000)

		require.Len(t, kept, 1)
		assert.True(t, utf8.ValidString(kept[0].text), unit)
		assert.LessOrEqual(t, build(kept, skips).Len(), 4000, unit)
		longer := loaded{path: "/p", text: text[:len(kept[0].text)+len(unit)]}
		assert.Greater(t, build([]loaded{longer}, skips).Len(), 4000, "one more rune must not fit: "+unit)
	}
}
