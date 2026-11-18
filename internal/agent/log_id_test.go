package agent

import (
	"testing"

	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"

	"github.com/stretchr/testify/require"
)

type namedLog struct {
	*sessions.MemoryLog
	id string
}

func (l namedLog) ID() string { return l.id }

// The log ID follows a reset, and a log with no ID gives an empty one.
func TestLogIDFollowsReset(t *testing.T) {
	fp, err := faux.New()
	require.NoError(t, err)
	model, _ := fp.Model("faux-1")
	loop := LoopConfig{Model: model, Stream: fp.Stream, Cwd: t.TempDir()}
	ids := []string{"log-1", "log-2"}
	a, err := New(Config{SessionID: "s", LoopConfig: loop, NewContext: func() (sessions.Writer, error) {
		id := ids[0]
		ids = ids[1:]
		return namedLog{&sessions.MemoryLog{}, id}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.Dispose() })
	require.Equal(t, "log-1", a.LogID())
	require.NoError(t, a.Reset())
	require.Equal(t, "log-2", a.LogID())

	plain, err := New(Config{SessionID: "p", LoopConfig: loop})
	require.NoError(t, err)
	t.Cleanup(func() { _ = plain.Dispose() })
	require.Empty(t, plain.LogID(), "a log in memory has no ID")
}
