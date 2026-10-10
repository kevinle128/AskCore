package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Asynchronous commands.
//
// Commands run outside the update loop. They capture their inputs before
// dispatch and return result messages; they never read or change model state.

// defaultSettleDelay is how long the terminal size must stay the same before
// the scrollback is rebuilt.
const defaultSettleDelay = 120 * time.Millisecond

// settleMsg reports that a size has not changed for the settle delay.
// Generation is the generation of the size event that started the wait.
type settleMsg struct{ generation uint64 }

// commitDoneMsg reports that a commit reached the renderer. Commits are
// sequenced, so the model can start the next output only after this message.
type commitDoneMsg struct{}

// firstFrameDelay is how long the session view waits after the first size
// before it lets the model commit rows. The renderer draws on a 60 Hz timer,
// so a few frame times are enough for the first frame to be on the screen.
const firstFrameDelay = 60 * time.Millisecond

// firstFrameMsg reports that the first frame had time to reach the screen.
type firstFrameMsg struct{}

func firstFrameAfter(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return firstFrameMsg{} })
}

// settleAfter waits for the settle delay and reports the generation.
func settleAfter(delay time.Duration, generation uint64) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return settleMsg{generation: generation} })
}

// commitCmd writes rows above the live region, in order, and then reports
// completion. Println and the completion message are one sequence, so two
// commits never interleave.
func commitCmd(rows []string) tea.Cmd {
	text := strings.Join(rows, "\n")
	if text == "" {
		// The renderer ignores an empty string; one space keeps the blank row.
		text = " "
	}
	return tea.Sequence(tea.Println(text), func() tea.Msg { return commitDoneMsg{} })
}
