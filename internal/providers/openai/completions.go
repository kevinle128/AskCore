package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"charm.land/fantasy"
	fopenai "charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	openaisdk "github.com/charmbracelet/openai-go"

	"AskCore/internal/providers"
	"AskCore/internal/providers/fantasykit"
	"AskCore/pkg/protocol"
)

const completionsStopKey = "finish_reason"

type completions struct {
	cfg config
}

// NewCompletions is the openai-completions stream. Registry.Register binds it
// by API, not by model id.
func NewCompletions(opts ...Option) providers.Provider {
	return &completions{cfg: newConfig(opts, providers.NormalizeCompletionsToolCallID)}
}

func (p *completions) API() string { return string(providers.APIOpenAICompletions) }

func (p *completions) Stream(ctx context.Context, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	req = providers.TranscriptRequest{Messages: append([]protocol.Message(nil), req.Messages...)}
	tm, err := prepareCompletionsModel(m)
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

func prepareCompletionsModel(m providers.Model) (providers.Model, error) {
	row := providers.TokenPlanCompletions()
	if m.API != "" && m.API != providers.APIOpenAICompletions {
		return m, fmt.Errorf("model API %q is not %s", m.API, providers.APIOpenAICompletions)
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
	if m.Compat == nil {
		m.Compat = row.Compat
	}
	return m, nil
}

func (p *completions) produce(ctx context.Context, a *providers.Assembler, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) {
	key := strings.TrimSpace(opts.APIKey)
	if opts.Auth != (providers.AuthSnapshot{}) {
		auth := opts.Auth
		if auth.Method != "api-key" || !auth.ValidFor(m.Provider, m.BaseURL) || (key != "" && key != auth.AccessToken) {
			err := fmt.Errorf("%w: Completions credential is not bound to this API", providers.ErrAuthentication)
			a.Fail(protocol.StopError, err.Error(), err)
			return
		}
		key = auth.AccessToken
	}
	if key == "" {
		key = providers.LookupKey(providers.ProviderTokenPlan, p.cfg.env)
	}
	if key == "" {
		a.Fail(protocol.StopError, "no API key for alibaba-token-plan (set StreamOptions.APIKey, ASK_ALIBABA_TOKEN_PLAN_API_KEY, or ALIBABA_TOKEN_PLAN_API_KEY)", errors.New("no API key for alibaba-token-plan"))
		return
	}

	msgs := providers.TransformMessages(req.Messages, m, p.cfg.now, p.cfg.normalize)
	pr := opts.Prepared
	if pr == nil {
		var err error
		if pr, err = p.compute(m, msgs, opts); err != nil {
			a.Fail(protocol.StopError, err.Error(), err)
			return
		}
	}
	extra, err := buildCompletionsBody(msgs, m, pr)
	if err != nil {
		a.Fail(protocol.StopError, err.Error(), err)
		return
	}

	a.Start()

	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	runCtx = fantasykit.TrackBody(runCtx)

	idle := fantasykit.StartIdle(p.cfg.idle, cancel)
	defer idle.Stop()

	witness := fantasykit.NewWitness(completionsStopKey)
	baseClient := isolateClient(p.cfg.http, m.Headers)
	if opts.Auth.Method == "api-key" {
		baseClient = profileClient(p.cfg.http, m, key, false)
	}
	httpClient := fantasykit.Client(baseClient, witness, idle.Touch)
	if compat, ok := m.Completions(); ok && !compat.SupportsStreamOptions {
		httpClient.Transport = omitStreamOptionsRT{base: httpClient.Transport}
	}

	var responseID, responseModel string
	lmOpts := []fopenai.LanguageModelOption{
		fopenai.WithLanguageModelStreamUsageFunc(func(chunk openaisdk.ChatCompletionChunk, usageCtx map[string]any, metadata fantasy.ProviderMetadata) (fantasy.Usage, fantasy.ProviderMetadata) {
			if chunk.ID != "" {
				responseID = chunk.ID
			}
			if chunk.Model != "" {
				responseModel = chunk.Model
			}
			return keepStreamUsage(chunk, usageCtx, metadata)
		}),
		fopenai.WithLanguageModelStreamExtraFunc(completionsStreamReasoning),
	}
	if compat, ok := m.Completions(); ok && compat.SupportsFinishReason {
		lmOpts = append(lmOpts, fopenai.WithLanguageModelRequireFinishReason())
	}

	fp, err := openaicompat.New(
		openaicompat.WithAPIKey(key),
		openaicompat.WithBaseURL(m.BaseURL),
		openaicompat.WithHTTPClient(httpClient),
		openaicompat.WithUserAgent(userAgent),
		openaicompat.WithLanguageModelOptions(lmOpts...),
	)
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
		ProviderOptions: openaicompat.NewProviderOptions(&openaicompat.ProviderOptions{
			ExtraBody: extra,
		}),
	})
	if err != nil {
		fantasykit.Fail(a, ctx, runCtx, fantasykit.ErrIdleTimeout, err)
		return
	}
	compat, _ := m.Completions()
	fantasykit.Fold(a, parts, fantasykit.Options{
		ReqCtx:          ctx,
		RunCtx:          runCtx,
		Idle:            fantasykit.ErrIdleTimeout,
		Witness:         witness,
		InferStopOnDone: !compat.SupportsFinishReason,
		MapStop:         mapCompletionsStop,
		Reasoning:       completionsReasoning,
		Usage:           foldUsage(m, ""),
		Meta: func(fantasy.StreamPart) providers.Metadata {
			meta := providers.Metadata{}
			if responseID != "" {
				meta.ResponseID = &responseID
			}
			if responseModel != "" {
				meta.ResponseModel = &responseModel
			}
			return meta
		},
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

// Fantasy sets stream_options on every Chat Completions stream, including
// compatible endpoints that do not accept this OpenAI-only field.
type omitStreamOptionsRT struct{ base http.RoundTripper }

func (t omitStreamOptionsRT) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, err
	}
	delete(payload, "stream_options")
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Body = io.NopCloser(bytes.NewReader(raw))
	clone.ContentLength = int64(len(raw))
	clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	return t.base.RoundTrip(clone)
}

func isolateClient(base *http.Client, headers map[string]string) *http.Client {
	out := &http.Client{Timeout: 0}
	var rt http.RoundTripper
	if base != nil {
		c := *base
		out = &c
		rt = base.Transport
	}
	out.Transport = Isolate(rt, headers)
	return out
}
