package sessions

import (
	"bytes"

	"AskCore/pkg/protocol"
)

// Entry is one record of the session log. The set of entry types is closed:
// only this package implements the interface. An entry holds no credential:
// every field is a plain value that the writer of the entry chose, and the
// model and option types below list the safe fields one by one.
type Entry interface {
	entry()
}

// MessageEntry is a finished message of the conversation. The model history
// is the list of message entries and nothing else.
type MessageEntry struct {
	Message protocol.Message `json:"message"`
	// InputID is the ID of the queued or prompted input that this message
	// carries into the log. It is empty for a message that no input made.
	InputID string `json:"inputId,omitempty"`
}

// CycleOpened records the start of an input cycle.
type CycleOpened struct {
	CycleID string `json:"cycleId"`
}

// CycleClosed records the end of an input cycle. Cause is one of the fixed
// words of cycle_end.cause, and is empty unless the cycle was aborted. Code is
// the failure code of cycle_end.code, and is empty unless the cycle ended in
// an error.
type CycleClosed struct {
	CycleID string `json:"cycleId"`
	Reason  string `json:"reason"`
	Cause   string `json:"cause,omitempty"`
	Code    string `json:"code,omitempty"`
}

// TurnOpened records the start of a turn.
type TurnOpened struct {
	CycleID string `json:"cycleId"`
}

// TurnClosed records the end of a turn.
type TurnClosed struct {
	CycleID string `json:"cycleId"`
}

// Failure is the cleaned failure of an attempt. Code is the failure code
// (RATE_LIMIT, AUTH, SERVER and so on; UNKNOWN for an error that holds no
// provider fact). Text has no URL query, no userinfo and no key, and it is at
// most 512 bytes.
type Failure struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// AttemptSettled records how a model request that started ended. It is never
// written for a request that did not start.
type AttemptSettled struct {
	AttemptID string `json:"attemptId"`
	Outcome   string `json:"outcome"`
	// Usage is nil when the usage of the attempt is unknown.
	Usage   *protocol.Usage `json:"usage,omitempty"`
	Failure *Failure        `json:"failure,omitempty"`
	// Binding is the credential binding that the request used. It is nil when
	// no credential was bound (a faux provider, or a binding that failed).
	Binding *AuthBinding `json:"binding,omitempty"`
}

// ToolCall records call intent before lookup, validation, or control handlers.
// AssistantEntry is the log position of the assistant MessageEntry that owns
// CallID. A record does not mean the body started and adds no model message.
type ToolCall struct {
	AssistantEntry int    `json:"assistantEntry"`
	CallID         string `json:"callId"`
}

// RetryScheduled records a retry before the backoff wait.
type RetryScheduled struct {
	RetryID    string  `json:"retryId"`
	CycleID    string  `json:"cycleId"`
	Turn       int     `json:"turn"`
	Provider   string  `json:"provider"`
	PolicyKey  string  `json:"policyKey"`
	Retry      int     `json:"retry"`
	MaxRetries int     `json:"maxRetries"`
	DelayMs    int64   `json:"delayMs"`
	Failure    Failure `json:"failure"`
}

// RetryStarted records the completion of the retry wait.
type RetryStarted struct {
	RetryID string `json:"retryId"`
	Retry   int    `json:"retry"`
}

// AuthBinding is the safe projection of the credential of a request: no token
// and no account ID. A request is rebuilt from it, because the login method
// shapes the request.
type AuthBinding struct {
	Provider    string `json:"provider,omitempty"`
	Method      string `json:"method,omitempty"`
	Profile     string `json:"profile,omitempty"`
	BillingHint string `json:"billingHint,omitempty"`
}

// RetryPolicy holds the safe policy values captured for one request.
// Durations are in milliseconds.
type RetryPolicy struct {
	Key         string `json:"key"`
	MaxRetries  int    `json:"maxRetries"`
	BaseDelayMs int64  `json:"baseDelayMs"`
	MaxDelayMs  int64  `json:"maxDelayMs"`
}

// PreparedRequest holds the effective values that the provider adapter used
// for a request, after its limits and defaults. It holds no credential. The
// names of the values that it leaves out are in the documentation of
// providers.Prepared. HeaderNames lists every header name; Headers has the
// value of the allowlisted headers only.
type PreparedRequest struct {
	PolicyOnly     bool                   `json:"policyOnly,omitempty"`
	RetryPolicy    RetryPolicy            `json:"retryPolicy"`
	Provider       string                 `json:"provider"`
	API            string                 `json:"api"`
	Model          string                 `json:"model"`
	Endpoint       string                 `json:"endpoint,omitempty"`
	MaxTokens      int                    `json:"maxTokens,omitempty"`
	Temperature    *float64               `json:"temperature,omitempty"`
	Thinking       protocol.ThinkingLevel `json:"thinking,omitempty"`
	Effort         string                 `json:"effort,omitempty"`
	ToolChoice     string                 `json:"toolChoice,omitempty"`
	CacheRetention string                 `json:"cacheRetention,omitempty"`
	PromptCacheKey string                 `json:"promptCacheKey,omitempty"`
	HeaderNames    []string               `json:"headerNames,omitempty"`
	Headers        map[string]string      `json:"headers,omitempty"`
}

// ModelRef names the model of a request without any credential. BaseURLHost
// holds the scheme and the host of the base URL: no userinfo, path or query.
// HeaderNames lists the names of the model headers, never their values.
type ModelRef struct {
	Provider    string   `json:"provider"`
	API         string   `json:"api"`
	ID          string   `json:"id"`
	BaseURLHost string   `json:"baseUrlHost,omitempty"`
	HeaderNames []string `json:"headerNames,omitempty"`
}

// RequestOptions are the options that the Agent sets for a request. The list
// is closed: a key, a token or a bound credential has no field here.
type RequestOptions struct {
	MaxTokens      int                    `json:"maxTokens,omitempty"`
	Temperature    *float64               `json:"temperature,omitempty"`
	Reasoning      protocol.ThinkingLevel `json:"reasoning,omitempty"`
	ToolChoice     string                 `json:"toolChoice,omitempty"`
	CacheRetention string                 `json:"cacheRetention,omitempty"`
	SessionID      string                 `json:"sessionId,omitempty"`
}

// RequestDelta records what an attempt sent, as a change of the baseline that
// the log gives: the last SystemSnapshot, then the committed messages. The
// messages of the request from index Start on replace Removed (baseline
// messages) with Added. A message that a handler edited is in both lists.
// It is written before the stream call, so the logical request of every
// attempt can be rebuilt.
type RequestDelta struct {
	AttemptID string             `json:"attemptId"`
	Start     int                `json:"start"`
	Removed   []protocol.Message `json:"removed,omitempty"`
	Added     []protocol.Message `json:"added,omitempty"`
	Model     ModelRef           `json:"model"`
	Options   RequestOptions     `json:"options"`
	// Prepared holds the effective values that the adapter sent. It is nil for
	// a provider that computes none.
	Prepared *PreparedRequest `json:"prepared,omitempty"`
}

// SystemSnapshot records the system prompt and the tool declarations that
// head the requests which follow it, until the next snapshot.
type SystemSnapshot struct {
	SystemPrompt string              `json:"systemPrompt"`
	Tools        []protocol.ToolDecl `json:"tools"`
}

// InputOutcome records what became of one claimed input: accepted when its
// message entered a turn and was committed, rejected when admission refused it.
// An input that was removed or cleared from a queue was never claimed; the
// queue_update events show it.
type InputOutcome struct {
	InputID  string `json:"inputId"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

func (MessageEntry) entry()   {}
func (CycleOpened) entry()    {}
func (CycleClosed) entry()    {}
func (TurnOpened) entry()     {}
func (TurnClosed) entry()     {}
func (AttemptSettled) entry() {}
func (ToolCall) entry()       {}
func (RetryScheduled) entry() {}
func (RetryStarted) entry()   {}
func (RequestDelta) entry()   {}
func (SystemSnapshot) entry() {}
func (InputOutcome) entry()   {}

// Clone returns a deep copy of e, so that a change of the copy never reaches
// the original.
func Clone(e Entry) Entry {
	switch v := e.(type) {
	case MessageEntry:
		v.Message = protocol.CloneMessage(v.Message)
		return v
	case AttemptSettled:
		if v.Usage != nil {
			u := v.Usage.Clone()
			v.Usage = &u
		}
		if v.Failure != nil {
			f := *v.Failure
			v.Failure = &f
		}
		if v.Binding != nil {
			b := *v.Binding
			v.Binding = &b
		}
		return v
	case RequestDelta:
		v.Removed = cloneMessages(v.Removed)
		v.Added = cloneMessages(v.Added)
		v.Model.HeaderNames = append([]string(nil), v.Model.HeaderNames...)
		if v.Options.Temperature != nil {
			t := *v.Options.Temperature
			v.Options.Temperature = &t
		}
		v.Prepared = v.Prepared.Clone()
		return v
	case SystemSnapshot:
		v.Tools = CloneTools(v.Tools)
		return v
	}
	return e
}

// Clone returns a deep copy of p. A nil p gives nil.
func (p *PreparedRequest) Clone() *PreparedRequest {
	if p == nil {
		return nil
	}
	out := *p
	if p.Temperature != nil {
		t := *p.Temperature
		out.Temperature = &t
	}
	out.HeaderNames = append([]string(nil), p.HeaderNames...)
	if p.Headers != nil {
		out.Headers = make(map[string]string, len(p.Headers))
		for k, v := range p.Headers {
			out.Headers[k] = v
		}
	}
	return &out
}

// CloneTools copies decls, including the raw parameter schemas.
func CloneTools(decls []protocol.ToolDecl) []protocol.ToolDecl {
	if decls == nil {
		return nil
	}
	out := make([]protocol.ToolDecl, len(decls))
	for i, d := range decls {
		out[i] = d
		out[i].Parameters = bytes.Clone(d.Parameters)
	}
	return out
}

func cloneMessages(msgs []protocol.Message) []protocol.Message {
	if msgs == nil {
		return nil
	}
	out := make([]protocol.Message, len(msgs))
	for i, m := range msgs {
		out[i] = protocol.CloneMessage(m)
	}
	return out
}

// MessagesOf returns the messages of the message entries in entries, in log
// order, as deep copies. It is the model history that the entries give.
func MessagesOf(entries []Entry) []protocol.Message {
	out := make([]protocol.Message, 0, len(entries))
	for _, e := range entries {
		if m, ok := e.(MessageEntry); ok {
			out = append(out, protocol.CloneMessage(m.Message))
		}
	}
	return out
}
