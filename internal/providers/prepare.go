package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"AskCore/pkg/protocol"
)

// AuthBinding is the safe projection of the credential that a request used:
// which provider, which login method and profile, and the billing hint. It has
// no token and no account ID. The Agent logs it with the attempt, and an
// adapter shapes a request from its Method (the Anthropic subscription profile
// adds a system prefix and maps tool names, for example).
type AuthBinding struct {
	Provider    string `json:"provider,omitempty"`
	Method      string `json:"method,omitempty"`
	Profile     string `json:"profile,omitempty"`
	BillingHint string `json:"billingHint,omitempty"`
}

// Binding is the safe projection of a.
func (a AuthSnapshot) Binding() AuthBinding {
	return AuthBinding{Provider: a.Provider, Method: a.Method, Profile: a.Profile, BillingHint: a.BillingHint}
}

// placeholderSecret stands in for every credential value of a dry run.
const placeholderSecret = "dry-run"

// DryRunOptions returns stream options that bind b to endpoint with a
// placeholder secret. A dry run (see Capture) uses them to send a request
// through the real adapter code when no credential is at hand.
func DryRunOptions(b AuthBinding, endpoint string, prepared *Prepared) StreamOptions {
	opts := StreamOptions{Prepared: prepared}
	if b.Method == "" {
		opts.APIKey = placeholderSecret
		return opts
	}
	opts.Auth = AuthSnapshot{
		Provider: b.Provider, Method: b.Method, Profile: b.Profile, BillingHint: b.BillingHint,
		Endpoint: endpoint, AccessToken: placeholderSecret,
	}
	return opts
}

// Prepared holds the effective values of one request: what the adapter sends
// after it applied its limits and defaults. An adapter computes them once, in
// Prepare, and Stream sends exactly those values, so the log of an attempt can
// rebuild the request that went on the wire.
//
// A Prepared holds no credential. These values are not in it, by name: the API
// key, an OAuth access token, an account ID, the values of the Authorization,
// X-Api-Key and Cookie headers, the values of account headers, and the value of
// every header that is not in the allowlist (only its name is kept). Three more
// inputs are not in it because they are static: the catalog row of the model
// (compat record, context window, reasoning flag, thinking level map, sampling
// parameters), the tool declarations and the messages, which the request log
// holds, and the shaping that depends on the credential binding, which the
// adapter derives from AuthBinding.Method.
type Prepared struct {
	// PolicyOnly is true when the registration has no request preparer.
	// Stream must compute its own adapter values for this registration.
	PolicyOnly  bool        `json:"-"`
	Provider    string      `json:"provider"`
	API         string      `json:"api"`
	Model       string      `json:"model"`
	RetryPolicy RetryPolicy `json:"retryPolicy"`
	// Endpoint is the scheme, host and path of the request URL, with no query
	// and no userinfo.
	Endpoint string `json:"endpoint,omitempty"`
	// MaxTokens is the output limit after the clamp. Zero means the request
	// carries none.
	MaxTokens int `json:"maxTokens,omitempty"`
	// Temperature is the requested sampling temperature. Nil means none.
	Temperature *float64 `json:"temperature,omitempty"`
	// Thinking is the thinking level that the request uses, with the default
	// applied. Effort is the word that goes on the wire for it.
	Thinking protocol.ThinkingLevel `json:"thinking,omitempty"`
	Effort   string                 `json:"effort,omitempty"`
	// ToolChoice is the canonical name of the forced tool, or empty.
	ToolChoice     string `json:"toolChoice,omitempty"`
	CacheRetention string `json:"cacheRetention,omitempty"`
	// PromptCacheKey is the cache key that a request sends, or empty.
	PromptCacheKey string `json:"promptCacheKey,omitempty"`
	// HeaderNames lists the name of every header of the request in lower case,
	// sorted, and Headers holds the value of each allowlisted header. Prepare
	// runs before the credential binding resolves, so both describe the request
	// that no credential profile has shaped: an API-key request. A header that
	// a login profile adds or replaces (the x-app, anthropic-beta and user-agent
	// headers of the Anthropic subscription profile, the bearer header) comes
	// from Registry.Encode with the AuthBinding of the attempt.
	HeaderNames []string          `json:"headerNames,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// Clone returns a deep copy of p. A nil p gives nil.
func (p *Prepared) Clone() *Prepared {
	if p == nil {
		return nil
	}
	out := *p
	if p.Temperature != nil {
		t := *p.Temperature
		out.Temperature = &t
	}
	out.HeaderNames = slices.Clone(p.HeaderNames)
	if p.Headers != nil {
		out.Headers = make(map[string]string, len(p.Headers))
		for k, v := range p.Headers {
			out.Headers[k] = v
		}
	}
	return &out
}

// allowedHeaderValues are the headers whose value is safe to keep.
var allowedHeaderValues = map[string]bool{
	"anthropic-version": true,
	"anthropic-beta":    true,
	"content-type":      true,
	"user-agent":        true,
	"x-app":             true,
}

// Encoded is a request as an adapter sends it: the URL, the headers and the
// body.
type Encoded struct {
	URL    *url.URL
	Header http.Header
	Body   []byte
}

// SetWireFacts fills Endpoint, HeaderNames and Headers of p from e. A header
// value is kept only when the header is in the allowlist.
func (p *Prepared) SetWireFacts(e *Encoded) {
	p.Endpoint = EndpointOf(e.URL.String())
	p.HeaderNames, p.Headers = nil, nil
	for name, values := range e.Header {
		lower := strings.ToLower(name)
		p.HeaderNames = append(p.HeaderNames, lower)
		if allowedHeaderValues[lower] && len(values) > 0 {
			if p.Headers == nil {
				p.Headers = map[string]string{}
			}
			p.Headers[lower] = values[len(values)-1]
		}
	}
	slices.Sort(p.HeaderNames)
}

// EndpointOf returns the scheme, host and path of raw: no userinfo, query or
// fragment. A value that is not an absolute URL gives an empty string.
func EndpointOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}

// Preparer is implemented by an adapter that computes the effective values of a
// request. Registry.RegisterProvider finds it.
type Preparer interface {
	// Prepare computes the effective values of the request. It reads no
	// credential: the binding resolves later, inside the stream function.
	Prepare(m Model, req TranscriptRequest, opts StreamOptions) (*Prepared, error)
	// Encode builds the request that Stream sends for prepared and binding. It
	// runs the real adapter code against a transport that records the request
	// and sends nothing, so it needs no credential.
	Encode(ctx context.Context, m Model, prepared *Prepared, req TranscriptRequest, binding AuthBinding) (*Encoded, error)
}

// ErrNoPreparer means that no adapter of the model API computes effective
// values.
var ErrNoPreparer = errors.New("no request preparer for the model API")

// Capture is an http.RoundTripper that records the request it gets and refuses
// it with an HTTP 400 answer, so that no network is used. An adapter built on a
// client with a Capture transport runs its whole request path and hands the
// request over, byte for byte, to the caller.
type Capture struct {
	mu  sync.Mutex
	got *Encoded
}

// NewCapture returns an empty Capture.
func NewCapture() *Capture { return &Capture{} }

// Client returns an http.Client that sends its requests to c.
func (c *Capture) Client() *http.Client { return &http.Client{Transport: c} }

// RoundTrip records the request and answers 400.
func (c *Capture) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	c.mu.Lock()
	if c.got == nil {
		u := *r.URL
		c.got = &Encoded{URL: &u, Header: r.Header.Clone(), Body: body}
	}
	c.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Status:     "400 Bad Request",
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":{"message":"dry run"}}`))),
		Request:    r,
	}, nil
}

// Request returns the first request that c recorded.
func (c *Capture) Request() (*Encoded, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.got == nil {
		return nil, errors.New("the adapter made no request")
	}
	return c.got, nil
}

// DryRun runs stream through a Capture and returns the request that it sent.
// build gets the client of the capture and returns the stream of an adapter that
// uses it.
func DryRun(ctx context.Context, build func(client *http.Client) *Stream) (*Encoded, error) {
	c := NewCapture()
	s := build(c.Client())
	for range s.Events() {
	}
	_, streamErr := s.Result(ctx)
	enc, err := c.Request()
	if err != nil {
		if streamErr != nil {
			return nil, fmt.Errorf("encode request: %w", streamErr)
		}
		return nil, err
	}
	return enc, nil
}

// RegisterProvider registers the stream of p for its API. When p is also a
// Preparer, Registry.Prepare and Registry.Encode use it for that API.
func (r *Registry) RegisterProvider(p Provider, policy ...RetryPolicy) {
	if r == nil {
		return
	}
	api := API(p.API())
	r.Register(api, p.Stream, policy...)
	if pr, ok := p.(Preparer); ok {
		r.mu.Lock()
		if r.preparers == nil {
			r.preparers = make(map[API]Preparer)
		}
		r.preparers[api] = pr
		r.mu.Unlock()
	}
}

func (r *Registry) preparerFor(api API) Preparer {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.preparers[api]
}

// Prepare computes the effective request and captures its registration policy.
// A provider without a Preparer still returns the model and policy facts.
func (r *Registry) Prepare(m Model, req TranscriptRequest, opts StreamOptions) (*Prepared, error) {
	policy := DefaultRetryPolicy()
	var pr Preparer
	if r != nil {
		r.mu.RLock()
		pr = r.preparers[m.API]
		if registered, ok := r.policies[m.API]; ok {
			policy = registered
		}
		r.mu.RUnlock()
	}
	p := &Prepared{
		PolicyOnly: true, Provider: m.Provider, API: string(m.API), Model: m.ID,
		Thinking: ClampThinkingLevel(m, opts.Reasoning), MaxTokens: opts.MaxTokens,
		Temperature: opts.Temperature, ToolChoice: opts.ToolChoice, CacheRetention: opts.CacheRetention,
	}
	p = p.Clone()
	if pr != nil {
		var err error
		p, err = pr.Prepare(m, req, opts)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, errors.New("provider preparer returned nil")
		}
		p = p.Clone()
	}
	p.RetryPolicy = policy
	return p, nil
}

// Encode builds the request that the adapter of m.API sends for prepared and
// binding. It sends nothing.
func (r *Registry) Encode(ctx context.Context, m Model, prepared *Prepared, req TranscriptRequest, binding AuthBinding) (*Encoded, error) {
	pr := r.preparerFor(m.API)
	if pr == nil {
		return nil, fmt.Errorf("%w %q", ErrNoPreparer, m.API)
	}
	return pr.Encode(ctx, m, prepared, req, binding)
}
