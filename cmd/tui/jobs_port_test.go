package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/jobs"
	"AskCore/pkg/protocol"
)

// steeringSeen returns a listener that closes ch at the first queue_update
// whose steering queue shows text.
func steeringSeen(text string, ch chan struct{}) func(protocol.Event) error {
	var once sync.Once
	return func(ev protocol.Event) error {
		q, ok := ev.(*protocol.QueueUpdate)
		if !ok {
			return nil
		}
		for _, s := range q.Steering {
			if strings.Contains(s, text) {
				once.Do(func() { close(ch) })
			}
		}
		return nil
	}
}

// A job notice now comes through the notice port, so it is not human input:
// Abort keeps it, and the next prompt carries it.
func TestJobNoticeSurvivesAbort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pgid, gate, done := filepath.Join(dir, "pgid"), filepath.Join(dir, "gate"), filepath.Join(dir, "done")
	command := recordGroup(pgid) + "while [ ! -f " + gate + " ]; do sleep 0.05; done; touch " + done
	var ag *agent.Agent
	queued := make(chan struct{})
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": command, "run_in_background": true}),
		sayAfter(func([]byte) {
			// The job ends while this run waits for the model; its notice is
			// queued for the run, then the user aborts the run.
			readPgid(t, pgid)
			assert.NoError(t, os.WriteFile(gate, nil, 0o644))
			waitFile(t, done)
			<-queued
			ag.Abort()
		}, "aborted answer"),
		say("next answered"),
	)
	ag, _ = newTestSession(t, tokenPlanOptions(m), nativeEnv(nil))
	unsubscribe := ag.Subscribe(steeringSeen("Background job bash-1", queued))
	defer unsubscribe()

	promptOnce(t, ag, "start the job")
	require.Equal(t, 2, m.requestCount(), "Abort started no run for the notice")

	promptOnce(t, ag, "next question")
	require.Equal(t, 3, m.requestCount())
	assert.Contains(t, m.userText(2), "Background job bash-1", "the notice rides with the next prompt")
}

// A reset is a new conversation for jobs: old jobs keep running but are
// hidden, and their notices do not reach the new conversation.
func TestResetHidesOldJobsAndDropsTheirNotices(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pgid, gate := filepath.Join(dir, "pgid"), filepath.Join(dir, "gate")
	command := recordGroup(pgid) + "while [ ! -f " + gate + " ]; do sleep 0.05; done"
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": command, "run_in_background": true}),
		say("started"),
		call(t, "job_list", map[string]any{}),
		say("listed"),
		say("after answered"),
	)
	ag, jt := newTestSession(t, tokenPlanOptions(m), nativeEnv(nil))

	promptOnce(t, ag, "start the job")
	readPgid(t, pgid)
	require.NoError(t, ag.Reset())

	promptOnce(t, ag, "list the jobs")
	assert.Contains(t, m.userText(3), "No jobs.", "the new conversation does not see the old job")

	// The old job ends; its notice is refused as stale.
	require.NoError(t, os.WriteFile(gate, nil, 0o644))
	require.Eventually(t, func() bool {
		list := jt.List(0)
		return len(list) == 1 && list[0].Status == jobs.StatusCompleted
	}, e2eWaitFor, 20*time.Millisecond)

	promptOnce(t, ag, "anything new?")
	require.Equal(t, 5, m.requestCount(), "the old notice started no run")
	assert.NotContains(t, m.userText(4), "Background job", "the old notice does not reach the new conversation")
}
