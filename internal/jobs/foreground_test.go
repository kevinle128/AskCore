package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A foreground command that ends before its caller starts to wait is reported
// by that wait, never by a notice: the model already gets the result.
func TestForegroundJobThatEndsBeforeTheWaitSendsNoNotice(t *testing.T) {
	r := newRig(t)
	j, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: "true", Cwd: r.dir, Foreground: true})
	require.NoError(t, err)
	r.track(j)
	<-j.done // the end comes before the wait
	require.Equal(t, Done, r.tb.Wait(context.Background(), j.ID(), time.Second))
	time.Sleep(100 * time.Millisecond) // the owner goroutine sends a notice right after the end, if at all
	require.Empty(t, r.n.followUps(), "no notice for a foreground command")
}

// A foreground command whose wait gives up at once is a background job: its
// end comes as a notice.
func TestForegroundJobWithoutAWaitBudgetStillGetsANotice(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("go")
	j, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: command, Cwd: r.dir, Foreground: true})
	require.NoError(t, err)
	r.track(j)
	require.Equal(t, Background, r.tb.Wait(context.Background(), j.ID(), 0))
	open()
	require.Eventually(t, func() bool { return len(r.n.followUps()) == 1 }, 5*time.Second, 10*time.Millisecond)
}
