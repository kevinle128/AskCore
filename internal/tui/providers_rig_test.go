package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// Fixtures of the provider screens. The fake keys and codes are placeholders;
// they exist so that tests can search every output for them.
const (
	fakeKey     = "sk-test-0000000000-fake"
	fakeCode    = "fake-code-7f3a91c0"
	fakeAddress = "http://127.0.0.1:1455/callback?code=fake-code-7f3a91c0&state=Zk3pQ9xT"
	fakeURL     = "https://auth.example.test/oauth/authorize?response_type=code&client_id=ask-cli&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&scope=openid+profile&state=Zk3pQ9xT&code_challenge=Vt8f0aLm2sQe&code_challenge_method=S256"

	tokenPlanFlash = "alibaba-token-plan/deepseek-v4.1-flash@anthropic-messages"
	tokenPlanChat  = "alibaba-token-plan/deepseek-v4.1-flash@openai-completions"
	openaiGPT55    = "openai/gpt-5.5@responses"
	openaiGPT56    = "openai/gpt-5.6-sol@responses"
)

// designOptions are the seven login options of the design, with the states of
// its sketches.
func designOptions() []protocol.ACPLoginOption {
	return []protocol.ACPLoginOption{
		{ProviderID: "alibaba-token-plan", MethodID: "api-key", Name: "Alibaba Token Plan API key", State: protocol.ACPLoginStateAPIKey},
		{ProviderID: "anthropic", MethodID: "anthropic-oauth", Name: "Claude Pro/Max sign-in", State: protocol.ACPLoginStateNone},
		{ProviderID: "anthropic", MethodID: "api-key", Name: "Anthropic API key", State: protocol.ACPLoginStateEnv, EnvVar: "ANTHROPIC_API_KEY"},
		{ProviderID: "openai", MethodID: "openai-chatgpt", Name: "ChatGPT Plus/Pro sign-in", State: protocol.ACPLoginStateSignedIn},
		{ProviderID: "openai", MethodID: "api-key", Name: "OpenAI API key", State: protocol.ACPLoginStateNone},
		{ProviderID: "xai", MethodID: "xai-oauth", Name: "xAI Grok sign-in", State: protocol.ACPLoginStateNone},
		{ProviderID: "xai", MethodID: "api-key", Name: "xAI API key", State: protocol.ACPLoginStateNone},
	}
}

// designModels are the models of the sketches: one provider with a model on
// two wire formats, and one with two models.
func designModels(withOpenAI bool) []protocol.ACPModel {
	rows := []protocol.ACPModel{
		{ModelID: tokenPlanFlash, Provider: "alibaba-token-plan", ID: "deepseek-v4.1-flash", API: "anthropic-messages", ContextWindow: 1_000_000},
		{ModelID: tokenPlanChat, Provider: "alibaba-token-plan", ID: "deepseek-v4.1-flash", API: "openai-completions", ContextWindow: 1_000_000, Reasoning: true},
	}
	if withOpenAI {
		rows = append(rows,
			protocol.ACPModel{ModelID: openaiGPT55, Provider: "openai", ID: "gpt-5.5", API: "responses", ContextWindow: 1_000_000, Reasoning: true},
			protocol.ACPModel{ModelID: openaiGPT56, Provider: "openai", ID: "gpt-5.6-sol", API: "responses", ContextWindow: 1_000_000, Reasoning: true},
		)
	}
	return rows
}

// singleFormatModels are models with one wire format each.
func singleFormatModels() []protocol.ACPModel {
	return []protocol.ACPModel{
		{ModelID: "alibaba-token-plan/deepseek-v4.1-flash@chat", Provider: "alibaba-token-plan", ID: "deepseek-v4.1-flash", API: "chat", ContextWindow: 128_000},
		openaiRow("gpt-5.5"),
		openaiRow("gpt-5.6-sol"),
	}
}

func openaiRow(id string) protocol.ACPModel {
	return protocol.ACPModel{ModelID: "openai/" + id + "@responses", Provider: "openai", ID: id, API: "responses", ContextWindow: 1_000_000, Reasoning: true}
}

const deepseekChat = "alibaba-token-plan/deepseek-v4.1-flash@chat"

// providerFake is a leader fake for the provider screens: single-format models
// with the filtered answer, and the design's seven options.
func providerFake() *fakeClient {
	fc := newFakeClient()
	fc.models = protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels()[:1], UsableOnly: true}
	fc.state = protocol.ACPStateResult{SessionID: testSessionID, ModelID: deepseekChat, ThinkingLevel: protocol.ThinkingOff}
	fc.providers = protocol.ACPProvidersResult{Generation: 1, Options: designOptions()}
	return fc
}

// noModelFake is the first run: nothing is usable.
func noModelFake() *fakeClient {
	fc := newFakeClient()
	fc.models = protocol.ACPModelsResult{UsableOnly: true}
	fc.state = protocol.ACPStateResult{SessionID: testSessionID}
	fc.providers = protocol.ACPProvidersResult{Generation: 1, Options: designOptions()}
	for i := range fc.providers.Options {
		fc.providers.Options[i].State = protocol.ACPLoginStateNone
		fc.providers.Options[i].EnvVar = ""
	}
	return fc
}

// setModels replaces the model list that the fake answers.
func (f *fakeClient) setModels(res protocol.ACPModelsResult) {
	f.mu.Lock()
	f.models = res
	f.mu.Unlock()
}

// setState replaces the state that the fake answers.
func (f *fakeClient) setState(st protocol.ACPStateResult) {
	f.mu.Lock()
	f.state = st
	f.mu.Unlock()
}

// callsWith lists the recorded calls that start with a prefix.
func (f *fakeClient) callsWith(prefix string) []string {
	var out []string
	for _, c := range f.callList() {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// openProviders types /providers and waits for the list.
func (r *viewRig) openProviders() {
	r.t.Helper()
	r.typeText("/providers")
	r.key("enter")
	r.h.waitText("Type to search")
}

// paste sends a bracketed paste.
func (r *viewRig) paste(text string) {
	r.t.Helper()
	r.h.send(tea.PasteMsg{Content: text})
	r.h.settle()
}

// rows returns the settled rows with the trailing spaces removed.
func (r *viewRig) rows() []string {
	r.t.Helper()
	out := r.h.settle()
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

// content returns the settled rows without the rule under the header and
// without blank rows above and below: the header row comes first, then the
// rows of the pane.
func (r *viewRig) content() []string {
	r.t.Helper()
	rows := r.rows()
	if len(rows) > 1 {
		rows = append(rows[:1:1], rows[2:]...)
	}
	for len(rows) > 0 && rows[0] == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// railRows returns the n rows above the prompt pane: the queue and the status.
func (r *viewRig) railRows(n int) []string {
	r.t.Helper()
	rows := r.content()
	// The prompt pane is a rule, the prompt rows and a rule above the bar.
	top := -1
	for i := len(rows) - 3; i >= 0; i-- {
		if rows[i] != "" && strings.Trim(rows[i], "─") == "" {
			top = i
			break
		}
	}
	require.GreaterOrEqual(r.t, top, n, "the prompt pane has a rule above it")
	return rows[top-n : top]
}

// screen returns the settled screen as one text.
func (r *viewRig) screen() string { return strings.Join(r.rows(), "\n") }

// requireOnScreen fails when a text is not on the settled screen.
func (r *viewRig) requireOnScreen(texts ...string) {
	r.t.Helper()
	screen := r.screen()
	for _, text := range texts {
		require.Contains(r.t, screen, text)
	}
}

// requireNotOnScreen fails when a text is on the settled screen.
func (r *viewRig) requireNotOnScreen(texts ...string) {
	r.t.Helper()
	screen := r.screen()
	for _, text := range texts {
		require.NotContains(r.t, screen, text)
	}
}
