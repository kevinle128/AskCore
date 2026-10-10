package tui_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
	"github.com/stretchr/testify/require"
)

// lockedBuffer is a writer that the renderer goroutine and the test share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// replayProbe asks for one inline replay as soon as it learns the window size.
// It uses the generation of that size, as an application must.
type replayProbe struct {
	entries []string
	result  chan tea.InlineReplayResult
}

func (m replayProbe) Init() tea.Cmd { return nil }

func (m replayProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, tea.ReplayInlineTranscript(msg.Width, msg.Height, msg.Generation, m.entries)
	case tea.InlineReplayResult:
		m.result <- msg
		return m, tea.Quit
	}
	return m, nil
}

func (m replayProbe) View() tea.View { return tea.NewView("live editor") }

// The renderer patch adds ReplayInlineTranscript. One replay must purge the
// screen and scrollback and draw every committed line once. The test reads the bytes through a terminal
// emulator, as a real terminal would show them.
func TestInlineReplayPatchPresent(t *testing.T) {
	const width, height = 40, 12
	entries := []string{"startup line", "committed one", "committed two"}
	out := &lockedBuffer{}
	probe := replayProbe{entries: entries, result: make(chan tea.InlineReplayResult, 1)}

	p := tea.NewProgram(probe,
		tea.WithInput(nil),
		tea.WithOutput(out),
		tea.WithWindowSize(width, height),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}),
		tea.WithoutSignalHandler(),
	)
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()

	select {
	case res := <-probe.result:
		require.NoError(t, res.Err)
		require.False(t, res.Stale, "the generation of the latest size must be current")
		require.Equal(t, len(entries), res.Entries)
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("no replay result")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("program did not stop")
	}

	// The purge clears the screen and the scrollback in the same write that
	// draws the lines again.
	require.Equal(t, 1, strings.Count(out.String(), "\x1b[2J\x1b[3J\x1b[H"), "one purge for one replay")

	// The program clears its live view when it stops, so only the committed
	// lines remain on the screen.
	emu := vt.NewEmulator(width, height)
	defer func() { require.NoError(t, emu.Close()) }()
	_, err := emu.Write([]byte(out.String()))
	require.NoError(t, err)
	screen := emu.String()
	for _, line := range entries {
		require.Equal(t, 1, strings.Count(screen, line), "%q must show once in:\n%s", line, screen)
	}
}
