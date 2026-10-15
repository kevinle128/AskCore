package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"unicode/utf8"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// prepareBase fills the identity and the options that every Api copies.
func prepareBase(tm providers.Model, opts providers.StreamOptions) *providers.Prepared {
	pr := &providers.Prepared{
		Provider:       tm.Provider,
		API:            string(tm.API),
		Model:          tm.ID,
		ToolChoice:     opts.ToolChoice,
		CacheRetention: opts.CacheRetention,
	}
	if opts.Temperature != nil {
		t := *opts.Temperature
		pr.Temperature = &t
	}
	return pr
}

// Prepare computes the effective values of a Completions request once: the
// thinking level and its effort word, the output limit after the clamp, and
// the endpoint and header names that the request has. It reads no credential.
func (p *completions) Prepare(m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) (*providers.Prepared, error) {
	tm, err := prepareCompletionsModel(m)
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

func (p *completions) compute(tm providers.Model, msgs []protocol.Message, opts providers.StreamOptions) (*providers.Prepared, error) {
	pr := prepareBase(tm, opts)
	pr.Thinking, pr.Effort = effectiveCompletionsThinking(tm, opts.Reasoning)
	extra, err := buildCompletionsBody(msgs, tm, pr)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}
	pr.MaxTokens = clampMaxTokens(tm, opts, utf8.RuneCount(raw)/4)
	return pr, nil
}

// Encode builds the request that Stream sends for pr and binding, with the
// real adapter code and no network.
func (p *completions) Encode(ctx context.Context, m providers.Model, pr *providers.Prepared, req providers.TranscriptRequest, binding providers.AuthBinding) (*providers.Encoded, error) {
	tm, err := prepareCompletionsModel(m)
	if err != nil {
		return nil, err
	}
	return providers.DryRun(ctx, func(client *http.Client) *providers.Stream {
		q := &completions{cfg: p.cfg}
		q.cfg.http = client
		return q.Stream(ctx, tm, req, providers.DryRunOptions(binding, tm.BaseURL, pr))
	})
}

// Prepare computes the effective values of a Responses request once. It reads
// no credential. The output limit is the requested one with the provider
// minimum applied; the compat record decides later whether it is sent.
func (p *responses) Prepare(m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) (*providers.Prepared, error) {
	tm, err := prepareResponsesModel(m)
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

func (p *responses) compute(tm providers.Model, msgs []protocol.Message, opts providers.StreamOptions) (*providers.Prepared, error) {
	pr := prepareBase(tm, opts)
	pr.Thinking = opts.Reasoning
	if word, ok := thinkingEffort(tm, opts.Reasoning); ok && opts.Reasoning != "" {
		pr.Effort = word
	}
	if opts.MaxTokens > 0 {
		pr.MaxTokens = max(opts.MaxTokens, 16)
	}
	if opts.CacheRetention != providers.CacheRetentionNone {
		pr.PromptCacheKey = clampPromptCacheKey(opts.SessionID)
	}
	if _, err := buildResponsesCall(msgs, tm, pr, ""); err != nil {
		return nil, err
	}
	return pr, nil
}

// Encode builds the request that Stream sends for pr and binding, with the
// real adapter code and no network.
func (p *responses) Encode(ctx context.Context, m providers.Model, pr *providers.Prepared, req providers.TranscriptRequest, binding providers.AuthBinding) (*providers.Encoded, error) {
	tm, err := prepareResponsesModel(m)
	if err != nil {
		return nil, err
	}
	return providers.DryRun(ctx, func(client *http.Client) *providers.Stream {
		q := &responses{cfg: p.cfg}
		q.cfg.http = client
		return q.Stream(ctx, tm, req, providers.DryRunOptions(binding, tm.BaseURL, pr))
	})
}
