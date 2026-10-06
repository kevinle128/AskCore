package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"charm.land/fantasy"
	fanthropic "charm.land/fantasy/providers/anthropic"

	"AskCore/internal/providers"
	"AskCore/internal/providers/fantasykit"
	"AskCore/pkg/protocol"
)

const (
	ProviderID = providers.ProviderTokenPlan
	APIID      = string(providers.APIAnthropicMessages)
	ModelID    = providers.ModelDeepSeekFlash
	// The Anthropic SDK appends v1/messages to this prefix.
	BaseURL = providers.TokenPlanMessagesURL

	askKeyEnv = "ASK_ALIBABA_TOKEN_PLAN_API_KEY"
	altKeyEnv = "ALIBABA_TOKEN_PLAN_API_KEY"
	userAgent = "AskCore"
	streamBuf = 64
	stopKey   = "stop_reason"
)

// Model returns the Token Plan messages catalog row. Stream uses the Model
// argument it is given, so a test can pass a different window or Input.
func Model() providers.Model {
	return providers.TokenPlanMessages()
}

// maxOutput holds the output limit of models whose limit differs from the
// catalog row. The route answers HTTP 400 "Range of max_tokens should be
// [1, 131072]" for qwen3.7-max above it.
var maxOutput = map[string]int{
	"qwen3.7-max": 131_072,
}

// ModelFor returns the catalog row with id as the model id. A known output
// limit of that model replaces the row default.
func ModelFor(id string) providers.Model {
	m := Model()
	if id == "" || id == m.ID {
		return m
	}
	m.ID, m.Name = id, id
	if limit, ok := maxOutput[id]; ok {
		m.MaxTokens = limit
	}
	return m
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
		normalize: providers.NormalizeAnthropicToolCallID,
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
		cfg.normalize = providers.NormalizeAnthropicToolCallID
	}
	return &adapter{cfg: cfg}
}

func (p *adapter) API() string { return APIID }

func (p *adapter) Stream(ctx context.Context, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	req = providers.TranscriptRequest{Messages: append([]protocol.Message(nil), req.Messages...)}
	tm, err := prepareModel(m)
	seed := protocol.AssistantMessage{
		API:       string(tm.API),
		Provider:  tm.Provider,
		Model:     tm.ID,
		Timestamp: p.cfg.now(),
	}
	if opts.Reasoning != "" {
		level := opts.Reasoning
		seed.ThinkingLevel = &level
	}
	return providers.NewStream(ctx, streamBuf, seed, func(a *providers.Assembler) {
		if err != nil {
			a.Fail(protocol.StopError, err.Error(), err)
			return
		}
		p.produce(ctx, a, tm, req, opts)
	})
}

func prepareModel(m providers.Model) (providers.Model, error) {
	row := providers.TokenPlanMessages()
	if m.API != "" && m.API != providers.APIAnthropicMessages {
		return m, fmt.Errorf("model API %q is not %s", m.API, providers.APIAnthropicMessages)
	}
	if m.API == "" {
		m.API = row.API
	}
	if m.Provider == "" {
		m.Provider = row.Provider
	}
	if m.BaseURL == "" {
		m.BaseURL = row.BaseURL
	}
	return m, nil
}

func (p *adapter) produce(ctx context.Context, a *providers.Assembler, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) {
	token, authErr := oauthToken(opts, m)
	if authErr != nil {
		a.Fail(protocol.StopError, authErr.Error(), authErr)
		return
	}
	key := strings.TrimSpace(opts.APIKey)
	if opts.Auth.Method == "api-key" {
		key = opts.Auth.AccessToken
	}
	if token == "" && key == "" {
		key = providers.LookupKey(m.Provider, p.cfg.env)
	}
	if token == "" && key == "" {
		hint := "the provider API key environment variable"
		switch m.Provider {
		case providers.ProviderTokenPlan:
			hint = "ASK_ALIBABA_TOKEN_PLAN_API_KEY, or ALIBABA_TOKEN_PLAN_API_KEY"
		case providers.ProviderAnthropic:
			hint = "ANTHROPIC_API_KEY"
		}
		msg := fmt.Sprintf("no API key for %s (set StreamOptions.APIKey or %s)", m.Provider, hint)
		a.Fail(protocol.StopError, msg, fmt.Errorf("%w: %s", providers.ErrAuthentication, msg))
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

	idle := fantasykit.StartIdle(p.cfg.idle, cancel)
	defer idle.Stop()

	witness := fantasykit.NewWitness(stopKey)
	httpClient := fantasykit.Client(p.cfg.http, witness, idle.Touch)
	var sdkOpts []fanthropic.Option
	if opts.Auth.Method == "api-key" {
		base := httpClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		httpClient.Transport = keyTransport{base: base, endpoint: m.BaseURL, key: key}
		httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		httpClient.Jar = nil
		sdkOpts = append(sdkOpts, fanthropic.WithHeaders(map[string]string{"Authorization": "", "X-Api-Key": key, "Cookie": ""}))
	}
	if token != "" {
		if len(m.Headers) > 0 {
			sdkOpts = append(sdkOpts, fanthropic.WithHeaders(m.Headers))
		}
		base := httpClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		httpClient.Transport = oauthTransport{base: base, token: token}
		httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		httpClient.Jar = nil
		sdkOpts = append(sdkOpts, fanthropic.WithHeaders(map[string]string{
			"Authorization":  "Bearer " + token,
			"X-Api-Key":      "",
			"X-App":          "cli",
			"Anthropic-Beta": oauthBeta,
			"Cookie":         "",
			"Anthropic-Dangerous-Direct-Browser-Access": "true",
		}))
	}

	sdkOpts = append(sdkOpts,
		fanthropic.WithAPIKey(key),
		fanthropic.WithBaseURL(m.BaseURL),
		fanthropic.WithHTTPClient(httpClient),
		fanthropic.WithUserAgent(func() string {
			if token != "" {
				return oauthUserAgent
			}
			return userAgent
		}()),
	)
	fp, err := fanthropic.New(sdkOpts...)
	if err != nil {
		fantasykit.Fail(a, ctx, runCtx, fantasykit.ErrIdleTimeout, err)
		return
	}
	lm, err := fp.LanguageModel(runCtx, m.ID)
	if err != nil {
		fantasykit.Fail(a, ctx, runCtx, fantasykit.ErrIdleTimeout, err)
		return
	}
	parts, err := lm.Stream(runCtx, fantasy.Call{
		Prompt: fantasy.Prompt{fantasy.NewUserMessage(".")},
		ProviderOptions: fanthropic.NewProviderOptions(&fanthropic.ProviderOptions{
			ExtraBody: extra,
		}),
	})
	if err != nil {
		fantasykit.Fail(a, ctx, runCtx, fantasykit.ErrIdleTimeout, err)
		return
	}
	if token != "" {
		codec, codecErr := newToolNames(msgs)
		if codecErr != nil {
			a.Fail(protocol.StopError, codecErr.Error(), codecErr)
			return
		}
		original := parts
		parts = func(yield func(fantasy.StreamPart) bool) {
			original(func(part fantasy.StreamPart) bool {
				part.ToolCallName = codec.decode(part.ToolCallName)
				return yield(part)
			})
		}
	}
	fantasykit.Fold(a, parts, fantasykit.Options{
		ReqCtx:    ctx,
		RunCtx:    runCtx,
		Idle:      fantasykit.ErrIdleTimeout,
		Witness:   witness,
		MapStop:   mapStop,
		Reasoning: reasoningMeta,
	})
	if !a.Settled() {
		if ctx.Err() != nil {
			a.Fail(protocol.StopAborted, "Request was aborted", ctx.Err())
			return
		}
		if cause := context.Cause(runCtx); errors.Is(cause, fantasykit.ErrIdleTimeout) {
			a.Fail(protocol.StopError, "idle timeout", fantasykit.ErrIdleTimeout)
		}
	}
}
