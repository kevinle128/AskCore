package agent

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// loggedRequest is the logical request of one attempt, as the log rebuilds
// it: the messages that the model got, the system prompt and the tools that
// head them, the model and the options that the Agent set. Prepared holds the
// effective values that the adapter computed (the clamped token limit, for
// example), and Binding the credential binding that the attempt used. With
// them the provider registry encodes the body that went on the wire.
// Prepared is nil for a provider that computes none; Binding is the zero value
// when no credential was bound or the attempt did not settle.
type loggedRequest struct {
	Messages     []protocol.Message
	SystemPrompt string
	Tools        []protocol.ToolDecl
	Model        sessions.ModelRef
	Options      sessions.RequestOptions
	Prepared     *providers.Prepared
	Binding      providers.AuthBinding
}

// snapshotOf is the system snapshot of a prompt and a tool registry. The tools
// are in name order, so the registration order never reaches a request.
func snapshotOf(prompt string, reg *tools.Registry) sessions.SystemSnapshot {
	var decls []protocol.ToolDecl
	if reg != nil {
		decls = sortedDecls(reg.Decls())
	}
	return sessions.SystemSnapshot{SystemPrompt: prompt, Tools: decls}
}

// sameSnapshot reports whether two snapshots head the same requests.
func sameSnapshot(a, b sessions.SystemSnapshot) bool {
	return a.SystemPrompt == b.SystemPrompt && slices.EqualFunc(a.Tools, b.Tools, providers.SameToolDecl)
}

// modelRef is the model of a request without any credential. Only the scheme
// and the host of the base URL are kept; header values are never copied.
func modelRef(m providers.Model) sessions.ModelRef {
	ref := sessions.ModelRef{Provider: m.Provider, API: string(m.API), ID: m.ID, BaseURLHost: baseURLHost(m.BaseURL)}
	for name := range m.Headers {
		ref.HeaderNames = append(ref.HeaderNames, name)
	}
	slices.Sort(ref.HeaderNames)
	return ref
}

// baseURLHost returns scheme and host of raw: no userinfo, path or query. A
// value that is not an absolute URL gives an empty string.
func baseURLHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

// requestOptions copies the options that the Agent sets, field by field.
// APIKey and Auth have no field in the result.
func requestOptions(o providers.StreamOptions) sessions.RequestOptions {
	out := sessions.RequestOptions{
		MaxTokens:      o.MaxTokens,
		Reasoning:      o.Reasoning,
		ToolChoice:     o.ToolChoice,
		CacheRetention: o.CacheRetention,
		SessionID:      o.SessionID,
	}
	if o.Temperature != nil {
		t := *o.Temperature
		out.Temperature = &t
	}
	return out
}

// freezeMessages returns a deep copy of msgs. The request that goes to the
// provider is this copy, so a later edit of the history, or of the request by
// a handler, never changes what the log shows for it.
func freezeMessages(msgs []protocol.Message) []protocol.Message {
	out := make([]protocol.Message, len(msgs))
	for i, m := range msgs {
		out[i] = protocol.CloneMessage(m)
	}
	return out
}

// sameMessage reports whether two messages encode to the same JSON.
func sameMessage(a, b protocol.Message) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	ja, errA := protocol.MarshalMessage(a)
	jb, errB := protocol.MarshalMessage(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

// projectHistory is the model view of a header and the committed messages
// under the default conversion. It is the baseline that a RequestDelta changes
// and that rebuildRequest starts from.
func projectHistory(header, committed []protocol.Message) []protocol.Message {
	all := append(slices.Clone(header), committed...)
	return providers.ConvertToLLM(all)
}

// newRequestDelta describes request as a change of baseline: the common prefix
// and the common suffix stay, the part between them is replaced.
func newRequestDelta(attemptID string, baseline, request []protocol.Message, model providers.Model, opts providers.StreamOptions, prepared *providers.Prepared) sessions.RequestDelta {
	start := 0
	for start < len(baseline) && start < len(request) && sameMessage(baseline[start], request[start]) {
		start++
	}
	tail := 0
	for tail < len(baseline)-start && tail < len(request)-start &&
		sameMessage(baseline[len(baseline)-1-tail], request[len(request)-1-tail]) {
		tail++
	}
	return sessions.RequestDelta{
		AttemptID: attemptID,
		Start:     start,
		Removed:   freezeMessages(baseline[start : len(baseline)-tail]),
		Added:     freezeMessages(request[start : len(request)-tail]),
		Model:     modelRef(model),
		Options:   requestOptions(opts),
		Prepared:  preparedEntry(prepared),
	}
}

// preparedEntry is the log form of the effective values of a request. Both
// types hold the same safe fields; the sessions package cannot import the
// providers package, so the Agent maps between them.
func preparedEntry(p *providers.Prepared) *sessions.PreparedRequest {
	if p == nil {
		return nil
	}
	c := p.Clone()
	return &sessions.PreparedRequest{
		Provider: c.Provider, API: c.API, Model: c.Model, Endpoint: c.Endpoint, PolicyOnly: c.PolicyOnly,
		MaxTokens: c.MaxTokens, Temperature: c.Temperature, Thinking: c.Thinking, Effort: c.Effort,
		ToolChoice: c.ToolChoice, CacheRetention: c.CacheRetention, PromptCacheKey: c.PromptCacheKey,
		HeaderNames: c.HeaderNames, Headers: c.Headers,
		RetryPolicy: sessions.RetryPolicy{Key: c.RetryPolicy.Key, MaxRetries: c.RetryPolicy.MaxRetries, BaseDelayMs: c.RetryPolicy.BaseDelay.Milliseconds(), MaxDelayMs: c.RetryPolicy.MaxDelay.Milliseconds()},
	}
}

// preparedOf is the inverse of preparedEntry.
func preparedOf(e *sessions.PreparedRequest) *providers.Prepared {
	if e == nil {
		return nil
	}
	c := e.Clone()
	return &providers.Prepared{
		Provider: c.Provider, API: c.API, Model: c.Model, Endpoint: c.Endpoint, PolicyOnly: c.PolicyOnly,
		MaxTokens: c.MaxTokens, Temperature: c.Temperature, Thinking: c.Thinking, Effort: c.Effort,
		ToolChoice: c.ToolChoice, CacheRetention: c.CacheRetention, PromptCacheKey: c.PromptCacheKey,
		HeaderNames: c.HeaderNames, Headers: c.Headers,
		RetryPolicy: providers.RetryPolicy{Key: c.RetryPolicy.Key, MaxRetries: c.RetryPolicy.MaxRetries, BaseDelay: time.Duration(c.RetryPolicy.BaseDelayMs) * time.Millisecond, MaxDelay: time.Duration(c.RetryPolicy.MaxDelayMs) * time.Millisecond},
	}
}

// bindingOf is the credential binding that the attempt settled with, or the
// zero value when the attempt has no settlement or no binding.
func bindingOf(entries []sessions.Entry, attemptID string) providers.AuthBinding {
	for _, e := range entries {
		if s, ok := e.(sessions.AttemptSettled); ok && s.AttemptID == attemptID && s.Binding != nil {
			b := s.Binding
			return providers.AuthBinding{Provider: b.Provider, Method: b.Method, Profile: b.Profile, BillingHint: b.BillingHint}
		}
	}
	return providers.AuthBinding{}
}

// rebuildRequest returns the request of an attempt from the entries alone: the
// last system snapshot before its RequestDelta, the messages that were
// committed before it, the delta, the effective values that the adapter used
// and the credential binding of the attempt.
func rebuildRequest(entries []sessions.Entry, attemptID string) (loggedRequest, error) {
	at := slices.IndexFunc(entries, func(e sessions.Entry) bool {
		d, ok := e.(sessions.RequestDelta)
		return ok && d.AttemptID == attemptID
	})
	if at < 0 {
		return loggedRequest{}, fmt.Errorf("agent: no request is logged for attempt %q", attemptID)
	}
	delta := entries[at].(sessions.RequestDelta)
	snap, ok := lastSnapshotOf(entries[:at])
	if !ok {
		return loggedRequest{}, fmt.Errorf("agent: no system snapshot before the request of attempt %q", attemptID)
	}
	baseline := projectHistory(systemMessages(snap), sessions.MessagesOf(entries[:at]))
	end := delta.Start + len(delta.Removed)
	if delta.Start > len(baseline) || end > len(baseline) {
		return loggedRequest{}, fmt.Errorf("agent: the request delta of attempt %q does not fit its baseline", attemptID)
	}
	msgs := append(slices.Clone(baseline[:delta.Start]), freezeMessages(delta.Added)...)
	msgs = append(msgs, baseline[end:]...)
	return loggedRequest{
		Messages:     freezeMessages(msgs),
		SystemPrompt: snap.SystemPrompt,
		Tools:        providers.CurrentTools(msgs),
		Model:        delta.Model,
		Options:      delta.Options,
		Prepared:     preparedOf(delta.Prepared),
		Binding:      bindingOf(entries[at:], attemptID),
	}, nil
}

// failureText is the cleaned form of an error text that the Agent stores.
func failureText(text string) string {
	return providers.CleanDiagnostic(errors.New(text))
}

// lastSnapshotOf returns the newest system snapshot of entries.
func lastSnapshotOf(entries []sessions.Entry) (sessions.SystemSnapshot, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if s, ok := entries[i].(sessions.SystemSnapshot); ok {
			return s, true
		}
	}
	return sessions.SystemSnapshot{}, false
}
