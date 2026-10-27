package agent_test

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/faux"
)

// TestSynctestSmoke proves that an Agent, the faux provider, and a prompt
// settle inside a synctest bubble. The tests that need a fake clock depend on
// this.
func TestSynctestSmoke(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("done"))
		a := newAgent(t, p, m, nil)
		require.NoError(t, a.Prompt(context.Background(), user("hi")))
		require.NoError(t, a.WaitForIdle(context.Background()))
		require.NoError(t, a.Dispose())
	})
}
