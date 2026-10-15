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

	"AskCore/internal/providers"
	"AskCore/internal/providers/fantasykit"
	"AskCore/pkg/protocol"
)

type responses struct {
	cfg config
}

// NewResponses is the openai-responses stream. Registry.Register binds it
// by API, not by model id.
func NewResponses(opts ...Option) providers.Provider {
	return &responses{cfg: newConfig(opts, providers.NormalizeResponsesToolCallID)}
}

func (p *responses) API() string { return string(providers.APIOpenAIResponses) }

func (p *responses) Stream(ctx context.Context, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	req = providers.TranscriptRequest{Messages: append([]protocol.Message(nil), req.Messages...)}
	tm, err := prepareResponsesModel(m)
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

func prepareResponsesModel(m providers.Model) (providers.Model, error) {
	row := providers.OpenAIGPT55()
	if m.API != "" && m.API != providers.APIOpenAIResponses {
		return m, fmt.Errorf("model API %q is not %s", m.API, providers.APIOpenAIResponses)
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

func (p *responses) produce(ctx context.Context, a *providers.Assembler, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) {
	key, subscription, authErr := responsesCredential(m, opts)
	if authErr != nil {
		a.Fail(protocol.StopError, authErr.Error(), authErr)
		return
	}
	if opts.Auth.Method == "" {
		key = strings.TrimSpace(opts.APIKey)
	}
	if key == "" && !subscription {
		key = providers.LookupKey(m.Provider, p.cfg.env)
	}
	if key == "" {
		hint := "the provider API key environment variable"
		switch m.Provider {
		case providers.ProviderOpenAI:
			hint = "OPENAI_API_KEY"
		case providers.ProviderXAI:
			hint = "XAI_API_KEY"
		}
		msg := fmt.Sprintf("no API key for %s (set StreamOptions.APIKey or %s)", m.Provider, hint)
		a.Fail(protocol.StopError, msg, fmt.Errorf("%w: %s", providers.ErrAuthentication, msg))
		return
	}
	if strings.TrimSpace(m.BaseURL) == "" {
		a.Fail(protocol.StopError, "openai model has no BaseURL", errors.New("openai model has no BaseURL"))
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
	if opts.Auth.Method == "openai-chatgpt" && (pr.Temperature != nil || pr.MaxTokens != 0 || pr.CacheRetention == providers.CacheRetentionLong) {
		err := fmt.Errorf("%w: ChatGPT Responses sampling, output limit, or long cache retention is unsupported", providers.ErrUnsupportedRequest)
		a.Fail(protocol.StopError, err.Error(), err)
		return
	}
	call, err := buildResponsesCall(msgs, m, pr, opts.Auth.Method)
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

	baseClient := isolateClient(p.cfg.http, m.Headers)
	if subscription || opts.Auth.Method == "api-key" {
		baseClient = profileClient(p.cfg.http, m, key, opts.Auth.Method == "openai-chatgpt")
	}
	httpClient := fantasykit.Client(baseClient, nil, idle.Touch)
	if namespaces := responseNamespaces(msgs, m); len(namespaces) > 0 {
		httpClient.Transport = responseNamespaceRT{base: httpClient.Transport, namespaces: namespaces}
	}

	fp, err := fopenai.New(
		fopenai.WithAPIKey(key),
		fopenai.WithBaseURL(m.BaseURL),
		fopenai.WithHTTPClient(httpClient),
		fopenai.WithUserAgent(userAgent),
		fopenai.WithUseResponsesAPI(),
		fopenai.WithResponsesAPIFunc(func(string) bool { return true }),
	)
	if err != nil {
		fantasykit.Fail(a, ctx, runCtx, fantasykit.ErrIdleTimeout, classifyResponsesError(runCtx, err))
		return
	}
	lm, err := fp.LanguageModel(runCtx, m.ID)
	if err != nil {
		fantasykit.Fail(a, ctx, runCtx, fantasykit.ErrIdleTimeout, classifyResponsesError(runCtx, err))
		return
	}
	parts, err := lm.Stream(runCtx, call)
	if err != nil {
		fantasykit.Fail(a, ctx, runCtx, fantasykit.ErrIdleTimeout, classifyResponsesError(runCtx, err))
		return
	}
	original := parts
	parts = func(yield func(fantasy.StreamPart) bool) {
		original(func(part fantasy.StreamPart) bool {
			part.Error = classifyResponsesError(runCtx, part.Error)
			return yield(part)
		})
	}
	fantasykit.Fold(a, parts, fantasykit.Options{
		ReqCtx:    ctx,
		RunCtx:    runCtx,
		Idle:      fantasykit.ErrIdleTimeout,
		MapStop:   mapResponsesStop,
		Reasoning: responsesReasoning,
		Text:      responsesText,
		Tool:      responsesToolID,
		Usage:     foldUsage(m, ""),
		Meta:      responsesFinishMeta,
		RawStop:   responsesRawStop,
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

// Fantasy's Responses tool metadata carries item IDs but has no namespace slot.
// Add namespaces only to calls replayed for the exact model that produced them.
func responseNamespaces(msgs []protocol.Message, target providers.Model) map[string]string {
	out := map[string]string{}
	for _, msg := range msgs {
		asst, ok := assistantValue(msg)
		if !ok || asst.API != string(target.API) || asst.Provider != target.Provider || asst.Model != target.ID {
			continue
		}
		for _, block := range asst.Content {
			var call protocol.ToolCall
			switch v := block.(type) {
			case protocol.ToolCall:
				call = v
			case *protocol.ToolCall:
				if v == nil {
					continue
				}
				call = *v
			default:
				continue
			}
			if call.Namespace == nil || *call.Namespace == "" {
				continue
			}
			callID, itemID := providers.SplitToolCallID(call.ID)
			if itemID != "" && !providers.DropResponsesItemID(itemID, "function_call", target, target) {
				out[callID+"|"+itemID] = *call.Namespace
			}
		}
	}
	return out
}

type responseNamespaceRT struct {
	base       http.RoundTripper
	namespaces map[string]string
}

func (t responseNamespaceRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.GetBody == nil {
		return nil, fmt.Errorf("responses request body is unavailable")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, err
	}
	input, ok := payload["input"].([]any)
	if !ok {
		return nil, fmt.Errorf("responses input is missing")
	}
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || item["type"] != "function_call" {
			continue
		}
		callID, _ := item["call_id"].(string)
		itemID, _ := item["id"].(string)
		if namespace, ok := t.namespaces[callID+"|"+itemID]; ok {
			item["namespace"] = namespace
		}
	}
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
