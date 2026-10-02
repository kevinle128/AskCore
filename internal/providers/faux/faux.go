// Package faux is a scripted model provider. It streams the replies that a
// caller queued, through the same assembler as the real adapters, so loop,
// retry and session tests need no network. The output is deterministic: chunk
// sizes come from a generator seeded per call and tool ids carry the call number.
package faux

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

const (
	defaultName          = "faux"
	defaultModelID       = "faux-1"
	defaultContextWindow = 128000
	defaultMaxTokens     = 16384
	defaultBuffer        = 64
	minBuffer            = 32
	runesPerToken        = 4
)

// Clock is the time source of the provider. A test passes a fake clock so that
// pacing needs no real sleep.
type Clock interface {
	Now() time.Time
	// After returns a channel that gets one value after d.
	After(d time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// ModelDef defines one model of the provider. Zero fields get the defaults:
// name = id, input = text and image, context 128000, max tokens 16384.
type ModelDef struct {
	ID            string
	Name          string
	Reasoning     bool
	Input         []string
	ContextWindow int
	MaxTokens     int
}

// Record is one call that the provider saw. It holds copies, so later changes
// by the caller or by the stream cannot change it.
type Record struct {
	Call       int
	Model      providers.Model
	Options    providers.StreamOptions
	Transcript providers.TranscriptRequest
}

// Option changes the provider that New builds.
type Option func(*config)

type config struct {
	api, provider      string
	models             []ModelDef
	chunkMin, chunkMax int
	seed               int64
	tps                float64
	clock              Clock
	buffer             int
}

// WithModels sets the models. The default is one model, faux-1.
func WithModels(defs ...ModelDef) Option { return func(c *config) { c.models = defs } }

// WithAPI sets the api id (default "faux").
func WithAPI(id string) Option { return func(c *config) { c.api = id } }

// WithProvider sets the provider id (default "faux").
func WithProvider(id string) Option { return func(c *config) { c.provider = id } }

// WithChunk sets the chunk size range in tokens of 4 runes (default 3 to 5).
func WithChunk(minTokens, maxTokens int) Option {
	return func(c *config) { c.chunkMin, c.chunkMax = minTokens, maxTokens }
}

// WithSeed sets the seed of the chunk size generator (default 1).
func WithSeed(seed int64) Option { return func(c *config) { c.seed = seed } }

// WithTokensPerSecond paces every chunk. Zero (the default) means no wait.
func WithTokensPerSecond(tps float64) Option { return func(c *config) { c.tps = tps } }

// WithClock sets the clock for timestamps and waits.
func WithClock(clock Clock) Option { return func(c *config) { c.clock = clock } }

// WithBuffer sets the capacity of the event channel of each stream. Values
// below 32 are raised to 32, so a consumer that reads after the producer ended
// does not block it for short replies.
func WithBuffer(n int) Option { return func(c *config) { c.buffer = n } }

// Provider is the scripted provider. It implements providers.Provider and is
// safe for concurrent use.
type Provider struct {
	api, provider      string
	models             map[string]providers.Model
	order              []string
	chunkMin, chunkMax int
	seed               uint64
	tps                float64
	clock              Clock
	buffer             int

	mu       sync.Mutex
	queue    []Step
	calls    int
	cache    map[string]string
	requests []Record
}

var _ providers.Provider = (*Provider)(nil)

// New builds a provider. It returns an error for duplicate or empty model ids,
// an invalid chunk range, a negative pace or buffer, or an empty api or
// provider id.
func New(opts ...Option) (*Provider, error) {
	c := config{
		api: defaultName, provider: defaultName,
		chunkMin: 3, chunkMax: 5, seed: 1,
		clock: systemClock{}, buffer: defaultBuffer,
	}
	for _, o := range opts {
		o(&c)
	}
	switch {
	case c.api == "" || c.provider == "":
		return nil, errors.New("faux: api and provider ids must not be empty")
	case c.chunkMin < 1 || c.chunkMax < c.chunkMin:
		return nil, fmt.Errorf("faux: invalid chunk range %d..%d", c.chunkMin, c.chunkMax)
	case c.tps < 0:
		return nil, errors.New("faux: tokens per second must not be negative")
	case c.buffer < 0:
		return nil, errors.New("faux: buffer must not be negative")
	case c.clock == nil:
		return nil, errors.New("faux: clock must not be nil")
	}
	if len(c.models) == 0 {
		c.models = []ModelDef{{ID: defaultModelID}}
	}
	p := &Provider{
		api: c.api, provider: c.provider,
		models:   make(map[string]providers.Model, len(c.models)),
		chunkMin: c.chunkMin, chunkMax: c.chunkMax, seed: uint64(c.seed),
		tps: c.tps, clock: c.clock, buffer: max(c.buffer, minBuffer),
		cache: map[string]string{},
	}
	for _, d := range c.models {
		if d.ID == "" {
			return nil, errors.New("faux: model id must not be empty")
		}
		if _, dup := p.models[d.ID]; dup {
			return nil, fmt.Errorf("faux: duplicate model id %q", d.ID)
		}
		p.models[d.ID] = p.buildModel(d)
		p.order = append(p.order, d.ID)
	}
	return p, nil
}

func (p *Provider) buildModel(d ModelDef) providers.Model {
	m := providers.Model{
		ID: d.ID, Name: d.Name, API: p.api, Provider: p.provider,
		Reasoning: d.Reasoning, Input: slices.Clone(d.Input),
		ContextWindow: d.ContextWindow, MaxTokens: d.MaxTokens,
	}
	if m.Name == "" {
		m.Name = d.ID
	}
	if m.Input == nil {
		m.Input = []string{"text", "image"}
	}
	if m.ContextWindow == 0 {
		m.ContextWindow = defaultContextWindow
	}
	if m.MaxTokens == 0 {
		m.MaxTokens = defaultMaxTokens
	}
	return m
}

// API returns the api id.
func (p *Provider) API() string { return p.api }

// Model returns the model with the given id. The second value is false for an
// unknown id; the first value is then the zero model.
func (p *Provider) Model(id string) (providers.Model, bool) {
	m, ok := p.models[id]
	if !ok {
		return providers.Model{}, false
	}
	m.Input = slices.Clone(m.Input)
	return m, true
}

// Set replaces the pending steps. It does not change the call count, a reply
// in progress or the session cache.
func (p *Provider) Set(steps ...Step) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = slices.Clone(steps)
}

// Append adds steps after the pending ones.
func (p *Provider) Append(steps ...Step) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = append(p.queue, steps...)
}

// Pending returns the number of steps that no call has taken.
func (p *Provider) Pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queue)
}

// Calls returns the number of Stream calls so far.
func (p *Provider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// Requests returns a copy of every call that the provider saw, in call order.
func (p *Provider) Requests() []Record {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Record, len(p.requests))
	for i, r := range p.requests {
		out[i] = cloneRecord(r)
	}
	return out
}

func cloneRecord(r Record) Record {
	r.Model.Input = slices.Clone(r.Model.Input)
	r.Options = cloneOptions(r.Options)
	r.Transcript = cloneTranscript(r.Transcript)
	return r
}

func cloneOptions(o providers.StreamOptions) providers.StreamOptions {
	if o.Temperature != nil {
		t := *o.Temperature
		o.Temperature = &t
	}
	return o
}

func cloneTranscript(t providers.TranscriptRequest) providers.TranscriptRequest {
	msgs := make([]protocol.Message, len(t.Messages))
	for i, m := range t.Messages {
		msgs[i] = protocol.CloneMessage(m)
	}
	return providers.TranscriptRequest{Messages: msgs}
}
