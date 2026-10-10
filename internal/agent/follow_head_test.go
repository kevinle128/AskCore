package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
)

// A resumed follow keeps the cursor of the caller and names the end of the
// replay in Head. A snapshot has its cursor as the head.
func TestFollowHeadNamesTheEndOfTheReplay(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	first := a.Follow(agent.Cursor{})
	assert.Equal(t, first.Cursor, first.Head, "a snapshot is cut at the head")
	require.NoError(t, a.Prompt(context.Background(), user("a")))
	got := takeReady(t, first)
	require.NotEmpty(t, got)
	cursor := agent.Cursor{Epoch: first.Cursor.Epoch, Seq: got[0].seq}
	first.Events.Close()

	resumed := a.Follow(cursor)
	defer resumed.Events.Close()
	require.True(t, resumed.Resumed)
	assert.Equal(t, cursor, resumed.Cursor, "the cursor of the caller stays")
	assert.Equal(t, first.Cursor.Epoch, resumed.Head.Epoch)
	assert.Equal(t, got[len(got)-1].seq, resumed.Head.Seq, "the head is the last published event")
	assert.Greater(t, resumed.Head.Seq, resumed.Cursor.Seq)
}
