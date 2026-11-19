package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// Session usage in the footer row and the error blocks of an overflow. The
// sizes and texts follow the approved sketches of the usage design.

const (
	modelSonnet55  = "anthropic/claude-sonnet-5-5@messages"
	modelAlibaba   = "alibaba/deepseek-v4.1-flash@messages"
	modelQwenLocal = "lmstudio/qwen3-coder@chat"
)

// usageRows are the account rows plus the models of the usage sketches.
func usageRows() []protocol.ACPModel {
	row := func(id, provider, name string, window int, reasoning bool) protocol.ACPModel {
		return protocol.ACPModel{ModelID: id, Provider: provider, ID: name, ContextWindow: window, Reasoning: reasoning}
	}
	return append(accountRows(),
		row(modelSonnet55, "anthropic", "claude-sonnet-5-5", 1_000_000, true),
		row(tokenPlanFlash, "alibaba-token-plan", "deepseek-v4.1-flash", 1_000_000, true),
		row(modelAlibaba, "alibaba", "deepseek-v4.1-flash", 1_000_000, true),
		row(modelQwenLocal, "lmstudio", "qwen3-coder", 0, false),
	)
}

// Usage totals of the sketches: the context estimate is 179,700 tokens, 18% of
// a window of 1,000,000.
var (
	pricedTotals = protocol.UsageTotals{
		Total:    protocol.Usage{Input: 274_200, Output: 3_400, Cost: protocol.Cost{Total: 420_000}},
		Complete: true, CostComplete: true, ContextTokens: 179_700,
	}
	partialCostTotals = protocol.UsageTotals{
		Total:         protocol.Usage{Input: 274_200, Output: 3_400, Cost: protocol.Cost{Total: 420_000}},
		ContextTokens: 179_700,
	}
	tokenTotals = protocol.UsageTotals{
		Total:    protocol.Usage{Input: 275_000, Output: 3_400},
		Complete: true, ContextTokens: 179_700,
	}
	partialTokenTotals = protocol.UsageTotals{
		Total:         protocol.Usage{Input: 275_000, Output: 3_400},
		ContextTokens: 179_700,
	}
	// freeTotals is a free model after a reply: tokens, no cost, nothing missing.
	freeTotals = protocol.UsageTotals{
		Total:    protocol.Usage{Input: 275_000, Output: 3_400},
		Complete: true, CostComplete: true, ContextTokens: 179_700,
	}
)

// usageFake is a leader that runs on model with a thinking level.
func usageFake(model string, level protocol.ThinkingLevel) *fakeClient {
	fc := accountFake(model, usageRows())
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: model, ThinkingLevel: level})
	return fc
}

// usageTurns plays the two replies of the sketches and ends the run with the
// usage totals. name is the model name that the meta lines show; priced adds
// the cost to them.
func (r *viewRig) usageTurns(name string, priced bool, totals protocol.UsageTotals) {
	r.t.Helper()
	cost := func(micro int64) protocol.Cost {
		if priced {
			return protocol.Cost{Total: micro}
		}
		return protocol.Cost{}
	}
	first := protocol.Usage{Input: 96_200, Output: 2_100, Cost: cost(150_000)}
	second := protocol.Usage{Input: 178_000, Output: 1_300, Cost: cost(270_000)}
	r.event(flat(
		&protocol.AgentStart{},
		userAt("why does TestLeaderStale fail on CI?", sketchTime(14, 2)),
		assistantStart(name, 0),
		textParts(0, "The test waits for the stale socket check, but CI runs it with a 1 s deadline."),
		assistantEndUsage(name, protocol.StopStop, 6100, first, protocol.Text{Text: "The test waits for the stale socket check, but CI runs it with a 1 s deadline."}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
		&protocol.AgentStart{},
		userAt("raise the deadline to 5 s and rerun it", sketchTime(14, 5)),
		assistantStart(name, 0),
		textParts(0, "Done. TestLeaderStale passes in 1.8 s on the CI image."),
		assistantEndUsage(name, protocol.StopStop, 5400, second, protocol.Text{Text: "Done. TestLeaderStale passes in 1.8 s on the CI image."}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{Usage: &totals},
	)...)
	r.h.waitText("Done. TestLeaderStale passes")
}

// usageScreen is a session on model after two replies that ended with totals.
func usageScreen(t *testing.T, w, h int, model, name string, level protocol.ThinkingLevel, priced bool, totals protocol.UsageTotals) *viewRig {
	t.Helper()
	r := startView(t, w, h, withFake(usageFake(model, level)))
	r.usageTurns(name, priced, totals)
	r.h.settle()
	return r
}

func TestFooterShowsSessionCost(t *testing.T) {
	r := usageScreen(t, 80, 24, openaiGPT55, "gpt-5.5", protocol.ThinkingHigh, true, pricedTotals)
	requireContentGolden(t, r.h)
	require.Equal(t, ` ⌂ ~/src/askcore                     openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18%`, r.footerOf())
}

func TestFooterMarksPartialCost(t *testing.T) {
	r := usageScreen(t, 80, 24, openaiGPT55, "gpt-5.5", protocol.ThinkingHigh, true, partialCostTotals)
	requireContentGolden(t, r.h)
	require.Equal(t, ` ⌂ ~/src/askcore                   openai/gpt-5.5 ◐ high  ≥ $0.42  ▰▱▱▱▱▱▱▱ 18%`, r.footerOf())
}

func TestFooterShowsTokensWithoutPrice(t *testing.T) {
	r := usageScreen(t, 80, 24, modelAlibaba, "deepseek-v4.1-flash", protocol.ThinkingHigh, false, tokenTotals)
	requireContentGolden(t, r.h)
	require.Equal(t, ` ⌂ ~/src/askcore  alibaba/deepseek-v4.1-flash ◐ high  ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18%`, r.footerOf())
	require.NotContains(t, r.footerOf(), "$")
}

func TestFooterMarksPartialTokens(t *testing.T) {
	r := usageScreen(t, 80, 24, modelAlibaba, "deepseek-v4.1-flash", protocol.ThinkingHigh, false, partialTokenTotals)
	requireContentGolden(t, r.h)
	require.Equal(t, ` ⌂ ~/…/askcore  alibaba/deepseek-v4.1-flash ◐ high  ≥ ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18%`, r.footerOf())
}

func TestFooterShowsNoUsageBeforeFirstReply(t *testing.T) {
	r := startView(t, 80, 24, withFake(usageFake(openaiGPT55, protocol.ThinkingHigh)))
	requireContentGolden(t, r.h)
	row := r.footerOf()
	require.Equal(t, ` ⌂ ~/src/askcore                             openai/gpt-5.5 ◐ high  ▱▱▱▱▱▱▱▱ 0%`, row)
	require.NotContains(t, row, "$")
	require.NotContains(t, row, "↑")
}

// The estimate that the leader answers sets the meter, with no event and no
// reply on the screen.
func TestFooterUsesServerContextEstimate(t *testing.T) {
	fc := usageFake(openaiGPT55, protocol.ThinkingHigh)
	fc.setUsage(protocol.UsageTotals{ContextTokens: 180_000})
	r := startView(t, 80, 24, withFake(fc))
	r.h.waitText("▰▱▱▱▱▱▱▱ 18%")
	require.Equal(t, ` ⌂ ~/src/askcore                            openai/gpt-5.5 ◐ high  ▰▱▱▱▱▱▱▱ 18%`, r.footerOf())

	// After a turn of a running session the same call gives the new estimate.
	fc.setUsage(protocol.UsageTotals{ContextTokens: 500_000})
	r.event(&protocol.AgentStart{}, &protocol.TurnEnd{})
	r.h.waitText("▰▰▰▰▱▱▱▱ 50%")
}

func TestFooterShowsUnknownWindowWithCost(t *testing.T) {
	r := usageScreen(t, 80, 24, modelQwenLocal, "qwen3-coder", protocol.ThinkingOff, false, freeTotals)
	requireContentGolden(t, r.h)
	require.Equal(t, ` ⌂ ~/src/askcore                                 lmstudio/qwen3-coder  $0.00  ?`, r.footerOf())
	require.NotContains(t, r.footerOf(), "▱")
}

func TestFooterShowsOverWindowEstimate(t *testing.T) {
	m := footerModel(t, colorprofile.TrueColor)
	s := m.sv
	s.models = usageRows()
	s.modelID, s.thinking = openaiGPT55, protocol.ThinkingHigh
	over := pricedTotals
	over.ContextTokens = 1_010_000
	s.usage = over
	require.Contains(t, s.footerText(80), m.theme.Render(RoleError, "▰▰▰▰▰▰▰▰ 101%"))
	over.ContextTokens = 50_000_000
	s.usage = over
	require.Contains(t, ansi.Strip(s.footerText(80)), "▰▰▰▰▰▰▰▰ 999%", "the share stops at 999%")
}

// usageFooterRow is the footer row for a model and totals at a width.
func usageFooterRow(t *testing.T, w int, model string, level protocol.ThinkingLevel, u protocol.UsageTotals) string {
	t.Helper()
	m := footerModel(t, colorprofile.NoTTY)
	s := m.sv
	s.models = usageRows()
	s.modelID, s.thinking, s.usage = model, level, u
	got := ansi.Strip(s.footerText(w))
	require.Equal(t, w, displayWidth(got), "the row fills the width")
	return strings.TrimRight(got, " ")
}

func TestFooterCutsLongIDBeforeCost(t *testing.T) {
	require.Equal(t,
		` ⌂ ~/…/askcore alibaba-token-plan/deepseek-v4.… ◐ medium  ≥ $0.42  ▰▱▱▱▱▱▱▱ 18%`,
		usageFooterRow(t, 80, tokenPlanFlash, protocol.ThinkingMedium, partialCostTotals))
}

// The usage part drops when the id would keep fewer than 6 columns.
func TestFooterDropsUsageForLongID(t *testing.T) {
	require.Equal(t,
		` ⌂ ~/…/askcore alibaba-token-plan/deepseek-v4… ◐ medium  ▰▱▱▱▱▱▱▱ 18%`,
		usageFooterRow(t, 70, tokenPlanFlash, protocol.ThinkingMedium, partialCostTotals))
	// With the end of the id cut to 6 columns the part still stays.
	require.Equal(t,
		` ⌂ ~/…/askcore alibaba-token-plan/deepse… ◐ medium  ≥ ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18%`,
		usageFooterRow(t, 80, tokenPlanFlash, protocol.ThinkingMedium, partialTokenTotals))
}

func TestFooterUsageByWidth(t *testing.T) {
	const (
		priced        = "priced"
		partialTokens = "partialTokens"
		partialCost   = "partialCost"
	)
	totals := map[string]protocol.UsageTotals{priced: pricedTotals, partialTokens: partialTokenTotals, partialCost: partialCostTotals}
	models := map[string]string{
		"openai/gpt-5.5":                         openaiGPT55,
		"anthropic/claude-sonnet-5-5":            modelSonnet55,
		"alibaba-token-plan/deepseek-v4.1-flash": tokenPlanFlash,
	}
	cases := []struct {
		w     int
		kind  string
		model string
		want  string
	}{
		{120, priced, "openai/gpt-5.5", " ⌂ ~/src/askcore                                                             openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18%"},
		{100, priced, "openai/gpt-5.5", " ⌂ ~/src/askcore                                         openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18%"},
		{80, priced, "openai/gpt-5.5", " ⌂ ~/src/askcore                     openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18%"},
		{70, priced, "openai/gpt-5.5", " ⌂ ~/src/askcore           openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18%"},
		{60, priced, "openai/gpt-5.5", " ⌂ ~/src/askcore openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18%"},
		{59, priced, "openai/gpt-5.5", "                                        openai/gpt-5.5 18%"},
		{40, priced, "openai/gpt-5.5", "                     openai/gpt-5.5 18%"},
		{80, partialTokens, "anthropic/claude-sonnet-5-5", " ⌂ ~/…/askcore anthropic/claude-sonnet-5… ◐ medium  ≥ ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18%"},
		{70, partialTokens, "anthropic/claude-sonnet-5-5", " ⌂ ~/…/askcore anthropic/claud… ◐ medium  ≥ ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18%"},
		{60, partialTokens, "anthropic/claude-sonnet-5-5", " ⌂ ~/…/askcore anthropic/claude-son… ◐ medium  ▰▱▱▱▱▱▱▱ 18%"},
		{80, partialCost, "alibaba-token-plan/deepseek-v4.1-flash", " ⌂ ~/…/askcore alibaba-token-plan/deepseek-v4.… ◐ medium  ≥ $0.42  ▰▱▱▱▱▱▱▱ 18%"},
		{70, partialCost, "alibaba-token-plan/deepseek-v4.1-flash", " ⌂ ~/…/askcore alibaba-token-plan/deepseek-v4… ◐ medium  ▰▱▱▱▱▱▱▱ 18%"},
		{60, partialCost, "alibaba-token-plan/deepseek-v4.1-flash", " ⌂ ~/…/askcore alibaba-token-plan/d… ◐ medium  ▰▱▱▱▱▱▱▱ 18%"},
	}
	for _, c := range cases {
		level := protocol.ThinkingMedium
		if c.model == "openai/gpt-5.5" {
			level = protocol.ThinkingHigh
		}
		name := c.model + " " + c.kind
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.want, usageFooterRow(t, c.w, models[c.model], level, totals[c.kind]), "%d columns", c.w)
		})
	}
}

// A resize across 60 columns removes the directory and the usage part and keeps
// the share.
func TestFooterUsageSurvivesResize(t *testing.T) {
	r := usageScreen(t, 80, 24, openaiGPT55, "gpt-5.5", protocol.ThinkingHigh, true, pricedTotals)
	require.Contains(t, r.footerOf(), "$0.42")
	r.h.resize(40, 14)
	r.h.waitFor("the short footer row", func(*screenHarness) bool { return !strings.Contains(r.footerOf(), "$0.42") })
	require.Equal(t, "                     openai/gpt-5.5 18%", r.footerOf())
}

func TestFooterIgnoresStaleUsageResult(t *testing.T) {
	m := footerModel(t, colorprofile.NoTTY)
	s := m.sv
	s.followed = testSessionID
	s.usage = pricedTotals
	before := ansi.Strip(s.footerText(80))
	cheaper := protocol.UsageTotals{Total: protocol.Usage{Input: 1, Cost: protocol.Cost{Total: 1_000}}, CostComplete: true}
	fresh := usageMsg{gen: s.gen, runSeq: s.run.seq, token: 5, session: testSessionID, totals: &cheaper}
	for name, msg := range map[string]usageMsg{
		"older run":     {gen: s.gen, runSeq: s.run.seq - 1, token: 5, session: testSessionID, totals: &cheaper},
		"older client":  {gen: s.gen + 1, runSeq: s.run.seq, token: 5, session: testSessionID, totals: &cheaper},
		"other session": {gen: s.gen, runSeq: s.run.seq, token: 5, session: "other", totals: &cheaper},
	} {
		s.onUsage(msg)
		require.Equal(t, before, ansi.Strip(s.footerText(80)), name)
	}
	s.onUsage(fresh)
	require.Contains(t, ansi.Strip(s.footerText(80)), "$0.001", "a current answer is used")
	// An answer of an earlier call does not replace a later one.
	s.onUsage(usageMsg{gen: s.gen, runSeq: s.run.seq, token: 4, session: testSessionID, totals: &pricedTotals})
	require.Contains(t, ansi.Strip(s.footerText(80)), "$0.001", "late answer")
}

func TestFooterKeepsValuesWhenUsageFails(t *testing.T) {
	r := usageScreen(t, 80, 24, openaiGPT55, "gpt-5.5", protocol.ThinkingHigh, true, pricedTotals)
	before := r.footerOf()
	r.fc.mu.Lock()
	r.fc.usageErr = errors.New("leader gone")
	r.fc.mu.Unlock()
	calls := r.fc.count("usage")
	r.event(&protocol.AgentStart{}, &protocol.TurnEnd{})
	r.h.waitFor("the usage call", func(*screenHarness) bool { return r.fc.count("usage") > calls })
	r.h.settle()
	require.Equal(t, before, r.footerOf())
	require.Zero(t, r.h.count("leader gone"), "the failure has no text")
}

func TestFooterClearsUsageOnNewSession(t *testing.T) {
	fc := usageFake(openaiGPT55, protocol.ThinkingHigh)
	r := startView(t, 80, 24, withFake(fc))
	r.usageTurns("gpt-5.5", true, pricedTotals)
	require.Contains(t, r.footerOf(), "$0.42")
	fc.setUsage(protocol.UsageTotals{})
	fc.mu.Lock()
	fc.sessionID = "sess_second"
	fc.mu.Unlock()
	r.typeText("/new")
	r.key("enter")
	r.h.waitFor("the footer row without usage", func(*screenHarness) bool { return !strings.Contains(r.footerOf(), "$0.42") })
	require.Equal(t, ` ⌂ ~/src/askcore                             openai/gpt-5.5 ◐ high  ▱▱▱▱▱▱▱▱ 0%`, r.footerOf())
}

func TestFooterUsagePartRules(t *testing.T) {
	tok := func(in, out int64) protocol.Usage { return protocol.Usage{Input: in, Output: out} }
	cost := func(micro int64) protocol.Usage { return protocol.Usage{Input: 1, Cost: protocol.Cost{Total: micro}} }
	cases := []struct {
		name string
		u    protocol.UsageTotals
		want string
	}{
		{"nothing known", protocol.UsageTotals{}, ""},
		{"only refused attempts", protocol.UsageTotals{Complete: true, CostComplete: true}, ""},
		{"free model", protocol.UsageTotals{Total: tok(10, 5), Complete: true, CostComplete: true}, "$0.00"},
		{"under one cent", protocol.UsageTotals{Total: cost(4_000), Complete: true, CostComplete: true}, "$0.004"},
		{"cents", protocol.UsageTotals{Total: cost(420_000), Complete: true, CostComplete: true}, "$0.42"},
		{"rounds up to a hundred dollars", protocol.UsageTotals{Total: cost(99_996_000), Complete: true, CostComplete: true}, "$100"},
		{"whole dollars", protocol.UsageTotals{Total: cost(123_400_000), Complete: true, CostComplete: true}, "$123"},
		{"partial cost", protocol.UsageTotals{Total: cost(420_000), Complete: true}, "≥ $0.42"},
		{"no price", protocol.UsageTotals{Total: tok(275_000, 3_400), Complete: true}, "↑275k ↓3.4k"},
		{"no price and a missing attempt", protocol.UsageTotals{Total: tok(275_000, 3_400)}, "≥ ↑275k ↓3.4k"},
		{"cost only from cache", protocol.UsageTotals{Total: protocol.Usage{CacheRead: 9}, Complete: true, CostComplete: true}, "$0.00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := footerModel(t, colorprofile.NoTTY)
			m.sv.usage = c.u
			require.Equal(t, c.want, m.sv.usageText())
		})
	}
}

// ---- overflow, quota and retry ----

const overflowPrompt = "summarize every file in internal/ and docs/"

// failedReply is a reply of a provider that refused the request.
func failedReply(provider, text string) protocol.Event {
	return &protocol.MessageEnd{Envelope: at(0), Message: protocol.AssistantMessage{
		Provider: provider, Model: "gpt-5.5", StopReason: protocol.StopError, ErrorMessage: &text,
	}}
}

// overflowTotals is the usage after a request over the window: the estimate is
// the last good usage plus the new prompt.
var overflowTotals = protocol.UsageTotals{
	Total:    protocol.Usage{Input: 1_000_000, Output: 4_100, Cost: protocol.Cost{Total: 1_840_000}},
	Complete: true, CostComplete: true, ContextTokens: 1_010_000,
}

func overflowRig(t *testing.T, w, h int) *viewRig {
	t.Helper()
	return startView(t, w, h, withFake(usageFake(openaiGPT55, protocol.ThinkingHigh)))
}

func (r *viewRig) explicitOverflow() {
	r.t.Helper()
	r.event(flat(
		&protocol.AgentStart{},
		userAt(overflowPrompt, sketchTime(14, 11)),
		assistantStart("gpt-5.5", 0),
		failedReply("openai", "HTTP 400: context_length_exceeded: maximum context length is 1000000 tokens"),
		&protocol.CycleEnd{Reason: "error", Code: "CONTEXT_WINDOW_EXCEEDED"}, &protocol.AgentEnd{},
		&protocol.AgentSettled{Usage: &overflowTotals},
	)...)
	r.h.waitText("Then send again.")
}

func (r *viewRig) silentOverflow() {
	r.t.Helper()
	reply := "The internal/ tree has 41 packages. The agent, pipeline and sessions packages hold the runtime core."
	used := protocol.Usage{Input: 1_000_000, Output: 4_100, Cost: protocol.Cost{Total: 2_100_000}}
	totals := overflowTotals
	totals.Total.Cost.Total = 2_970_000
	r.event(flat(
		&protocol.AgentStart{},
		userAt(overflowPrompt, sketchTime(14, 11)),
		assistantStart("gpt-5.5", 0),
		textParts(0, reply),
		assistantEndUsage("gpt-5.5", protocol.StopStop, 41_000, used, protocol.Text{Text: reply}),
		&protocol.CycleEnd{Reason: "error", Code: "CONTEXT_WINDOW_EXCEEDED"}, &protocol.AgentEnd{},
		&protocol.AgentSettled{Usage: &totals},
	)...)
	r.h.waitText("Then send again.")
}

func TestExplicitOverflowShowsContextBlock(t *testing.T) {
	r := overflowRig(t, 80, 24)
	r.explicitOverflow()
	requireContentGolden(t, r.h)
	rows := strings.Join(r.rows(), "\n")
	require.Contains(t, rows, "Error (context): The conversation is larger than the context window of the")
	require.Contains(t, rows, "Start a new session with /new, or choose a model with a larger window")
	require.NotContains(t, rows, "context_length_exceeded", "the provider sentence does not show")
	require.Contains(t, r.footerOf(), "▰▰▰▰▰▰▰▰ 101%")
}

func TestSilentOverflowShowsContextBlock(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		r := overflowRig(t, 80, 24)
		r.silentOverflow()
		requireContentGolden(t, r.h)
		rows := strings.Join(r.rows(), "\n")
		require.Contains(t, rows, "The internal/ tree has 41 packages.")
		require.Contains(t, rows, "Error (context): The provider reported more input tokens than the context")
		require.NotContains(t, rows, "The request failed.")
		require.Contains(t, r.footerOf(), "▰▰▰▰▰▰▰▰ 101%")
	})
	t.Run("40x14", func(t *testing.T) {
		r := overflowRig(t, 40, 14)
		r.silentOverflow()
		requireContentGolden(t, r.h)
		require.NotContains(t, strings.Join(r.rows(), "\n"), "The request failed.")
	})
}

func (r *viewRig) quotaFailure() {
	r.t.Helper()
	r.event(flat(
		&protocol.AgentStart{},
		userAt(overflowPrompt, sketchTime(14, 11)),
		assistantStart("gpt-5.5", 0),
		failedReply("openai", "HTTP 402: insufficient_quota"),
		&protocol.CycleEnd{Reason: "error", Code: "QUOTA"}, &protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
	r.h.waitText("Check the plan or the credit of the account")
}

func TestQuotaRetryAndOverflowLookDifferent(t *testing.T) {
	screens := map[string]string{}
	t.Run("quota", func(t *testing.T) {
		r := overflowRig(t, 80, 24)
		r.quotaFailure()
		requireContentGolden(t, r.h)
		screens["quota"] = strings.Join(r.rows(), "\n")
	})
	t.Run("retry", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(usageFake(openaiGPT55, protocol.ThinkingHigh)), withOptions(func(o *Options) { o.TickInterval = 10 * time.Millisecond }))
		r.event(flat(&protocol.AgentStart{}, userAt(overflowPrompt, sketchTime(14, 11)))...)
		r.event(&protocol.AutoRetryStart{Attempt: 1, MaxAttempts: 5, DelayMs: 4000, ErrorMessage: "HTTP 429: rate limit"})
		r.h.waitText("┊ ◌ Retrying (1/5) in 4s · rate limit · esc to cancel")
		requireContentGolden(t, r.h)
		screens["retry"] = strings.Join(r.rows(), "\n")
		r.event(&protocol.AutoRetryEnd{Success: true, Attempt: 1})
		r.h.waitText("Working…")
	})
	t.Run("overflow", func(t *testing.T) {
		r := overflowRig(t, 80, 24)
		r.explicitOverflow()
		screens["overflow"] = strings.Join(r.rows(), "\n")
	})
	require.Contains(t, screens["quota"], "✗ Error (quota)")
	require.NotContains(t, screens["quota"], "Error (context)")
	require.Contains(t, screens["retry"], "↻ Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 5 scheduled.")
	require.Contains(t, screens["retry"], "Retrying (1/5)")
	require.NotContains(t, screens["retry"], "✗ Error")
	require.Contains(t, screens["overflow"], "✗ Error (context)")
	require.NotContains(t, screens["overflow"], "Error (quota)")
	require.NotEqual(t, screens["quota"], screens["overflow"])
	require.NotEqual(t, screens["quota"], screens["retry"])
	require.NotEqual(t, screens["retry"], screens["overflow"])
}
