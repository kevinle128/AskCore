package anthropic

import (
	"context"
	"net/http"
	"unicode/utf8"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// Prepare computes the effective values of a request once: the thinking level
// and its effort word, the output limit after the clamp, and the endpoint and
// header names that the request has. It reads no credential. The size estimate
// of the clamp counts the document without the shaping of the subscription
// profile, because the binding resolves later; that shaping adds about fifteen
// tokens, which the model limit and the context margin absorb.
func (p *adapter) Prepare(m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) (*providers.Prepared, error) {
	tm, err := prepareModel(m)
	if err != nil {
		return nil, err
	}
	msgs := providers.TransformMessages(req.Messages, tm, p.cfg.now, p.cfg.normalize)
	pr, err := p.compute(tm, msgs, opts)
	if err != nil {
		return nil, err
	}
	enc, err := p.Encode(context.Background(), tm, pr, req, providers.AuthBinding{})
	if err != nil {
		return nil, err
	}
	pr.SetWireFacts(enc)
	return pr, nil
}

// compute makes the Prepared of a request from the transformed messages.
func (p *adapter) compute(tm providers.Model, msgs []protocol.Message, opts providers.StreamOptions) (*providers.Prepared, error) {
	level, err := effectiveThinking(tm, opts)
	if err != nil {
		return nil, err
	}
	pr := &providers.Prepared{
		Provider:       tm.Provider,
		API:            string(tm.API),
		Model:          tm.ID,
		Thinking:       level,
		ToolChoice:     opts.ToolChoice,
		CacheRetention: opts.CacheRetention,
	}
	if opts.Temperature != nil {
		t := *opts.Temperature
		pr.Temperature = &t
	}
	if level != protocol.ThinkingOff {
		pr.Effort = effortOf(level)
	}
	doc, _, err := buildDocument(msgs, tm, pr, false)
	if err != nil {
		return nil, err
	}
	pr.MaxTokens = clampMaxTokens(tm, opts, utf8.RuneCount(doc.body)/4)
	return pr, nil
}

// Encode builds the request that Stream sends for pr and binding, with the
// real adapter code and no network.
func (p *adapter) Encode(ctx context.Context, m providers.Model, pr *providers.Prepared, req providers.TranscriptRequest, binding providers.AuthBinding) (*providers.Encoded, error) {
	tm, err := prepareModel(m)
	if err != nil {
		return nil, err
	}
	return providers.DryRun(ctx, func(client *http.Client) *providers.Stream {
		q := &adapter{cfg: p.cfg}
		q.cfg.http = client
		return q.Stream(ctx, tm, req, providers.DryRunOptions(binding, tm.BaseURL, pr))
	})
}
