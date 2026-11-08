package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The model rows of the account list in the design sketches. Every row is a
// reasoning model; claude-opus-4-9 matches a family rule of Ask but has no
// metadata, so its context window is unknown.
const (
	miniModel = "openai/gpt-5.4-mini@responses"
	opus47    = "anthropic/claude-opus-4-7@messages"
	opus49    = "anthropic/claude-opus-4-9@messages"
	sonnet46  = "anthropic/claude-sonnet-4-6@messages"
	sketchBar = " Tab blocks · ctrl+t thinking · ctrl+l model · / commands"
	// sketchHeader is the header row: the logo and nothing else.
	sketchHeader = " ◢◤ ask"
	// sketchDir is the start of the footer row in the sketches.
	sketchDir = " ⌂ ~/src/askcore"
)

func accountRows() []protocol.ACPModel {
	row := func(id, provider, name, api string, window int) protocol.ACPModel {
		return protocol.ACPModel{ModelID: id, Provider: provider, ID: name, API: api, ContextWindow: window, Reasoning: true}
	}
	return []protocol.ACPModel{
		row(opus47, "anthropic", "claude-opus-4-7", "messages", 1_000_000),
		row(opus49, "anthropic", "claude-opus-4-9", "messages", 0),
		row(sonnet46, "anthropic", "claude-sonnet-4-6", "messages", 1_000_000),
		row(miniModel, "openai", "gpt-5.4-mini", "responses", 400_000),
		row(openaiGPT55, "openai", "gpt-5.5", "responses", 1_000_000),
	}
}

// accountFake is a leader with the account list of the sketches; the session
// runs on current with the thinking level medium.
func accountFake(current string, rows []protocol.ACPModel) *fakeClient {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: current, Models: rows, UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: current, ThinkingLevel: protocol.ThinkingMedium})
	return fc
}

// sketchTurn plays the reply of the sketches on a model. tokens is the context
// use of the reply.
func (r *viewRig) sketchTurn(model string, tokens int64) {
	r.t.Helper()
	const reply = "The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket after the lock probe, and the probe can take 1.2 s."
	usage := protocol.Usage{Input: 12_400, Output: 1_800, TotalTokens: tokens, Cost: protocol.Cost{Total: 40_000}}
	r.event(flat(
		&protocol.AgentStart{},
		userAt("why does TestLeaderStale fail on CI?", sketchTime(14, 2)),
		assistantStart(model, 0),
		textParts(0, reply),
		assistantEndUsage(model, protocol.StopStop, 4200, usage, protocol.Text{Text: reply}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
	r.h.waitText("take 1.2 s.")
}

// idleSketch is an idle session: one reply on gpt-5.4-mini that used
// 4% of its context window.
func idleSketch(t *testing.T, w, h int) *viewRig {
	r := startView(t, w, h, withFake(accountFake(miniModel, accountRows())))
	r.sketchTurn("gpt-5.4-mini", 16_000)
	r.h.waitText("4%")
	return r
}

func TestFooterRowGoldens(t *testing.T) {
	t.Run("idle_80x24", func(t *testing.T) {
		r := idleSketch(t, 80, 24)
		requireContentGolden(t, r.h)
		rows := r.rows()
		require.Len(t, rows, 24)
		require.Equal(t, sketchHeader, rows[0], "the header row holds the logo only")
		require.Equal(t, ruleRow(80), rows[1])
		require.Equal(t, strings.TrimRight(promptRow("why does TestLeaderStale fail on CI?", "14:02"), " "), rows[2], "the pane starts on row 3")
		for _, row := range rows {
			require.False(t, strings.HasPrefix(row, "  model  "), "a model line: %q", row)
		}
		for _, row := range []string{rows[0], rows[22]} {
			for _, link := range []string{"●", "leader", "7c1e", "in-process", "observer"} {
				require.NotContains(t, row, link, "a link state in the header row or the footer row")
			}
		}
		require.Equal(t, ruleRow(80), rows[19])
		require.Equal(t, ruleRow(80), rows[21])
		require.Equal(t, ` ⌂ ~/src/askcore                      openai/gpt-5.4-mini ◐ medium  ▱▱▱▱▱▱▱▱ 4%`, rows[22])
		require.NotContains(t, rows[22], "$", "the footer row shows no cost")
		require.Equal(t, sketchBar, rows[23])
	})
	t.Run("idle_120x40", func(t *testing.T) {
		r := idleSketch(t, 120, 40)
		requireContentGolden(t, r.h)
		rows := r.rows()
		require.Equal(t, sketchHeader, rows[0])
		require.True(t, strings.HasPrefix(rows[38], sketchDir), rows[38])
		require.True(t, strings.HasSuffix(rows[38], "openai/gpt-5.4-mini ◐ medium  ▱▱▱▱▱▱▱▱ 4%"), rows[38])
		require.True(t, strings.HasSuffix(rows[39], "shift/option+drag select"), rows[39])
	})
	t.Run("idle_40x14", func(t *testing.T) {
		r := idleSketch(t, 40, 14)
		requireContentGolden(t, r.h)
		rows := r.rows()
		require.Equal(t, sketchHeader, rows[0])
		require.Equal(t, ruleRow(40), rows[1])
		require.Equal(t, "                 openai/gpt-5.4-mini 4%", rows[12])
		require.Equal(t, " Tab blocks · / commands", rows[13])
	})
}

// The queue rows and the status row stay above the prompt pane while a tool
// runs; the footer row and the bar do not move.
func TestFooterRowWhileToolRunsAndSteerWaits(t *testing.T) {
	r := idleSketch(t, 80, 24)
	r.event(flat(
		&protocol.AgentStart{},
		userAt("run the leader tests and fix what fails", sketchTime(14, 4)),
		toolStart("c1", "bash", `{"command":"go test ./internal/leader -count=1"}`, 0),
	)...)
	r.clock.advance(4 * time.Second)
	r.event(&protocol.QueueUpdate{Steering: []string{"use the fake clock from internal/testsupport"}})
	r.h.waitText("Working… 4s")
	rows := r.rows()
	require.Equal(t, []string{
		"┆ ⧗ steer   use the fake clock from internal/testsupport",
		"┆ alt+↑ edit queue · dashed rail = not sent yet",
		"┊ ◌ Working… 4s · esc to interrupt",
	}, rows[16:19])
	require.True(t, strings.HasPrefix(rows[22], sketchDir), rows[22])
	require.Equal(t, sketchBar, rows[23])
}

// After a resize the footer row is always the second-to-last row.
func TestFooterRowFollowsResize(t *testing.T) {
	r := idleSketch(t, 80, 24)
	r.h.resize(40, 14)
	rows := r.rows()
	require.Len(t, rows, 14)
	require.Equal(t, "                 openai/gpt-5.4-mini 4%", rows[12])
	require.Equal(t, " Tab blocks · / commands", rows[13])
	r.h.resize(80, 24)
	rows = r.rows()
	require.Len(t, rows, 24)
	require.Equal(t, sketchHeader, rows[0])
	require.Equal(t, ` ⌂ ~/src/askcore                      openai/gpt-5.4-mini ◐ medium  ▱▱▱▱▱▱▱▱ 4%`, rows[22])
	require.Equal(t, sketchBar, rows[23])
}

// Small terminals keep the footer row and the bar; the rules show from 9 rows
// and the header row goes first under 5 rows.
func TestFooterRowOnShortScreens(t *testing.T) {
	isRule := func(row string) bool { return row != "" && strings.Trim(row, "─") == "" }
	t.Run("9 rows", func(t *testing.T) {
		r := startView(t, 80, 9, withFake(noModelFake()))
		rows := r.rows()
		require.Len(t, rows, 9)
		require.Equal(t, sketchHeader, rows[0])
		require.True(t, isRule(rows[1]))
		require.False(t, isRule(rows[2]), "one pane row: %q", rows[2])
		require.True(t, strings.HasPrefix(rows[3], "┊ "), "the status row: %q", rows[3])
		require.True(t, isRule(rows[4]))
		require.True(t, strings.HasPrefix(rows[5], " ❯ "), rows[5])
		require.True(t, isRule(rows[6]))
		require.True(t, strings.HasPrefix(rows[7], sketchDir), rows[7])
		require.Equal(t, sketchBar, rows[8])
	})
	t.Run("8 rows", func(t *testing.T) {
		r := startView(t, 80, 8, withFake(noModelFake()))
		rows := r.rows()
		require.Len(t, rows, 8)
		require.Equal(t, sketchHeader, rows[0])
		for _, row := range rows {
			require.False(t, isRule(row), "no rule under 9 rows: %q", row)
		}
		require.True(t, strings.HasPrefix(rows[6], sketchDir), rows[6])
		require.Equal(t, sketchBar, rows[7])
	})
	t.Run("5 rows", func(t *testing.T) {
		r := startView(t, 80, 5, withFake(noModelFake()))
		rows := r.rows()
		require.Len(t, rows, 5)
		require.Equal(t, sketchHeader, rows[0])
		require.True(t, strings.HasPrefix(rows[3], sketchDir), rows[3])
		require.Equal(t, sketchBar, rows[4])
	})
	t.Run("4 rows", func(t *testing.T) {
		r := startView(t, 80, 4, withFake(noModelFake()))
		rows := r.rows()
		require.Len(t, rows, 4)
		for _, row := range rows {
			require.NotContains(t, row, "◢◤", "the header row gives way first")
		}
		require.True(t, strings.HasPrefix(rows[2], sketchDir), rows[2])
		require.Equal(t, sketchBar, rows[3])
	})
	t.Run("3 rows", func(t *testing.T) {
		r := startView(t, 80, 3, withFake(noModelFake()))
		rows := r.rows()
		require.Len(t, rows, 3)
		require.True(t, strings.HasPrefix(rows[0], " ❯ "), rows[0])
		require.True(t, strings.HasPrefix(rows[1], sketchDir), rows[1])
		require.Equal(t, sketchBar, rows[2])
	})
}

// footerState sets the view to one row of the footer forms of the design.
type footerState struct {
	name string
	set  func(s *sessionView)
	at80 string
	at40 string
}

func TestFooterRowForms(t *testing.T) {
	inProcess := ConnInfo{Mode: ModeInProcess}
	use := func(model string, tokens int64) func(s *sessionView) {
		return func(s *sessionView) { s.modelID, s.ctxTokens = model, tokens }
	}
	// connected is the footer row of a session on gpt-5.4-mini. No link state
	// changes it: the footer row names the work, not the connection.
	connected := footerState{
		at80: ` ⌂ ~/src/askcore                      openai/gpt-5.4-mini ◐ medium  ▱▱▱▱▱▱▱▱ 4%`,
		at40: "                 openai/gpt-5.4-mini 4%",
	}
	withState := func(name string, set func(s *sessionView)) footerState {
		st := connected
		st.name, st.set = name, set
		return st
	}
	states := []footerState{
		withState("connected", use(miniModel, 16_000)),
		{name: "unknown context window", set: use(opus49, 16_000),
			at80: ` ⌂ ~/src/askcore                          anthropic/claude-opus-4-9 ◐ medium  ?`,
			at40: "            anthropic/claude-opus-4-9 ?"},
		{name: "context at 93 percent", set: func(s *sessionView) { s.modelID, s.ctxTokens, s.thinking = sonnet46, 930_000, protocol.ThinkingHigh },
			at80: ` ⌂ ~/src/askcore               anthropic/claude-sonnet-4-6 ◐ high  ▰▰▰▰▰▰▰▱ 93%`,
			at40: "        anthropic/claude-sonnet-4-6 93%"},
		{name: "link not up", set: func(s *sessionView) { s.opening, s.modelID = true, "" },
			at80: ` ⌂ ~/src/askcore                                                    ▱▱▱▱▱▱▱▱ 0%`,
			at40: "                                     0%"},
		withState("leader lost", func(s *sessionView) { use(miniModel, 16_000)(s); s.conn = connDown }),
		withState("in-process", func(s *sessionView) { use(miniModel, 16_000)(s); s.info = inProcess }),
		withState("observer", func(s *sessionView) {
			use(miniModel, 16_000)(s)
			s.sess = SessionInfo{ID: "sess_3f9a00112233", Role: protocol.ACPRoleObserver}
		}),
		{name: "no model", set: func(s *sessionView) { s.modelID, s.haveState = "", true },
			at80: ` ⌂ ~/src/askcore                                                       no model`,
			at40: "                               no model"},
		{name: "long directory gives way", set: func(s *sessionView) {
			s.modelID, s.ctxTokens, s.thinking = sonnet46, 930_000, protocol.ThinkingHigh
			s.cwd = "/home/u/src/github.com/kevinle128/askcore"
		},
			at80: ` ⌂ ~/…/askcore                 anthropic/claude-sonnet-4-6 ◐ high  ▰▰▰▰▰▰▰▱ 93%`,
			at40: "        anthropic/claude-sonnet-4-6 93%"},
		{name: "long model and long directory", set: func(s *sessionView) {
			s.models = append(s.models, protocol.ACPModel{ModelID: tokenPlanFlash, Provider: "alibaba-token-plan", ID: "deepseek-v4.1-flash", ContextWindow: 128_000})
			s.modelID, s.ctxTokens = tokenPlanFlash, 23_040
			s.cwd = "/home/u/src/github.com/kevinle128/askcore"
		},
			at80: ` ⌂ ~/…/askcore             alibaba-token-plan/deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%`,
			at40: "  alibaba-token-plan/deepseek-v4.1… 18%"},
	}
	for _, st := range states {
		t.Run(st.name, func(t *testing.T) {
			for _, c := range []struct {
				w    int
				want string
			}{{80, st.at80}, {40, st.at40}} {
				m := footerModel(t, colorprofile.NoTTY)
				s := m.sv
				s.models = accountRows()
				s.thinking = protocol.ThinkingMedium
				st.set(s)
				got := ansi.Strip(s.footerText(c.w))
				require.Equal(t, c.w, displayWidth(got), "the row fills the width")
				require.True(t, strings.HasSuffix(got, " "), "the row ends with one space")
				require.Equal(t, c.want, strings.TrimRight(got, " "), "%d columns", c.w)
				require.NotContains(t, got, "$", "no cost")
				require.NotContains(t, got, "◢◤", "no logo")
				for _, link := range []string{"●", "◌", "○", "◉", "leader", "starting", "lost", "in-process", "observer"} {
					require.NotContains(t, got, link, "no link state at %d columns", c.w)
				}
			}
		})
	}
}

// The directory never goes from 60 columns: it shortens to its last element,
// and then the end of the model id is cut with the provider whole.
func TestFooterRowNarrowingOrder(t *testing.T) {
	m := footerModel(t, colorprofile.NoTTY)
	s := m.sv
	s.models = append(accountRows(), protocol.ACPModel{ModelID: tokenPlanFlash, Provider: "alibaba-token-plan", ID: "deepseek-v4.1-flash", ContextWindow: 128_000, Reasoning: true})
	s.modelID, s.ctxTokens, s.thinking = tokenPlanFlash, 23_040, protocol.ThinkingHigh
	s.cwd = "/home/u/src/github.com/kevinle128/askcore"
	plain := func(w int) string { return strings.TrimRight(ansi.Strip(s.footerText(w)), " ") }

	require.Equal(t, ` ⌂ ~/…/askcore      alibaba-token-plan/deepseek-v4.1-flash ◐ high  ▰▱▱▱▱▱▱▱ 18%`, plain(80),
		"the directory shortens first and the model text stays whole")
	for w := 60; w <= 80; w++ {
		got := plain(w)
		require.Contains(t, got, "⌂ ~/…/askcore", "the directory stays at %d columns", w)
		require.Contains(t, got, "alibaba-token-plan/", "the provider stays whole at %d columns", w)
		require.LessOrEqual(t, displayWidth(got), w)
	}
	require.Contains(t, plain(70), "alibaba-token-plan/deepseek-v4.1…", "the end of the model id is cut")
}

// In a very narrow terminal the model text is cut from its end, provider
// included, and the row never grows past the width.
func TestFooterRowLastResortCut(t *testing.T) {
	m := footerModel(t, colorprofile.NoTTY)
	s := m.sv
	s.models = append(accountRows(), protocol.ACPModel{ModelID: tokenPlanFlash, Provider: "alibaba-token-plan", ID: "deepseek-v4.1-flash", ContextWindow: 128_000})
	s.modelID, s.ctxTokens = tokenPlanFlash, 23_040
	got := strings.TrimRight(ansi.Strip(s.footerText(24)), " ")
	require.Equal(t, "  alibaba-token-pl… 18%", got)
	for w := 1; w <= 120; w++ {
		require.LessOrEqual(t, displayWidth(s.footerText(w)), w, "width %d", w)
	}
}

// The "?" of an unknown context window is muted: there is no share to rate.
func TestFooterUnknownWindowIsMuted(t *testing.T) {
	m := footerModel(t, colorprofile.TrueColor)
	s := m.sv
	s.models, s.modelID, s.thinking = accountRows(), opus49, protocol.ThinkingMedium
	row := s.footerText(80)
	require.Contains(t, row, m.theme.Render(RoleMuted, "?"))
	require.NotContains(t, ansi.Strip(row), GlyphBarEmpty)
	require.NotContains(t, ansi.Strip(row), "%")
}

// Before the model list is known the footer row keeps its bar, so that a "?"
// does not change to a bar a moment after the start.
func TestFooterKeepsBarBeforeModelList(t *testing.T) {
	m := footerModel(t, colorprofile.NoTTY)
	s := m.sv
	s.models, s.modelID = nil, opus49
	got := ansi.Strip(s.footerText(80))
	require.True(t, strings.HasSuffix(got, "▱▱▱▱▱▱▱▱ 0% "), got)
	require.NotContains(t, got, "?")
	s.models = accountRows()
	require.True(t, strings.HasSuffix(ansi.Strip(s.footerText(80)), "  ? "))
}
