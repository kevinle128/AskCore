package tokenplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"charm.land/fantasy"
	fanthropic "charm.land/fantasy/providers/anthropic"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

const (
	ProviderID = "alibaba-token-plan"
	APIID      = "anthropic-messages"
	ModelID    = "deepseek-v4.1-flash"
	// The Anthropic SDK appends v1/messages to this prefix.
	BaseURL = "https://token-plan.ap-southeast-1.maas.aliyuncs.com/apps/anthropic"

	askKeyEnv = "ASK_ALIBABA_TOKEN_PLAN_API_KEY"
	altKeyEnv = "ALIBABA_TOKEN_PLAN_API_KEY"
	userAgent = "AskCore"
	streamBuf = 64
)

var errIdleTimeout = errors.New("idle timeout")

// Model returns the one catalog row. ContextWindow 1_000_000 and MaxTokens
// 384_000 are provisional third-party figures, not Alibaba docs. Stream uses
// the Model argument it is given, so a test can pass a different window or Input.
func Model() providers.Model {
	return providers.Model{
		ID:            ModelID,
		Name:          ModelID,
		API:           APIID,
		Provider:      ProviderID,
		Reasoning:     true,
		Input:         []string{"text", "image"},
		ContextWindow: 1_000_000,
		MaxTokens:     384_000,
	}
}

type Option func(*config)

type config struct {
	env       func(string) (string, bool)
	http      *http.Client
	now       func() int64
	normalize providers.NormalizeToolCallID
	idle      time.Duration
}

func WithEnv(lookup func(string) (string, bool)) Option {
	return func(c *config) { c.env = lookup }
}

func WithHTTPClient(c *http.Client) Option {
	return func(cfg *config) { cfg.http = c }
}

func WithNow(now func() int64) Option {
	return func(c *config) { c.now = now }
}

func WithNormalizeID(fn providers.NormalizeToolCallID) Option {
	return func(c *config) { c.normalize = fn }
}

func WithIdle(d time.Duration) Option {
	return func(c *config) { c.idle = d }
}

type adapter struct {
	cfg config
}

func New(opts ...Option) providers.Provider {
	cfg := config{
		env:       os.LookupEnv,
		now:       func() int64 { return time.Now().UnixMilli() },
		normalize: normalizeAnthropicToolCallID,
		idle:      5 * time.Minute,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.env == nil {
		cfg.env = os.LookupEnv
	}
	if cfg.now == nil {
		cfg.now = func() int64 { return time.Now().UnixMilli() }
	}
	if cfg.normalize == nil {
		cfg.normalize = normalizeAnthropicToolCallID
	}
	return &adapter{cfg: cfg}
}

func (p *adapter) API() string { return APIID }

func (p *adapter) Stream(ctx context.Context, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	req = providers.TranscriptRequest{Messages: append([]protocol.Message(nil), req.Messages...)}
	seed := protocol.AssistantMessage{
		API:       APIID,
		Provider:  ProviderID,
		Model:     m.ID,
		Timestamp: p.cfg.now(),
	}
	if opts.Reasoning != "" {
		level := opts.Reasoning
		seed.ThinkingLevel = &level
	}
	tm := providers.Model{
		ID:            m.ID,
		Name:          m.Name,
		API:           APIID,
		Provider:      ProviderID,
		Reasoning:     m.Reasoning,
		Input:         m.Input,
		ContextWindow: m.ContextWindow,
		MaxTokens:     m.MaxTokens,
	}
	return providers.NewStream(ctx, streamBuf, seed, func(a *providers.Assembler) {
		p.produce(ctx, a, tm, req, opts)
	})
}

func (p *adapter) produce(ctx context.Context, a *providers.Assembler, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) {
	key := strings.TrimSpace(opts.APIKey)
	if key == "" {
		if v, ok := p.cfg.env(askKeyEnv); ok {
			key = strings.TrimSpace(v)
		}
	}
	if key == "" {
		if v, ok := p.cfg.env(altKeyEnv); ok {
			key = strings.TrimSpace(v)
		}
	}
	if key == "" {
		a.Fail(protocol.StopError, "no API key for alibaba-token-plan (set StreamOptions.APIKey, ASK_ALIBABA_TOKEN_PLAN_API_KEY, or ALIBABA_TOKEN_PLAN_API_KEY)", errors.New("no API key for alibaba-token-plan"))
		return
	}
	switch opts.CacheRetention {
	case "", providers.CacheRetentionNone, providers.CacheRetentionShort:
	case providers.CacheRetentionLong:
		a.Fail(protocol.StopError, `cache retention "long" is not supported`, errors.New(`cache retention "long" is not supported`))
		return
	default:
		msg := fmt.Sprintf("cache retention %q is not supported", opts.CacheRetention)
		a.Fail(protocol.StopError, msg, errors.New(msg))
		return
	}

	msgs := providers.TransformMessages(req.Messages, m, p.cfg.now, p.cfg.normalize)
	doc, diags, err := buildDocument(msgs, m, opts, nil)
	if err != nil {
		a.Fail(protocol.StopError, err.Error(), err)
		return
	}

	a.Start()
	if len(diags) > 0 {
		a.SetMetadata(providers.Metadata{Diagnostics: diags})
	}

	var extra map[string]any
	if err := json.Unmarshal(doc.body, &extra); err != nil {
		a.Fail(protocol.StopError, err.Error(), err)
		return
	}

	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var idleMu sync.Mutex
	var timer *time.Timer
	if p.cfg.idle > 0 {
		timer = time.AfterFunc(p.cfg.idle, func() {
			cancel(errIdleTimeout)
		})
		defer timer.Stop()
	}
	touch := func() {
		if timer == nil {
			return
		}
		idleMu.Lock()
		timer.Reset(p.cfg.idle)
		idleMu.Unlock()
	}

	witness := &stopWitness{}
	httpClient := wrapClient(p.cfg.http, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		base := http.DefaultTransport
		if p.cfg.http != nil && p.cfg.http.Transport != nil {
			base = p.cfg.http.Transport
		} else if p.cfg.http != nil {
			base = http.DefaultTransport
		}
		witness.base = base
		resp, err := witness.RoundTrip(req)
		if err != nil || resp == nil || resp.Body == nil {
			return resp, err
		}
		resp.Body = &touchBody{ReadCloser: resp.Body, touch: touch}
		return resp, nil
	}))

	fp, err := fanthropic.New(
		fanthropic.WithAPIKey(key),
		fanthropic.WithBaseURL(BaseURL),
		fanthropic.WithHTTPClient(httpClient),
		fanthropic.WithUserAgent(userAgent),
	)
	if err != nil {
		failStream(a, ctx, runCtx, errIdleTimeout, err)
		return
	}
	lm, err := fp.LanguageModel(runCtx, m.ID)
	if err != nil {
		failStream(a, ctx, runCtx, errIdleTimeout, err)
		return
	}
	parts, err := lm.Stream(runCtx, fantasy.Call{
		Prompt: fantasy.Prompt{fantasy.NewUserMessage(".")},
		ProviderOptions: fanthropic.NewProviderOptions(&fanthropic.ProviderOptions{
			ExtraBody: extra,
		}),
	})
	if err != nil {
		failStream(a, ctx, runCtx, errIdleTimeout, err)
		return
	}
	fold(a, parts, ctx, runCtx, errIdleTimeout, witness)
	if !a.Settled() {
		if ctx.Err() != nil {
			a.Fail(protocol.StopAborted, "Request was aborted", ctx.Err())
			return
		}
		if cause := context.Cause(runCtx); errors.Is(cause, errIdleTimeout) {
			a.Fail(protocol.StopError, "idle timeout", errIdleTimeout)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func wrapClient(base *http.Client, rt http.RoundTripper) *http.Client {
	out := &http.Client{Timeout: 0}
	if base != nil {
		c := *base
		out = &c
	}
	out.Transport = rt
	return out
}

type touchBody struct {
	io.ReadCloser
	touch func()
}

func (b *touchBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 && b.touch != nil {
		b.touch()
	}
	return n, err
}

func normalizeAnthropicToolCallID(id string, _ providers.Model, _ protocol.AssistantMessage) string {
	runes := []rune(id)
	for i, r := range runes {
		if r <= 0x7f && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			continue
		}
		runes[i] = '_'
	}
	if len(runes) > 64 {
		runes = runes[:64]
	}
	return string(runes)
}
