package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/tui"
)

func TestThinkingFoldSettingReadsTheAskHome(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		perm os.FileMode
		want tui.ThinkingFold
	}{
		{"no file", "", 0, tui.ThinkingExpanded},
		{"no key", `{"defaultModel":"a/b@c"}`, 0o600, tui.ThinkingExpanded},
		{"expanded", `{"thinkingFold":"expanded"}`, 0o600, tui.ThinkingExpanded},
		{"truncated", `{"thinkingFold":"truncated"}`, 0o600, tui.ThinkingTruncated},
		{"collapsed", `{"thinkingFold":"collapsed"}`, 0o600, tui.ThinkingCollapsed},
		{"unknown word", `{"thinkingFold":"sideways"}`, 0o600, tui.ThinkingExpanded},
		{"invalid JSON", `{"thinkingFold":`, 0o600, tui.ThinkingExpanded},
		{"file that others can read", `{"thinkingFold":"collapsed"}`, 0o644, tui.ThinkingExpanded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			require.NoError(t, os.Mkdir(home, 0o700))
			if tc.body != "" {
				path := filepath.Join(home, "settings.json")
				require.NoError(t, os.WriteFile(path, []byte(tc.body), tc.perm))
				require.NoError(t, os.Chmod(path, tc.perm))
			}
			require.Equal(t, tc.want, thinkingFoldSetting(home))
		})
	}
}

func TestThinkingFoldSettingOfAHomeThatCannotBeUsedIsExpanded(t *testing.T) {
	require.Equal(t, tui.ThinkingExpanded, thinkingFoldSetting("relative/home"), "a relative home is refused")

	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.WriteFile(home, nil, 0o600))
	require.Equal(t, tui.ThinkingExpanded, thinkingFoldSetting(home), "a home that is a file is refused")
}
