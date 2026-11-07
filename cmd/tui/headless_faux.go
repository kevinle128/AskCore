package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"AskCore/internal/app"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"
)

// fauxPaceEnv sets the tokens per second of the faux replies, so the signal
// lanes have a run to interrupt.
const fauxPaceEnv = "ASK_FAUX_TPS"

// fauxDemo is the H2 demo script. After a tool result it says the result
// text. A prompt "echo <text>" calls the echo tool with <text>, "fail <text>"
// fails with <text>, and any other prompt is said back.
var fauxDemo = faux.Func(func(_ context.Context, c faux.Call) (faux.Step, error) {
	msgs := c.Request.Messages
	if len(msgs) == 0 {
		return faux.Say(""), nil
	}
	switch m := msgs[len(msgs)-1].(type) {
	case protocol.ToolResultMessage:
		return faux.Say(blockText(m.Content)), nil
	case protocol.UserMessage:
		prompt := blockText(m.Content)
		if text, ok := strings.CutPrefix(prompt, "echo "); ok {
			return faux.Reply(faux.ToolCall("echo", map[string]string{"text": text})), nil
		}
		if text, ok := strings.CutPrefix(prompt, "fail "); ok {
			return faux.Fail(text), nil
		}
		return faux.Say(prompt), nil
	default:
		return faux.Say(""), nil
	}
})

// fauxStream returns the model with the given id and a stream function that
// serves fauxDemo on every call.
func fauxStream(modelID string, getenv func(string) string) (providers.StreamFn, providers.Model, error) {
	var tps float64
	if v := getenv(fauxPaceEnv); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			return nil, providers.Model{}, fmt.Errorf("%s must be a non-negative number, got %q", fauxPaceEnv, v)
		}
		tps = f
	}
	p, err := faux.New(faux.WithTokensPerSecond(tps))
	if err != nil {
		return nil, providers.Model{}, err
	}
	m, ok := p.Model(modelID)
	if !ok {
		return nil, providers.Model{}, fmt.Errorf("model %q not found for provider %q", modelID, defaultProvider)
	}
	stream := func(ctx context.Context, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
		p.Set(fauxDemo)
		return p.Stream(ctx, m, req, opts)
	}
	return stream, m, nil
}

// openProvider returns the stream and the model of a headless run. Only the
// demo provider has a stream of its own. Every other provider, Token Plan too,
// resolves through the model list of view (the cache, the catalog and the user
// models.json) and returns a nil stream: the native wires of the agent serve
// the row by its API. It makes no network call.
func openProvider(o options, getenv func(string) string, view *app.ModelView) (providers.StreamFn, providers.Model, error) {
	if o.provider == defaultProvider {
		return fauxStream(o.model, getenv)
	}
	set := view.Set()
	if set == nil {
		return nil, providers.Model{}, fmt.Errorf("provider %q is not available", o.provider)
	}
	ref := providers.Ref{Provider: o.provider, ID: o.model}
	m, err := set.Find(ref)
	if errors.Is(err, providers.ErrAmbiguousModel) && o.provider == anthropic.ProviderID {
		// A Token Plan model is on two wires; print mode uses the Messages
		// wire, as it always did.
		ref.API = providers.APIAnthropicMessages
		m, err = set.Find(ref)
	}
	if err != nil {
		if !slices.Contains(providerNames(set), o.provider) {
			return nil, providers.Model{}, fmt.Errorf("provider %q is not available", o.provider)
		}
		return nil, providers.Model{}, err
	}
	return nil, m, nil
}

func providerNames(set *providers.ModelSet) []string {
	var out []string
	for _, m := range set.All() {
		if !slices.Contains(out, m.Provider) {
			out = append(out, m.Provider)
		}
	}
	return out
}

func blockText(blocks []protocol.UserBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		if t, ok := blk.(protocol.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}
