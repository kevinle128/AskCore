package main

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/leader"
	"AskCore/internal/testsupport"
	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

// Keys as the terminal reports them. The program asks for key disambiguation, so
// the keys that have a plain byte form (ctrl+l, ctrl+t) use it and the others use
// the extended form.
const (
	keyEsc      = "\x1b[27u"
	keyAltEnter = "\x1b[13;3u"
	keyAltUp    = "\x1b[1;3A"
	keyCtrlC    = "\x03"
	keyCtrlD    = "\x04"
	keyCtrlL    = "\x0c"
	keyCtrlT    = "\x14"
)

// hasRow reports whether one row holds every part.
func hasRow(rows []string, parts ...string) bool {
	for _, r := range rows {
		ok := true
		for _, p := range parts {
			if !strings.Contains(r, p) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// hasRowPrefix reports whether a row starts with the prefix. Committed prompt
// bands start with two spaces, and the editor box starts with its border.
func hasRowPrefix(rows []string, prefix string) bool {
	for _, r := range rows {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}

func countRows(rows []string, part string) int {
	n := 0
	for _, r := range rows {
		if strings.Contains(r, part) {
			n++
		}
	}
	return n
}

func (a *askTerm) waitRow(parts ...string) {
	a.t.Helper()
	a.waitFor(strings.Join(parts, " + "), func(rows []string) bool { return hasRow(rows, parts...) })
}

// waitReplyMeta waits for the meta line that ends a finished reply.
func (a *askTerm) waitReplyMeta(model string) {
	a.t.Helper()
	a.waitFor("the meta line of the reply", func(rows []string) bool { return hasRow(rows, model+" · ", "↑") })
}

// quitDetached ends `ask` with ctrl+d on an empty draft.
func (a *askTerm) quitDetached() {
	a.t.Helper()
	a.write(keyCtrlD)
	require.Equal(a.t, 0, a.waitExit(), a.stderr.String())
	a.requireRestored()
}

// selectModel opens the selector, filters it, and chooses the first row.
func (a *askTerm) selectModel(filter, row, footerModel string) {
	a.t.Helper()
	a.write(keyCtrlL)
	a.waitText("Select model")
	a.write(filter)
	a.waitRow("│", row)
	a.write("\r")
	a.waitFor("the footer model", func(rows []string) bool { return hasRow(rows, "leader ", footerModel) })
}

func envPairs(m map[string]string) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// leaderPID returns the process id of the leader of the home, after it checks
// that the process is one that this test started.
func leaderPID(t *testing.T, bin, home string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	st, err := leader.Status(ctx, e2eConnect(home, bin))
	require.NoError(t, err)
	var mine bool
	for _, row := range leaderProcesses(bin) {
		if strings.HasPrefix(row, strconv.Itoa(st.PID)+" ") {
			mine = true
		}
	}
	require.True(t, mine, "pid %d is a leader of this test run", st.PID)
	return st.PID
}

// pauseLeader freezes the leader of the home, so a request to it stays open. The
// returned function resumes it; the cleanup resumes it too, so the leader can
// stop at the end of the test.
func pauseLeader(t *testing.T, bin, home string) (resume func()) {
	t.Helper()
	pid := leaderPID(t, bin, home)
	var resumed atomic.Bool
	resume = func() {
		if resumed.CompareAndSwap(false, true) {
			_ = syscall.Kill(pid, syscall.SIGCONT)
		}
	}
	t.Cleanup(resume)
	require.NoError(t, syscall.Kill(pid, syscall.SIGSTOP))
	return resume
}

// liveSessionIDs lists the sessions of the leader with a short-lived client.
func liveSessionIDs(t *testing.T, bin, home string) []string {
	t.Helper()
	c, _ := openLeaderClient(t, bin, home)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	live, err := c.ListSessions(ctx)
	require.NoError(t, err)
	var ids []string
	for _, l := range live {
		ids = append(ids, l.SessionID)
	}
	return ids
}

const slowPrompt = "slow words go by one at a time "

func TestInteractivePromptStreams(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{}, pacedEnv(8)...)
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)

	// With the leader frozen the prompt cannot be accepted: the draft stays in
	// the editor and the status says so.
	resume := pauseLeader(t, bin, home)
	prompt := strings.Repeat(slowPrompt, 4)
	a.write(prompt + "\r")
	a.waitText("Sending…")
	rows := a.rows()
	require.True(t, hasRowPrefix(rows, "│ ❯ slow words"), "the draft is still in the editor:\n%s", a.screen())
	require.False(t, hasRowPrefix(rows, "  ❯ slow words"), "no prompt band before the leader accepts:\n%s", a.screen())

	resume()
	a.waitFor("the prompt band in scrollback", func(rows []string) bool { return hasRowPrefix(rows, "  ❯ slow words") })
	a.waitFor("the draft cleared", func(rows []string) bool { return hasRow(rows, "│ ❯ ", "Ask anything") })
	a.waitFor("the status with elapsed seconds", func(rows []string) bool {
		for _, r := range rows {
			if regexp.MustCompile(`Working… \d+s`).MatchString(r) {
				return true
			}
		}
		return false
	})
	a.waitFor("the reply streaming", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ slow words") })
	require.True(t, containsRow(a.rows(), "Working…"), "the reply is not finished yet:\n%s", a.screen())

	a.waitReplyMeta("faux-1")
	a.waitFor("the status gone", func(rows []string) bool { return !containsRow(rows, "Working…") })
	requireLeaderFooter(t, a, bin, home)
	a.quitDetached()
}

func TestInteractiveRefusedPromptKeepsDraft(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{})
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)

	// A second client attaches and takes the session. The first screen does not
	// know yet, so its next prompt is refused by the leader.
	other, _ := openLeaderClient(t, bin, home)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	live, err := other.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	info, err := other.Attach(ctx, live[0].SessionID)
	require.NoError(t, err)
	require.Equal(t, protocol.ACPRoleObserver, info.Role)
	_, err = other.Take(ctx)
	require.NoError(t, err)

	a.write("show me the probe code\r")
	a.waitRow("✗ Not sent: not_driver", "draft kept", "/take to drive")
	rows := a.rows()
	require.True(t, hasRowPrefix(rows, "│ ❯ show me the probe code"), "the draft stays in the editor:\n%s", a.screen())
	require.False(t, hasRowPrefix(rows, "  ❯ show me the probe code"), "the refused prompt is not in the scrollback:\n%s", a.screen())
	require.False(t, containsRow(rows, "Working…"))
	st, err := other.State(ctx)
	require.NoError(t, err)
	require.Zero(t, st.MessageCount, "the leader ran nothing")
	requireLeaderFooter(t, a, bin, home)
	a.write(keyCtrlC) // clears the draft
	a.waitText("Ask anything")
	a.quitDetached()
}

func TestInteractiveRefusedPromptDuringAnotherRun(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{}, pacedEnv(8)...)
	a.waitText("Ask anything")
	other, col := openLeaderClient(t, bin, home)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	var live []protocol.ACPLiveSession
	require.Eventually(t, func() bool {
		live, _ = other.ListSessions(ctx)
		return len(live) == 1
	}, exitWait, 50*time.Millisecond)
	_, err := other.Attach(ctx, live[0].SessionID)
	require.NoError(t, err)
	_, err = other.Take(ctx)
	require.NoError(t, err)
	resume := pauseLeader(t, bin, home)
	id, err := other.Prompt(strings.Repeat("other reply words ", 40))
	require.NoError(t, err)
	// The new driver writes first while the old driver still believes it owns
	// the session. Its start event must not accept the old driver's submission.
	time.Sleep(100 * time.Millisecond)
	a.write("keep this refused draft\r")
	a.waitText("Sending…")
	resume()
	col.waitFor("driver prompt accepted", func(m tui.Message) bool { p, ok := m.(tui.PromptAcceptedMsg); return ok && p.ID == id })
	a.waitRow("✗ Not sent: not_driver", "draft kept")
	a.waitRow("│ ❯ keep this refused draft")
	require.NoError(t, other.Cancel())
	a.write(keyCtrlC)
	a.waitText("Ask anything")
	a.quitDetached()
}

func TestInteractiveEchoToolLine(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{})
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)

	a.write("echo tool output words\r")
	// The echo tool returns at once, so the running line lasts less than a frame
	// and only the committed line is a stable thing to see.
	a.waitFor("the committed tool line", func(rows []string) bool { return hasRow(rows, "● echo", "✓") })
	a.waitFor("the reply after the tool", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ tool output words") })
	a.waitReplyMeta("faux-1")
	rows := a.rows()
	require.Equal(t, 1, countRows(rows, "● echo"), "one tool line:\n%s", a.screen())
	require.False(t, containsRow(rows, "◇ echo"), "no running line stays after the tool ended:\n%s", a.screen())
	a.quitDetached()
}

func TestInteractiveQueueRoundTrip(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{}, pacedEnv(5)...)
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)

	a.write(strings.Repeat(slowPrompt, 6) + "\r")
	a.waitText("Working…")
	a.write("use the fake clock\r") // steer
	a.waitRow("┆ ⧗ steer", "use the fake clock")
	a.write("then run the tests" + keyAltEnter) // follow-up
	a.waitRow("┆ ⧗ next", "then run the tests")
	a.waitRow("alt+↑ edit queue")
	require.True(t, containsRow(a.rows(), "Working…"))

	a.write(keyAltUp)
	a.waitFor("both texts in the draft and an empty queue", func(rows []string) bool {
		return hasRow(rows, "│", "use the fake clock") && hasRow(rows, "│", "then run the tests") &&
			!containsRow(rows, "⧗") && !containsRow(rows, "alt+↑ edit queue")
	})
	// The leader queue is empty too: nothing is sent when the run ends.
	ids := liveSessionIDs(t, bin, home)
	require.Len(t, ids, 1)
	c, _ := openLeaderClient(t, bin, home)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	_, err := c.Attach(ctx, ids[0])
	require.NoError(t, err)
	st, err := c.State(ctx)
	require.NoError(t, err)
	require.Empty(t, st.Steering)
	require.Empty(t, st.FollowUp)

	a.write(keyEsc)
	a.waitText("⊘ Cancelled by you.")
}

func TestInteractiveCancelThenPrompt(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{}, pacedEnv(8)...)
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)

	a.write(strings.Repeat(slowPrompt, 6) + "\r")
	a.waitFor("a partial reply", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ slow words") })

	// The frozen leader cannot settle the run, so the screen shows the wait
	// state and no cancel marker yet.
	resume := pauseLeader(t, bin, home)
	a.write(keyEsc)
	a.waitText("Cancelling…")
	require.False(t, containsRow(a.rows(), "⊘"), "no marker before the run settles:\n%s", a.screen())
	resume()

	var at []string
	a.waitFor("the cancel marker", func(rows []string) bool {
		if containsRow(rows, "⊘ Cancelled by you.") {
			at = rows
			return true
		}
		return false
	})
	require.False(t, containsRow(at, "Working…") || containsRow(at, "Cancelling…"), "the marker comes with the settle:\n%s", strings.Join(at, "\n"))
	require.True(t, containsRow(at, "Partial reply kept above."), "the marker text:\n%s", strings.Join(at, "\n"))
	require.True(t, hasRowPrefix(at, "  ◆ slow words"), "the partial reply stays:\n%s", strings.Join(at, "\n"))

	// The next prompt runs normally.
	a.write("hello again\r")
	a.waitFor("the second reply", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ hello again") })
	a.waitFor("the second meta line", func(rows []string) bool { return countRows(rows, "faux-1 · ") == 1 && hasRow(rows, "◆ hello again") })
	a.quitDetached()
}

const sonnetFooter = "claude-sonnet-4-6"

func TestInteractiveModelSwitch(t *testing.T) {
	bin, home := newE2EHome(t)
	// The key is a placeholder: it makes the provider ready, and no request is made.
	a := startAskTerm(t, bin, home, askTermOptions{}, "ANTHROPIC_API_KEY=sk-ant-placeholder-for-readiness")
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)
	// The model name arrives with the state answer, after the leader id.
	a.waitFor("the start model in the footer", func(rows []string) bool { return hasRow(rows, "leader ", "faux-1") })

	a.write("keep this draft")
	a.write(keyCtrlL)
	a.waitText("Select model")
	a.waitText("your draft is kept")
	require.False(t, hasRowPrefix(a.rows(), "│ ❯ keep this draft"), "the selector replaces the editor:\n%s", a.screen())
	// One model that the catalog lists on two wire formats has two rows that
	// the user can tell apart.
	a.write("deepseek")
	a.waitRow("│", "(anthropic-mess")
	a.waitRow("│", "(openai-complet")
	require.Equal(t, 2, countRows(a.rows(), "alibaba-token-plan/deepseek-v4.1-flash"), a.screen())
	a.write("\x15") // ctrl+u clears the filter
	a.write("anthropic/" + sonnetFooter)
	a.waitRow("│", "anthropic/"+sonnetFooter)
	a.write("\r")
	a.waitFor("the new model in the footer", func(rows []string) bool { return hasRow(rows, "leader ", sonnetFooter) })
	a.waitFor("the editor back with the draft", func(rows []string) bool { return hasRowPrefix(rows, "│ ❯ keep this draft") })
	require.False(t, containsRow(a.rows(), "Select model"))
	require.False(t, hasRow(a.rows(), "leader ", "faux-1"), "the old model left the footer:\n%s", a.screen())
	requireLeaderFooter(t, a, bin, home)

	a.write(keyCtrlC) // clears the draft
	a.waitText("Ask anything")
	a.quitDetached()
}

// retryFixture answers the first model request with a 429 that asks for a wait of
// three seconds, and every later request with a reply.
func retryFixture(t *testing.T) *providerFixture {
	t.Helper()
	f := newProviderFixture(t)
	f.retryAfter = "3"
	f.inference = func(n int, _ *http.Request, _ []byte) (int, string, string) {
		if n == 0 {
			return http.StatusTooManyRequests, "application/json", `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`
		}
		return http.StatusOK, "text/event-stream", sseText("pong from the fixture")
	}
	return f
}

func TestInteractiveRetryVisible(t *testing.T) {
	bin, home := newE2EHome(t)
	f := retryFixture(t)
	env := append(envPairs(f.env()), "ANTHROPIC_API_KEY=sk-ant-placeholder-for-fixture")
	a := startAskTerm(t, bin, home, askTermOptions{}, env...)
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)
	a.selectModel("anthropic/"+sonnetFooter, "anthropic/"+sonnetFooter, sonnetFooter)
	require.Zero(t, f.calls(), "choosing the model sends no request")

	a.write("ping the provider\r")
	a.waitFor("the retry status", func(rows []string) bool {
		for _, r := range rows {
			if regexp.MustCompile(`Retrying \(1/\d+\) in \d+s`).MatchString(r) {
				return true
			}
		}
		return false
	})
	a.waitRow("↻ Attempt 1 failed", "rate limit")
	a.waitFor("the reply after the retry", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ pong from the fixture") })
	a.waitFor("the status gone", func(rows []string) bool { return !containsRow(rows, "Retrying") && !containsRow(rows, "Working…") })
	require.Equal(t, 2, f.calls(), "one failed request and one that answered")
	require.Equal(t, 1, countRows(a.rows(), "↻ Attempt 1 failed"))
	a.quitDetached()
}

// sseThinking is an Anthropic Messages stream with a thinking block and then a
// text block.
func sseThinking(thinking, text string) string {
	return `event:message_start
data:{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}

event:content_block_start
data:{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}

event:content_block_delta
data:{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"` + thinking + `"}}

event:content_block_delta
data:{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"c2lnbmF0dXJl"}}

event:content_block_stop
data:{"type":"content_block_stop","index":0}

event:content_block_start
data:{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event:content_block_delta
data:{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"` + text + `"}}

event:content_block_stop
data:{"type":"content_block_stop","index":1}

event:message_delta
data:{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":9}}

event:message_stop
data:{"type":"message_stop"}

`
}

func TestInteractiveThinkingToggle(t *testing.T) {
	bin, home := newE2EHome(t)
	const thought = "Check the stale socket before the deadline."
	f := newProviderFixture(t)
	f.inference = func(int, *http.Request, []byte) (int, string, string) {
		return http.StatusOK, "text/event-stream", sseThinking(thought, "The probe is slow.")
	}
	env := append(envPairs(f.env()), "ANTHROPIC_API_KEY=sk-ant-placeholder-for-fixture")
	a := startAskTerm(t, bin, home, askTermOptions{}, env...)
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)
	a.selectModel("anthropic/"+sonnetFooter, "anthropic/"+sonnetFooter, sonnetFooter)

	a.write("why is the probe slow\r")
	a.waitFor("the thinking text", func(rows []string) bool { return hasRow(rows, "∴ "+thought) })
	a.waitFor("the reply", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ The probe is slow.") })
	a.waitReplyMeta(sonnetFooter)

	a.write(keyCtrlT)
	a.waitFor("hidden thinking", func(rows []string) bool {
		return hasRow(rows, "∴ Thinking...") && !containsRow(rows, thought) && containsRow(rows, "Thinking blocks: hidden")
	})
	require.True(t, hasRowPrefix(a.rows(), "  ◆ The probe is slow."), "the reply stays:\n%s", a.screen())

	a.write(keyCtrlT)
	a.waitFor("shown thinking", func(rows []string) bool {
		return hasRow(rows, "∴ "+thought) && !containsRow(rows, "∴ Thinking...")
	})
	a.waitText("Thinking blocks: visible")
	a.quitDetached()
}

func TestInteractiveAuthErrorBlock(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	bin, home := newE2EHome(t)
	var refuse atomic.Bool
	f := newProviderFixture(t)
	f.token = func(grant map[string]string) (int, any) {
		switch grant["grant_type"] {
		case "authorization_code":
			tokens := oauthTokens()
			// A lifetime under the refresh lead makes every use refresh the token.
			tokens["expires_in"] = 60
			return http.StatusOK, tokens
		case "refresh_token":
			if refuse.Load() {
				return http.StatusBadRequest, map[string]any{"error": "invalid_grant"}
			}
			return http.StatusOK, map[string]any{"access_token": "rotated-access-canary", "refresh_token": "rotated-refresh-canary", "expires_in": 60, "token_type": "Bearer", "scope": "user:inference"}
		}
		return http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"}
	}
	f.inference = func(int, *http.Request, []byte) (int, string, string) {
		return http.StatusOK, "text/event-stream", pongSSE()
	}
	login := startProductionOAuth(t, home, f.env())
	completeOAuth(t, login, false)
	login.finish(t, true)

	// No API key in the environment: the saved sign-in is the credential.
	a := startAskTerm(t, bin, home, askTermOptions{}, envPairs(f.env())...)
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)
	a.selectModel("anthropic/"+sonnetFooter, "anthropic/"+sonnetFooter, sonnetFooter)

	// From now on the token service refuses the refresh, as it does for a
	// token that expired and cannot be renewed.
	refuse.Store(true)
	a.write("ping with an expired sign-in\r")
	a.waitRow("✗ Error (auth)")
	a.waitRow("Run in a shell: ask auth login --provider anthropic. Then send again.")
	a.waitFor("the run ended", func(rows []string) bool { return !containsRow(rows, "Working…") })
	require.True(t, hasRowPrefix(a.rows(), "  ❯ ping with an expired sign-in"), "the prompt was accepted before the run failed:\n%s", a.screen())
	require.Zero(t, f.calls(), "the model service never saw a request")
	for _, canary := range []string{"private-access-canary", "private-refresh-canary", "rotated-access-canary", "rotated-refresh-canary"} {
		require.NotContains(t, a.rawOutput(), canary)
	}
	a.quitDetached()
}

// withHomeAway moves the home of the test away and puts a link in its place, so
// every attempt to reach or start a leader fails. restore undoes it. The running
// leader keeps its files, because the move keeps them.
func withHomeAway(t *testing.T, home string) (restore func()) {
	t.Helper()
	away := home + ".away"
	require.NoError(t, os.Rename(home, away))
	require.NoError(t, os.Symlink(t.TempDir(), home))
	var done atomic.Bool
	restore = func() {
		if done.CompareAndSwap(false, true) {
			_ = os.Remove(home)
			_ = os.Rename(away, home)
		}
	}
	t.Cleanup(restore)
	return restore
}

func TestInteractiveLeaderDownShowsDisconnected(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{})
	a.waitText("Ask anything")
	requireLeaderFooter(t, a, bin, home)
	a.write("first words\r")
	a.waitReplyMeta("faux-1")
	a.write("also check the lock timeout")
	a.waitRow("│ ❯ also check the lock timeout")

	// The home is unusable before the leader stops, so the client cannot start
	// a new leader while the screen is checked.
	pid := leaderPID(t, bin, home)
	restore := withHomeAway(t, home)
	require.NoError(t, syscall.Kill(pid, syscall.SIGTERM))
	require.Eventually(t, func() bool { return len(leaderProcesses(bin)) == 0 }, exitWait, 50*time.Millisecond, "the leader stopped")

	a.waitRow("✗ Disconnected from the leader", "reconnecting (attempt")
	a.waitRow("leader lost")
	a.waitRow("draft kept · sending waits until the leader is back · nothing is resent")
	a.write("\r")
	a.waitRow("Reconnecting, please wait…")
	rows := a.rows()
	require.True(t, hasRowPrefix(rows, "│ ❯ also check the lock timeout"), "the draft is kept:\n%s", a.screen())
	require.False(t, hasRowPrefix(rows, "  ❯ also check the lock timeout"), "nothing was sent:\n%s", a.screen())
	// The client keeps trying and the leader stays down.
	a.waitFor("a later attempt", func(rows []string) bool {
		return hasRow(rows, "reconnecting (attempt 3") || hasRow(rows, "reconnecting (attempt 4")
	})
	require.Empty(t, leaderProcesses(bin), "no leader runs while the screen is checked")

	// Only now may a leader start again.
	restore()
	a.waitFor("the session is gone after the restart", func(rows []string) bool {
		return hasRow(rows, "is gone: the leader restarted.") && !containsRow(rows, "✗ Disconnected from the leader")
	})
	require.Len(t, leaderProcesses(bin), 1)
	a.write(keyCtrlC)
	a.waitText("Ask anything")
	a.quitDetached()
}

func TestInteractiveSessionGone(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{})
	a.waitText("Ask anything")
	oldLeader := requireLeaderFooter(t, a, bin, home)
	a.write("remember this\r")
	a.waitReplyMeta("faux-1")
	a.write("also check the lock timeout")
	a.waitRow("│ ❯ also check the lock timeout")

	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	res, err := leader.Stop(ctx, e2eConnect(home, bin), 20*time.Second)
	require.NoError(t, err)
	require.True(t, res.WasRunning)

	// The client starts a new leader while it reconnects.
	gone := regexp.MustCompile(`Session \S+ is gone: the leader restarted\.`)
	a.waitFor("the session is gone", func(rows []string) bool {
		for _, r := range rows {
			if gone.MatchString(r) {
				return true
			}
		}
		return false
	})
	a.waitRow("The transcript above is kept. Your next prompt starts a new session.")
	st, err := leader.Status(ctx, e2eConnect(home, bin))
	require.NoError(t, err)
	require.NotEqual(t, oldLeader, st.InstanceID, "a new leader runs")
	a.waitFor("the new leader in the footer", func(rows []string) bool { return hasRow(rows, "leader "+tui.ShortID(st.InstanceID)) })
	require.Zero(t, st.Sessions, "the new leader holds no session yet")
	rows := a.rows()
	require.Equal(t, 1, countRows(rows, "❯ remember this"), "the old transcript is kept once:\n%s", a.screen())
	require.True(t, hasRowPrefix(rows, "│ ❯ also check the lock timeout"), "the draft is kept:\n%s", a.screen())

	// The next prompt starts a new session, and the old prompt is not sent again.
	a.write("\r")
	a.waitFor("the reply in the new session", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ also check the lock timeout") })
	a.waitReplyMeta("faux-1")
	ids := liveSessionIDs(t, bin, home)
	require.Len(t, ids, 1)
	c, _ := openLeaderClient(t, bin, home)
	_, err = c.Attach(ctx, ids[0])
	require.NoError(t, err)
	state, err := c.State(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, state.MessageCount, "the new session holds only the new prompt and its reply")
	require.Equal(t, 1, countRows(a.rows(), "❯ remember this"), "the old prompt is not sent again")
	a.quitDetached()
}

// watchingPair starts a first `ask` with a long run and a second `ask` that
// chooses the session of the first from /sessions. The second screen is the
// observer. It returns both screens and the short id of the watched session.
func watchingPair(t *testing.T, bin, home string) (driver, observer *askTerm, short string) {
	t.Helper()
	driver = startAskTerm(t, bin, home, askTermOptions{}, pacedEnv(8)...)
	driver.waitText("Ask anything")
	requireLeaderFooter(t, driver, bin, home)
	// The run must outlast the start of the second screen.
	driver.write(strings.Repeat(slowPrompt, 16) + "\r")
	driver.waitText("Working…")
	ids := liveSessionIDs(t, bin, home)
	require.Len(t, ids, 1)
	short = tui.ShortID(ids[0])

	observer = startAskTerm(t, bin, home, askTermOptions{})
	observer.waitText("Ask anything")
	requireLeaderFooter(t, observer, bin, home)
	observer.write("/sessions\r")
	observer.waitText("Live sessions")
	return driver, observer, short
}

func TestInteractiveSecondClientObserves(t *testing.T) {
	bin, home := newE2EHome(t)
	driver, observer, short := watchingPair(t, bin, home)
	observer.write(short)
	observer.waitRow("│", short, "driver busy")
	observer.write("\r")

	observer.waitRow("◉ observer " + short)
	observer.waitRow("◉ Watching. Type /take to take control when the run ends.")
	observer.waitRow("Working…", "another client drives this session")
	require.False(t, containsRow(observer.rows(), "Ask anything"), "the editor slot shows the watching text:\n%s", observer.screen())

	// When the run ends the observer takes control and gets its editor back.
	driver.waitReplyMeta("faux-1")
	observer.waitFor("the run ended on the observer", func(rows []string) bool { return !containsRow(rows, "Working…") })
	observer.write("/take\r")
	observer.waitFor("the editor and the leader footer", func(rows []string) bool {
		return hasRow(rows, "Ask anything") && hasRow(rows, "leader ") && !containsRow(rows, "◉ observer")
	})
	observer.quitDetached()
	driver.quitDetached()
}

func TestInteractiveSessionsAttach(t *testing.T) {
	bin, home := newE2EHome(t)
	driver, observer, short := watchingPair(t, bin, home)
	// The list holds both sessions: the own one and the one that another client drives.
	observer.waitRow("this client drives")
	observer.waitRow("driver busy")
	observer.write(short)
	observer.waitRow("│", short, "driver busy")
	observer.write("\r")

	observer.waitRow("── session " + short + " ──")
	observer.waitRow("◉ observer " + short)
	// The followed reply streams in the transcript of the observer.
	observer.waitFor("the followed reply", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ slow words") })
	require.True(t, containsRow(observer.rows(), "Working…"), "the run goes on:\n%s", observer.screen())
	driver.waitReplyMeta("faux-1")
	observer.waitReplyMeta("faux-1")
	observer.quitDetached()
	driver.quitDetached()
}

func TestInteractiveSwitchFromRunningSession(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{}, pacedEnv(8)...)
	a.waitText("Ask anything")
	var ids []string
	require.Eventually(t, func() bool {
		ids = liveSessionIDs(t, bin, home)
		return len(ids) == 1
	}, exitWait, 50*time.Millisecond)
	old, _ := openLeaderClient(t, bin, home)
	ctx, cancel := context.WithTimeout(context.Background(), exitWait)
	defer cancel()
	_, err := old.Attach(ctx, ids[0])
	require.NoError(t, err)
	a.write(strings.Repeat("old reply words ", 16) + "\r")
	a.waitText("Working…")
	a.waitFor("the old reply streaming", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ old reply") })

	b := startAskTerm(t, bin, home, askTermOptions{})
	b.waitText("Ask anything")
	require.Eventually(t, func() bool { return len(liveSessionIDs(t, bin, home)) == 2 }, exitWait, 50*time.Millisecond)
	b.write(strings.Repeat("new reply words ", 80) + "\r")
	b.waitText("Working…")
	live := liveSessionIDs(t, bin, home)
	var target string
	for _, id := range live {
		if id != ids[0] {
			target = id
		}
	}
	require.NotEmpty(t, target)
	a.write("/sessions\r")
	a.waitText("Live sessions")
	a.write(tui.ShortID(target))
	a.waitRow("│", tui.ShortID(target), "driver busy")
	a.write("\r")
	a.waitRow("◉ observer " + tui.ShortID(target))
	a.waitRow("Working…", "another client drives this session")
	require.Eventually(t, func() bool {
		state, err := old.State(ctx)
		return err == nil && !state.Running
	}, exitWait, 50*time.Millisecond, "the old run settled")
	// Let the old prompt response and another frame of the watched run arrive.
	time.Sleep(300 * time.Millisecond)
	require.True(t, containsRow(a.rows(), "Working…"), "the old prompt must not settle the watched run:\n%s", a.screen())

	b.write(keyEsc)
	b.waitText("Cancelled by you")
	a.waitText("Cancelled by you")
	a.waitFor("the watched run settled", func(rows []string) bool { return !containsRow(rows, "Working…") })
	// A frozen old tail must not keep both sessions in the live region forever.
	a.write("/take\r")
	a.waitText("You drive the session now.")
	a.write("latest reply\r")
	a.waitFor("the latest reply", func(rows []string) bool { return hasRowPrefix(rows, "  ◆ latest reply") })
	a.quitDetached()
	b.quitDetached()
}
