package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// logged returns the entries of type T in log order.
func logged[T sessions.Entry](log sessions.Writer) []T {
	var out []T
	for _, e := range log.Entries() {
		if v, ok := e.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// withLog makes the Agent write to log.
func withLog(log sessions.Writer) func(*agent.Config) {
	return func(c *agent.Config) { c.NewContext = func() sessions.Writer { return log } }
}

// edits chains config edits.
func edits(fs ...func(*agent.Config)) func(*agent.Config) {
	return func(c *agent.Config) {
		for _, f := range fs {
			f(c)
		}
	}
}

// spyStream calls inspect with every request before the provider sees it.
func spyStream(p *faux.Provider, inspect func(providers.TranscriptRequest)) func(*agent.Config) {
	return func(c *agent.Config) {
		c.Stream = func(ctx context.Context, m providers.Model, req providers.TranscriptRequest, o providers.StreamOptions) *providers.Stream {
			inspect(req)
			return p.Stream(ctx, m, req, o)
		}
	}
}

func userTexts(msgs []protocol.Message) []string {
	var out []string
	for _, m := range msgs {
		if u, ok := m.(protocol.UserMessage); ok {
			for _, b := range u.Content {
				if t, ok := b.(protocol.Text); ok {
					out = append(out, t.Text)
				}
			}
		}
	}
	return out
}

func TestPromptIsCommittedBeforeFirstRequest(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	log := &sessions.MemoryLog{}
	var inLog [][]string
	a := newAgent(t, p, m, edits(withLog(log), spyStream(p, func(providers.TranscriptRequest) {
		inLog = append(inLog, userTexts(log.Messages()))
	})))

	require.NoError(t, a.Prompt(context.Background(), user("first prompt")))
	assert.Equal(t, [][]string{{"first prompt"}}, inLog, "the prompt is in the log when the first request goes out")
}

func TestSteeringMessageIsCommittedBeforeNextRequest(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	log := &sessions.MemoryLog{}
	var inLog [][]string
	a := newAgent(t, p, m, edits(withLog(log),
		spyStream(p, func(providers.TranscriptRequest) { inLog = append(inLog, userTexts(log.Messages())) })))
	a.Subscribe(onEvent("turn_start", 1, func() { mustSteer(t, a, "steer now") }))

	require.NoError(t, a.Prompt(context.Background(), user("start")))
	assert.Equal(t, [][]string{{"start"}, {"start", "steer now"}}, inLog)
}

func TestCommitHappensBeforeMessageEndIsPublished(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	a := newAgent(t, p, m, nil)
	var checked int
	a.Subscribe(func(ev protocol.Event) error {
		if end, ok := ev.(*protocol.MessageEnd); ok {
			msgs := a.State().Messages
			require.NotEmpty(t, msgs)
			assert.Equal(t, end.Message, msgs[len(msgs)-1], "the message is in the log when message_end is published")
			checked++
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 4, checked) // user, assistant, tool result, assistant
}

func TestAssistantFramesLieInsideTheirAttempt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	var events []protocol.Event
	// settledBefore holds the AttemptSettled IDs that the log held when each
	// attempt_end was published.
	settledBefore := map[string]bool{}
	a.Subscribe(func(ev protocol.Event) error {
		events = append(events, ev)
		if end, ok := ev.(*protocol.AttemptEnd); ok {
			for _, s := range logged[sessions.AttemptSettled](log) {
				if s.AttemptID == end.AttemptID {
					settledBefore[end.AttemptID] = true
				}
			}
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	open := ""
	attempts := 0
	for _, ev := range events {
		switch e := ev.(type) {
		case *protocol.AttemptStart:
			require.Empty(t, open, "attempts do not overlap")
			open = e.AttemptID
			attempts++
		case *protocol.AttemptEnd:
			assert.Equal(t, open, e.AttemptID)
			assert.True(t, settledBefore[e.AttemptID], "AttemptSettled is in the log before attempt_end is published")
			open = ""
		case *protocol.MessageStart:
			if _, ok := e.Message.(protocol.AssistantMessage); ok {
				assert.NotEmpty(t, open, "assistant message_start lies inside an attempt")
			}
		case *protocol.MessageUpdate:
			assert.NotEmpty(t, open, "message_update lies inside an attempt")
		case *protocol.MessageEnd:
			if _, ok := e.Message.(protocol.AssistantMessage); ok {
				assert.NotEmpty(t, open, "assistant message_end lies inside an attempt")
			}
		}
	}
	assert.Empty(t, open)
	assert.Equal(t, 2, attempts)
	assert.Len(t, logged[sessions.AttemptSettled](log), 2)
}

// decompose splits what the provider received into the parts that the log
// keeps apart: the system prompt, the tools and the rest of the messages.
func decompose(msgs []protocol.Message) (prompt string, decls []protocol.ToolDecl, body []protocol.Message) {
	if len(msgs) > 0 {
		if sys, ok := msgs[0].(protocol.SystemMessage); ok {
			for _, c := range sys.Content {
				prompt += c.Text
			}
		}
	}
	return prompt, providers.CurrentTools(msgs), msgs[1:]
}

func toJSON(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func attemptIDs(events []protocol.Event) []string {
	var ids []string
	for _, ev := range events {
		if s, ok := ev.(*protocol.AttemptStart); ok {
			ids = append(ids, s.AttemptID)
		}
	}
	return ids
}

func TestRebuiltLogicalRequestMatchesFauxRequest(t *testing.T) {
	p, err := faux.New(faux.WithChunk(1000, 1000), faux.WithModels(
		faux.ModelDef{ID: "faux-1", ContextWindow: 1000, MaxTokens: 100},
		faux.ModelDef{ID: "faux-2", ContextWindow: 1000, MaxTokens: 200},
	))
	require.NoError(t, err)
	first, _ := p.Model("faux-1")
	second, _ := p.Model("faux-2")
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("one"), faux.Say("two"))

	log := &sessions.MemoryLog{}
	a := newAgent(t, p, first, edits(withLog(log), func(c *agent.Config) {
		c.SystemPrompt = "be brief"
		c.Options.MaxTokens = 77
		c.Options.CacheRetention = providers.CacheRetentionShort
		c.Ready = func(context.Context, providers.Model, string) error { return nil }
	}))
	var events []protocol.Event
	a.Subscribe(func(ev protocol.Event) error { events = append(events, ev); return nil })

	require.NoError(t, a.Prompt(context.Background(), user("one")))
	require.NoError(t, a.SetModel(context.Background(), second))
	require.NoError(t, a.Prompt(context.Background(), user("two")))

	reqs := p.Requests()
	ids := attemptIDs(events)
	require.Len(t, reqs, 3)
	require.Len(t, ids, 3)
	for i, rec := range reqs {
		got, err := agent.RebuildRequest(log.Entries(), ids[i])
		require.NoError(t, err, "attempt %d", i)
		prompt, decls, body := decompose(rec.Transcript.Messages)
		assert.Equal(t, "be brief", got.SystemPrompt)
		assert.Equal(t, prompt, got.SystemPrompt)
		assert.JSONEq(t, toJSON(t, decls), toJSON(t, got.Tools), "attempt %d tools", i)
		assert.JSONEq(t, toJSON(t, rec.Transcript.Messages), toJSON(t, got.Messages), "attempt %d messages", i)
		assert.JSONEq(t, toJSON(t, body), toJSON(t, got.Messages[1:]), "attempt %d history", i)
		assert.Equal(t, rec.Model.ID, got.Model.ID, "attempt %d model", i)
		assert.Equal(t, rec.Model.Provider, got.Model.Provider)
		assert.Equal(t, string(rec.Model.API), got.Model.API)
		assert.Equal(t, 77, got.Options.MaxTokens)
		assert.Equal(t, providers.CacheRetentionShort, got.Options.CacheRetention)
		assert.Equal(t, rec.Options.SessionID, got.Options.SessionID)
	}
	assert.Equal(t, "faux-1", reqs[0].Model.ID)
	assert.Equal(t, "faux-2", reqs[2].Model.ID, "the model switch is logged with the request")

	_, err = agent.RebuildRequest(log.Entries(), "no-such-attempt")
	assert.Error(t, err)
}

func TestRebuildAfterSystemPromptChangeUsesLoggedPrompt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	log := &sessions.MemoryLog{} // two Agents share one log

	var events []protocol.Event
	collect := func(a *agent.Agent) {
		a.Subscribe(func(ev protocol.Event) error { events = append(events, ev); return nil })
	}
	oldAgent := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) { c.SystemPrompt = "old prompt" }))
	collect(oldAgent)
	require.NoError(t, oldAgent.Prompt(context.Background(), user("one")))
	newer := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) { c.SystemPrompt = "new prompt" }))
	collect(newer)
	require.NoError(t, newer.Prompt(context.Background(), user("two")))

	require.Len(t, logged[sessions.SystemSnapshot](log), 2)
	ids := attemptIDs(events)
	require.Len(t, ids, 2)
	first, err := agent.RebuildRequest(log.Entries(), ids[0])
	require.NoError(t, err)
	assert.Equal(t, "old prompt", first.SystemPrompt)
	assert.Equal(t, "old prompt", p.Requests()[0].Transcript.Messages[0].(protocol.SystemMessage).Content[0].Text)
	second, err := agent.RebuildRequest(log.Entries(), ids[1])
	require.NoError(t, err)
	assert.Equal(t, "new prompt", second.SystemPrompt)
	assert.JSONEq(t, toJSON(t, p.Requests()[1].Transcript.Messages), toJSON(t, second.Messages))
}

func TestLoggedSystemSnapshotIsSeparateCopyOfDispatchedHeader(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.SystemPrompt = "be brief"
		// An ExecuteModel handler changes the dispatched header in place after
		// it passes the request on.
		registryOf(c).OnExecuteModel(func(ctx context.Context, call pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
			s, err := next(ctx, call)
			if sys, ok := call.Request.Messages[0].(protocol.SystemMessage); ok {
				for i := range sys.ToolsAdded {
					for j := range sys.ToolsAdded[i].Parameters {
						sys.ToolsAdded[i].Parameters[j] = 'X'
					}
				}
				sys.Content[0].Text = "edited after dispatch"
			}
			return s, err
		})
	}))

	require.NoError(t, a.Prompt(context.Background(), user("one")))
	require.NoError(t, a.Prompt(context.Background(), user("two")))

	snaps := logged[sessions.SystemSnapshot](log)
	require.Len(t, snaps, 1, "the same prompt and tools are logged once")
	assert.Equal(t, "be brief", snaps[0].SystemPrompt)
	require.Len(t, snaps[0].Tools, 1)
	assert.Equal(t, "echo", snaps[0].Tools[0].Name)
	assert.True(t, json.Valid(snaps[0].Tools[0].Parameters), "the logged schema was not edited")
}

func TestHandlerMutationDoesNotChangeCommittedHistory(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	log := &sessions.MemoryLog{}
	edit := false
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		registryOf(c).OnPrepareRequest(func(ctx context.Context, r pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			if edit {
				for _, msg := range r.Context.Messages {
					if u, ok := msg.(protocol.UserMessage); ok {
						u.Content[0] = protocol.Text{Text: "edited in place"}
					}
				}
			}
			return next(ctx, r)
		})
	}))
	require.NoError(t, a.Prompt(context.Background(), user("original")))
	edit = true
	require.NoError(t, a.Prompt(context.Background(), user("second")))

	assert.Equal(t, []string{"original", "second"}, userTexts(log.Messages()), "the log keeps the committed text")
	assert.Equal(t, []string{"original", "second"}, userTexts(a.State().Messages))
	// The edit is model-visible, so the request delta carries it.
	assert.Equal(t, []string{"edited in place", "edited in place"}, userTexts(p.Requests()[1].Transcript.Messages))
}

func TestPrepareRequestEditsAreLoggedNotStoredAsMessages(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	log := &sessions.MemoryLog{}
	var events []protocol.Event
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.SystemPrompt = "be brief"
		registryOf(c).OnPrepareRequest(func(_ context.Context, r pipeline.Request, _ pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			return &pipeline.RequestUpdate{RequestMessages: append(slices.Clone(r.Context.Messages), user("ephemeral note"))}, nil
		})
	}))
	a.Subscribe(func(ev protocol.Event) error { events = append(events, ev); return nil })
	require.NoError(t, a.Prompt(context.Background(), user("hello")))

	assert.Equal(t, []string{"hello"}, userTexts(log.Messages()), "an edit of the request is not a stored message")
	assert.Equal(t, []string{"hello", "ephemeral note"}, userTexts(p.Requests()[0].Transcript.Messages))
	deltas := logged[sessions.RequestDelta](log)
	require.Len(t, deltas, 1)
	require.Len(t, deltas[0].Added, 1)
	assert.Equal(t, []string{"ephemeral note"}, userTexts(deltas[0].Added))
	assert.Empty(t, deltas[0].Removed)

	got, err := agent.RebuildRequest(log.Entries(), attemptIDs(events)[0])
	require.NoError(t, err)
	assert.JSONEq(t, toJSON(t, p.Requests()[0].Transcript.Messages), toJSON(t, got.Messages))
}

func TestDispatchedRequestDoesNotChangeAfterLaterHistoryEdit(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	var first providers.TranscriptRequest
	var firstJSON string
	calls := 0
	edit := false
	a := newAgent(t, p, m, edits(
		spyStream(p, func(req providers.TranscriptRequest) {
			calls++
			if calls == 1 {
				first = req // the request as the provider holds it
				firstJSON = toJSON(t, req.Messages)
			}
		}),
		func(c *agent.Config) {
			registryOf(c).OnPrepareRequest(func(ctx context.Context, r pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				if edit {
					for _, msg := range r.Context.Messages {
						if u, ok := msg.(protocol.UserMessage); ok {
							u.Content[0] = protocol.Text{Text: "edited later"}
						}
					}
				}
				return next(ctx, r)
			})
		}))
	require.NoError(t, a.Prompt(context.Background(), user("original")))
	edit = true
	require.NoError(t, a.Prompt(context.Background(), user("second")))

	assert.JSONEq(t, firstJSON, toJSON(t, first.Messages), "the dispatched request is not changed by a later edit")
	assert.Equal(t, []string{"edited later", "edited later"}, userTexts(p.Requests()[1].Transcript.Messages), "the next request has the edit")
}

func TestRequestToolsAreInNameOrderNotRegistrationOrder(t *testing.T) {
	run := func(order ...tools.Tool) (names []string, snapshot []string) {
		p, m := newFaux(t)
		p.Set(faux.Say("ok"))
		log := &sessions.MemoryLog{}
		a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) { c.Tools = registry(t, order...) }))
		require.NoError(t, a.Prompt(context.Background(), user("go")))
		for _, d := range providers.CurrentTools(p.Requests()[0].Transcript.Messages) {
			names = append(names, d.Name)
		}
		for _, d := range logged[sessions.SystemSnapshot](log)[0].Tools {
			snapshot = append(snapshot, d.Name)
		}
		return names, snapshot
	}
	add, mul, echo := namedTool("add"), namedTool("mul"), tools.Echo{}
	a1, s1 := run(mul, echo, add)
	a2, s2 := run(add, mul, echo)
	assert.Equal(t, []string{"add", "echo", "mul"}, a1)
	assert.Equal(t, a1, a2)
	assert.Equal(t, a1, s1)
	assert.Equal(t, a1, s2)
}

// namedTool is a tool that has only a name and answers "ok".
func namedTool(name string) tools.Tool {
	return &funcTool{name: name, run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		return textResult("ok"), nil
	}}
}

func TestSessionEntriesRebuildSameContext(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	var live []protocol.Message
	a.Subscribe(func(ev protocol.Event) error {
		if end, ok := ev.(*protocol.MessageEnd); ok {
			live = append(live, end.Message)
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	rebuilt := sessions.MessagesOf(log.Entries())
	require.Len(t, rebuilt, len(live))
	for i := range live {
		assert.JSONEq(t, toJSON(t, live[i]), toJSON(t, rebuilt[i]), "message %d", i)
	}
	assert.Equal(t, []string{"user", "assistant", "toolResult:c1", "assistant"}, roles(rebuilt))
}

// canary values are unique, so that a leak into any output is found.
const (
	canaryToken   = "CANARY-access-token-7f3a"
	canaryAccount = "CANARY-account-id-91c2"
	canaryAPIKey  = "CANARY-options-api-key-55d0"
	canarySecret  = "CANARY-bound-secret-0be8"
	canaryHeader  = "CANARY-model-header-a41e"
	canaryQuery   = "CANARY-base-url-query-c7d9"
	canaryKey     = "CANARY-resolved-key-2d6b"
	canaryStreamQ = "CANARY-stream-error-query-6e1f"
	canaryStreamB = "CANARY-stream-error-bearer-b3a7"
)

func TestCredentialCanariesNeverReachEntriesEventsOrRebuild(t *testing.T) {
	t.Run("HTTP failures", testAdapterCredentialCanaries)
	canaries := []string{canaryToken, canaryAccount, canaryAPIKey, canarySecret, canaryHeader, canaryQuery, canaryKey, canaryStreamQ, canaryStreamB}
	p, m := newFaux(t)
	m.BaseURL = "https://user:" + canarySecret + "@api.example.test/v1?key=" + canaryQuery
	m.Headers = map[string]string{"X-Canary": canaryHeader}
	// The second prompt ends in a provider error whose raw text carries a key
	// in the URL query and a bearer token.
	failure := providers.NewFailure(providers.CodeServer, 503, 0, "upstream POST https://api.example.test/v1/chat?key="+canaryStreamQ+" said: Authorization: Bearer "+canaryStreamB, nil)
	p.Set(faux.Say("fine"), faux.Reply().Err(failure), faux.Reply().Err(failure), faux.Say("recovered"))

	log := &sessions.MemoryLog{}
	failAuth := false
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(context.Context, time.Duration) error { return nil }
		c.SystemPrompt = "be brief"
		c.Options.APIKey = canaryAPIKey
		c.Options.Auth = providers.AuthSnapshot{Provider: m.Provider, Method: "oauth", AccessToken: canaryToken, AccountID: canaryAccount}
		// The pin is for another provider, so GetAPIKey still resolves this one.
		c.BoundKey = providers.BoundKey{Provider: "other-provider", Secret: canarySecret}
		c.GetAPIKey = func(context.Context, string) (string, error) {
			if failAuth {
				return "", errors.New("refresh failed: POST https://auth.example.test/token?refresh=" + canaryQuery +
					": Authorization: Bearer " + canaryToken + " account " + "sk-" + canaryAccount)
			}
			return canaryKey, nil // the key that goes into opts.APIKey of the request
		}
		// The stream reports the binding of the request, as the composed
		// AuthRunner does, and a preparer lists the header names of the model.
		c.Stream = func(ctx context.Context, model providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
			return p.Stream(ctx, model, req, opts).WithBinding(opts.Auth.Binding())
		}
		c.Prepare = func(model providers.Model, _ providers.TranscriptRequest, _ providers.StreamOptions) (*providers.Prepared, error) {
			pr := &providers.Prepared{Provider: model.Provider, API: string(model.API), Model: model.ID, MaxTokens: 50, RetryPolicy: providers.DefaultRetryPolicy()}
			for name := range model.Headers {
				pr.HeaderNames = append(pr.HeaderNames, strings.ToLower(name))
			}
			return pr, nil
		}
	}))
	var events []protocol.Event
	a.Subscribe(func(ev protocol.Event) error { events = append(events, ev); return nil })

	require.NoError(t, a.Prompt(context.Background(), user("one")))
	require.NoError(t, a.Prompt(context.Background(), user("provider error")))
	assert.Len(t, logged[sessions.RetryScheduled](log), 2)
	failAuth = true
	require.Error(t, a.Prompt(context.Background(), user("two")))

	var out bytes.Buffer
	for _, e := range log.Entries() {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		out.Write(b)
	}
	for _, ev := range events {
		b, err := protocol.EncodeEvent(ev)
		require.NoError(t, err)
		out.Write(b)
	}
	ids := attemptIDs(events)
	require.NotEmpty(t, ids)
	for _, id := range ids {
		got, err := agent.RebuildRequest(log.Entries(), id)
		require.NoError(t, err)
		out.WriteString(toJSON(t, got))
	}
	for _, c := range canaries {
		assert.False(t, bytes.Contains(out.Bytes(), []byte(c)), "canary %q leaked", c)
	}

	// The allowlist keeps what is safe and useful.
	got, err := agent.RebuildRequest(log.Entries(), ids[0])
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.test", got.Model.BaseURLHost)
	assert.Equal(t, []string{"X-Canary"}, got.Model.HeaderNames)

	// The prepared values and the binding are in the log, and the dump above
	// holds neither the header value nor the access token.
	require.NotNil(t, got.Prepared)
	assert.Equal(t, 50, got.Prepared.MaxTokens)
	assert.Equal(t, []string{"x-canary"}, got.Prepared.HeaderNames)
	assert.Equal(t, providers.AuthBinding{Provider: m.Provider, Method: "oauth"}, got.Binding)
}

func TestRunFailureMessageIsCleaned(t *testing.T) {
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	long := strings.Repeat("x", 800)
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.GetAPIKey = func(context.Context, string) (string, error) {
			return "", errors.New(`Get "https://api.example.test/v1/models?key=SECRETQ": Bearer SECRETB ` + long)
		}
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)

	err := a.Prompt(context.Background(), user("go"))
	require.Error(t, err)

	end := rec.events[len(rec.events)-2].(*protocol.AgentEnd)
	msg := end.Messages[0].(protocol.AssistantMessage)
	text := errorText(msg)
	assert.NotContains(t, text, "SECRETQ")
	assert.NotContains(t, text, "SECRETB")
	assert.Contains(t, text, "https://api.example.test/v1/models")
	assert.LessOrEqual(t, len(text), 512)
	stored := lastAssistant(t, log.Messages())
	assert.Equal(t, text, errorText(stored), "the stored message has the cleaned text")
}

func TestWriteFailureStopsFurtherSideEffects(t *testing.T) {
	errDisk := errors.New("disk full")
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	// The log takes every write up to the assistant message, which it refuses.
	log := &commitGate{failWhen: func(entries []sessions.Entry) bool {
		for _, e := range entries {
			if me, ok := e.(sessions.MessageEntry); ok {
				if _, isAssistant := me.Message.(protocol.AssistantMessage); isAssistant {
					return true
				}
			}
		}
		return false
	}, err: errDisk}
	bodies := 0
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		registryOf(c).OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
			bodies++
			return next(ctx, in)
		})
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)

	err := a.Prompt(context.Background(), user("go"))
	require.ErrorIs(t, err, errDisk)
	assert.Zero(t, bodies, "no tool body starts after the write failed")
	assert.Len(t, p.Requests(), 1)
	labels := rec.eventLabels()
	assert.Equal(t, "agent_settled", labels[len(labels)-1])
	assert.NotContains(t, labels, "tool_execution_start(c1)")

	t.Run("second append", func(t *testing.T) {
		p, model := newFaux(t)
		p.Set(faux.Reply(call("count", "c1", nil)))
		bodies := 0
		tool := &funcTool{name: "count", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			bodies++
			return textResult("ran"), nil
		}}
		appends := 0
		failedAppend := 0
		log := &commitGate{err: errDisk, failWhen: func([]sessions.Entry) bool {
			appends++
			if appends == 2 {
				failedAppend = appends
				return true
			}
			return false
		}}
		a := newAgent(t, p, model, edits(withLog(log), func(config *agent.Config) {
			config.Tools = registry(t, tool)
		}))
		t.Cleanup(func() { require.NoError(t, a.Dispose()) })
		rec := &recorder{}
		a.Subscribe(rec.emit)

		require.ErrorIs(t, a.Prompt(context.Background(), user("go")), errDisk)
		require.Equal(t, 2, failedAppend, "the real writer rejects exactly its second Append call")
		require.Zero(t, bodies)
		require.Empty(t, p.Requests(), "the failed early append stops model dispatch")
		require.Len(t, logged[sessions.SystemSnapshot](log), 1, "the first append committed the snapshot")
		labels := rec.eventLabels()
		require.NotEmpty(t, labels)
		require.Equal(t, "agent_settled", labels[len(labels)-1])
		require.NotContains(t, labels, "tool_execution_start(c1)")
	})
}

// commitGate refuses every Append for which failWhen is true.
type commitGate struct {
	sessions.MemoryLog
	failWhen func([]sessions.Entry) bool
	err      error
}

func (g *commitGate) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	if g.failWhen(entries) {
		return sessions.CommitRef{}, g.err
	}
	return g.MemoryLog.Append(entries...)
}

func TestProviderErrorTextIsCleanedBeforeCommitAndPublish(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Fail("POST https://api.example.test/v1?key=SECRETQ: Bearer SECRETB"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	var texts []string
	for _, ev := range rec.events {
		switch e := ev.(type) {
		case *protocol.MessageEnd:
			if am, ok := e.Message.(protocol.AssistantMessage); ok {
				texts = append(texts, errorText(am))
			}
		case *protocol.TurnEnd:
			texts = append(texts, errorText(e.Message.(protocol.AssistantMessage)))
		case *protocol.AgentEnd:
			for _, msg := range e.Messages {
				if am, ok := msg.(protocol.AssistantMessage); ok {
					texts = append(texts, errorText(am))
				}
			}
		}
	}
	texts = append(texts, errorText(lastAssistant(t, log.Messages())))
	require.Len(t, texts, 4)
	for _, text := range texts {
		assert.NotContains(t, text, "SECRETQ")
		assert.NotContains(t, text, "SECRETB")
		assert.Contains(t, text, "https://api.example.test/v1")
	}
}

func TestExecuteModelHandlerEditsDoNotReachTheProvider(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	log := &sessions.MemoryLog{}
	var events []protocol.Event
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.SystemPrompt = "be brief"
		registryOf(c).OnExecuteModel(func(ctx context.Context, call pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
			call.Request.Messages = append(call.Request.Messages, user("smuggled"))
			if u, ok := call.Request.Messages[1].(protocol.UserMessage); ok {
				u.Content[0] = protocol.Text{Text: "rewritten"}
			}
			return next(ctx, call)
		})
	}))
	a.Subscribe(func(ev protocol.Event) error { events = append(events, ev); return nil })
	require.NoError(t, a.Prompt(context.Background(), user("original")))

	sent := p.Requests()[0].Transcript.Messages
	assert.Equal(t, []string{"original"}, userTexts(sent), "the provider gets the logged request")
	got, err := agent.RebuildRequest(log.Entries(), attemptIDs(events)[0])
	require.NoError(t, err)
	assert.JSONEq(t, toJSON(t, sent), toJSON(t, got.Messages))
	assert.Empty(t, logged[sessions.RequestDelta](log)[0].Added)
}

func TestTurnClosedIsCommittedOnceWhenRunFailsAfterTurnEnd(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	log := &sessions.MemoryLog{}
	errStop := errors.New("stop handler failed")
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		registryOf(c).OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
			return pipeline.Proceed, errStop
		})
	}))
	require.ErrorIs(t, a.Prompt(context.Background(), user("go")), errStop)
	assert.Len(t, logged[sessions.TurnOpened](log), 1)
	assert.Len(t, logged[sessions.TurnClosed](log), 1, "a run that fails after turn_end closes the turn once")
}

func TestNoTurnClosedWhenNoTurnWasOpened(t *testing.T) {
	p, m := newFaux(t)
	log := &commitGate{failWhen: func(entries []sessions.Entry) bool {
		for _, e := range entries {
			if _, ok := e.(sessions.SystemSnapshot); ok {
				return true
			}
		}
		return false
	}, err: errors.New("disk full")}
	a := newAgent(t, p, m, withLog(log))
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.Error(t, a.Prompt(context.Background(), user("go")))
	assert.Empty(t, logged[sessions.TurnOpened](log))
	assert.Empty(t, logged[sessions.TurnClosed](log), "no turn was opened, so none is closed in the log")
	assert.Contains(t, rec.eventLabels(), "turn_end", "turn_end is still published")
	assert.Empty(t, p.Requests())
}

func TestAttemptUsageIsUnknownWhenProviderSentNone(t *testing.T) {
	log := &sessions.MemoryLog{}
	m := providers.TokenPlanMessages()
	a, err := agent.New(agent.Config{
		LoopConfig: agent.LoopConfig{
			Model: m,
			Stream: func(ctx context.Context, m providers.Model, _ providers.TranscriptRequest, _ providers.StreamOptions) *providers.Stream {
				return providers.NewStream(ctx, 0, protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID},
					func(as *providers.Assembler) { as.Start(); as.Done(protocol.StopStop) })
			},
		},
		NewContext: func() sessions.Writer { return log },
	})
	require.NoError(t, err)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 1)
	assert.Nil(t, settled[0].Usage, "no usage from the provider is unknown, not zero")

	// A provider that reports usage gets it logged.
	p, fm := newFaux(t)
	p.Set(faux.Say("ok"))
	log2 := &sessions.MemoryLog{}
	b := newAgent(t, p, fm, withLog(log2))
	require.NoError(t, b.Prompt(context.Background(), user("go")))
	require.NotNil(t, logged[sessions.AttemptSettled](log2)[0].Usage)
}

func TestRequestToolsStayInNameOrderAfterMidRunToolChange(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("install", nil, faux.ID("c1"))), faux.Say("done"))
	reg := &tools.Registry{}
	ran := false
	require.NoError(t, reg.Register(namedTool("zeta"), tools.SourceInfo{Kind: tools.SourceBuiltin}))
	require.NoError(t, reg.Register(installer(reg, lateTool(&ran)), tools.SourceInfo{Kind: tools.SourceBuiltin}))
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = reg })
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	names := func(msgs []protocol.Message) []string {
		var out []string
		for _, d := range providers.CurrentTools(msgs) {
			out = append(out, d.Name)
		}
		return out
	}
	reqs := p.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"install", "zeta"}, names(reqs[0].Transcript.Messages))
	assert.Equal(t, []string{"install", "late", "zeta"}, names(reqs[1].Transcript.Messages), "the tool added in the run sorts by name")
}

func TestRunFailureAfterToolBatchEndsWithFailureTail(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("never"))
	log := &commitGate{failWhen: func(entries []sessions.Entry) bool {
		for _, e := range entries {
			if me, ok := e.(sessions.MessageEntry); ok && me.Message.Role() == protocol.RoleToolResult {
				return true
			}
		}
		return false
	}, err: errors.New("disk full")}
	a := newAgent(t, p, m, withLog(log))
	rec := &recorder{}
	a.Subscribe(rec.emit)

	err := a.Prompt(context.Background(), user("hi"))

	require.ErrorContains(t, err, "disk full")
	assert.Equal(t, append([]string{
		"agent_start", "cycle_start", "turn_start", "message_start(user)", "message_end(user)",
		"attempt_start",
		"message_start(assistant)",
		"message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)",
		"message_end(assistant)",
		"attempt_end",
		"tool_execution_start(c1)", "tool_execution_end(c1)",
		"message_start(toolResult:c1)",
	}, failureTail()...), rec.eventLabels(), "the failure message follows the tool batch; nothing is published for the refused result")
	assert.Equal(t, 1, p.Calls(), "no model call after the failed commit")
	st := a.State()
	assert.Equal(t, agent.Idle, st.Status)
	assert.Equal(t, protocol.StopError, lastAssistant(t, st.Messages).StopReason)
}

func testAdapterCredentialCanaries(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": "overloaded_error", "message": "Authorization: Bearer " + canaryStreamB + " https://api.example.test?key=" + canaryStreamQ}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicTextSSE()))
	}))
	defer server.Close()
	adapter := anthropic.New(anthropic.WithHTTPClient(server.Client()), anthropic.WithEnv(func(string) (string, bool) { return "", false }))
	registry := providers.NewRegistry()
	registry.RegisterProvider(adapter)
	model := providers.TokenPlanMessages()
	model.BaseURL = server.URL
	log := &sessions.MemoryLog{}
	a, err := agent.New(agent.Config{Registry: registry, NewContext: func() sessions.Writer { return log }, LoopConfig: agent.LoopConfig{Model: model, Options: providers.StreamOptions{APIKey: canaryAPIKey}, Wait: func(context.Context, time.Duration) error { return nil }}})
	require.NoError(t, err)
	defer func() { require.NoError(t, a.Dispose()) }()
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("hello")))
	assert.Equal(t, 3, calls)
	assert.Len(t, logged[sessions.RetryScheduled](log), 2)
	var out bytes.Buffer
	for _, entry := range log.Entries() {
		out.WriteString(toJSON(t, entry))
	}
	for _, event := range rec.events {
		wire, encodeErr := protocol.EncodeEvent(event)
		require.NoError(t, encodeErr)
		out.Write(wire)
	}
	for _, id := range attemptIDs(rec.events) {
		request, rebuildErr := agent.RebuildRequest(log.Entries(), id)
		require.NoError(t, rebuildErr)
		out.WriteString(toJSON(t, request))
	}
	for _, canary := range []string{canaryAPIKey, canaryStreamB, canaryStreamQ} {
		assert.NotContains(t, out.String(), canary)
	}
	assert.Equal(t, "hello", assistantText(lastAssistant(t, a.State().Messages)), "successful model text must stay intact")
}
