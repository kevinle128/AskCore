package main

import (
	"context"
	"time"

	"AskCore/internal/settings"
	"AskCore/internal/tui"
)

// settingsReadWait bounds the read of settings.json at the start of the TUI,
// so that a lock that another process holds cannot delay the first frame.
const settingsReadWait = 500 * time.Millisecond

// thinkingFoldSetting returns the thinkingFold setting of the Ask home: the
// mode that a thinking step takes when it ends. An empty home uses ASK_HOME
// or ~/.ask. A home or a file that cannot be read gives the default and no
// notice, so that the TUI starts as it does without the setting.
// The user file gives the first frame only: a trusted project can override
// the value, and the session state then gives the effective value.
func thinkingFoldSetting(askHome string) tui.ThinkingFold {
	store, err := settings.NewSettingsStore(askHome)
	if err != nil {
		return tui.ThinkingExpanded
	}
	ctx, cancel := context.WithTimeout(context.Background(), settingsReadWait)
	defer cancel()
	// The store gives the default with each error. The TUI shows no notice
	// for a setting that cannot be read.
	fold, _ := store.ThinkingFold(ctx)
	return tuiThinkingFold(fold)
}

// tuiThinkingFold maps a value of the setting to the value of the TUI.
func tuiThinkingFold(f settings.ThinkingFold) tui.ThinkingFold {
	switch f {
	case settings.ThinkingFoldTruncated:
		return tui.ThinkingTruncated
	case settings.ThinkingFoldCollapsed:
		return tui.ThinkingCollapsed
	}
	return tui.ThinkingExpanded
}
