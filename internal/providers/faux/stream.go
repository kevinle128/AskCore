package faux

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
	"unicode/utf8"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

const (
	msgExhausted = "No more faux responses queued"
	msgPending   = "Faux response ended without a stop reason"
	msgAborted   = "Request was aborted"
	msgFailed    = "Faux response failed"
)

// planBlock is one normalized block of a reply: the final id, the argument
// text and the chunks of the text that the stream sends as deltas.
type planBlock struct {
	kind   byte // 't' text, 'k' thinking, 'c' tool call
	text   string
	id     string
	name   string
	args   string
	final  bool
	chunks []string
}

// plan is a step with ids assigned and chunk sizes drawn.
type plan struct {
	step   Step
	blocks []planBlock
}

// outputText is the text that the usage estimate counts for the reply.
func (pl *plan) outputText() string {
	lines := make([]string, 0, len(pl.blocks))
	for _, b := range pl.blocks {
		if b.kind == 'c' {
			lines = append(lines, b.name+":"+b.args)
		} else {
			lines = append(lines, b.text)
		}
	}
	return strings.Join(lines, "\n")
}

// Stream implements providers.Provider. It never fails at call time. The step,
// the call number, the request record and the cache estimate are all taken in
// the calling goroutine, so they follow call order.
func (p *Provider) Stream(ctx context.Context, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	req = cloneTranscript(req)
	opts = cloneOptions(opts)

	p.mu.Lock()
	p.calls++
	call := p.calls
	model, known := p.models[m.ID]
	if !known {
		model = m
	}
	model.Input = append([]string(nil), model.Input...)
	p.requests = append(p.requests, cloneRecord(Record{Call: call, Model: model, Options: opts, Transcript: req}))

	var step *Step
	if known && len(p.queue) > 0 {
		s := p.queue[0]
		p.queue = p.queue[1:]
		step = &s
	}
	prompt := p.estimatePrompt(opts.SessionID, opts.CacheRetention, serializeTranscript(req.Messages), known)
	var pl *plan
	var planErr error
	if step != nil && step.kind != kindFunc {
		pl, planErr = p.plan(*step, call)
	}
	p.mu.Unlock()

	seed := protocol.AssistantMessage{API: p.api, Provider: p.provider, Model: model.ID}
	if step != nil && step.ts != nil {
		seed.Timestamp = *step.ts
	} else {
		seed.Timestamp = p.clock.Now().UnixMilli()
	}
	var out int64
	if pl != nil {
		out = estimateTokens(pl.outputText())
	}
	seed.Usage = prompt.usage(out)

	r := &run{
		p: p, ctx: ctx, prompt: prompt, step: step, pl: pl, planErr: planErr, known: known,
		call: Call{Number: call, Model: model, Options: opts, Request: req},
	}
	return providers.NewStream(ctx, p.buffer, seed, r.produce)
}

// plan normalizes a Reply or Raw step for one call. Chunk sizes come from a
// generator seeded with the seed and the call number, and generated tool ids
// carry the call number. A step therefore gets the same chunks and ids whatever
// other steps ran before it and whenever its producer goroutine runs.
func (p *Provider) plan(s Step, call int) (*plan, error) {
	pl := &plan{step: s}
	rng := rand.New(rand.NewPCG(p.seed, uint64(call)))
	toolSeq := 0
	if s.kind == kindRaw {
		return pl, nil
	}
	for _, b := range s.blocks {
		switch v := b.(type) {
		case textBlock:
			pl.blocks = append(pl.blocks, planBlock{kind: 't', text: v.text, chunks: p.split(rng, v.text)})
		case thinkingBlock:
			pl.blocks = append(pl.blocks, planBlock{kind: 'k', text: v.text, chunks: p.split(rng, v.text)})
		case toolBlock:
			if v.err != nil {
				return nil, fmt.Errorf("faux: tool call %q: %w", v.name, v.err)
			}
			id := v.id
			if id == "" {
				toolSeq++
				id = fmt.Sprintf("tool:%d:%d", call, toolSeq)
			}
			pl.blocks = append(pl.blocks, planBlock{
				kind: 'c', id: id, name: v.name, args: v.raw, final: v.auth, chunks: p.split(rng, v.raw),
			})
		default:
			return nil, errors.New("faux: unknown block type")
		}
	}
	return pl, nil
}

// split cuts text into chunks of min..max tokens (4 runes each) at rune
// boundaries. Empty text gives one empty chunk.
func (p *Provider) split(rng *rand.Rand, text string) []string {
	if text == "" {
		return []string{""}
	}
	var chunks []string
	for len(text) > 0 {
		tokens := p.chunkMin
		if p.chunkMax > p.chunkMin {
			tokens += rng.IntN(p.chunkMax - p.chunkMin + 1)
		}
		n := 0
		for i := 0; i < tokens*runesPerToken && n < len(text); i++ {
			_, w := utf8.DecodeRuneInString(text[n:])
			n += w
		}
		chunks = append(chunks, text[:n])
		text = text[n:]
	}
	return chunks
}

// run holds the state of one call for the producer goroutine.
type run struct {
	p       *Provider
	ctx     context.Context
	prompt  promptEstimate
	step    *Step
	pl      *plan
	planErr error
	known   bool
	call    Call

	a     *providers.Assembler
	sent  int
	limit int // events before truncation; negative means no limit
}

// produce is the stream body.
func (r *run) produce(a *providers.Assembler) {
	r.a = a
	if !r.known {
		r.setupFail(r.prompt.usage(0), fmt.Errorf("unknown faux model: %s", r.call.Model.ID))
		return
	}
	if r.step == nil {
		// The text is the fixed message of the script contract, so it stays capitalized.
		r.setupFail(r.prompt.usage(0), errors.New(msgExhausted)) //nolint:staticcheck // fixed message text
		return
	}
	if r.planErr != nil {
		r.setupFail(r.prompt.usage(0), r.planErr)
		return
	}
	if r.pl == nil {
		pl, err := r.resolveFunc()
		if err != nil {
			r.setupFail(r.prompt.usage(0), err)
			return
		}
		r.pl = pl
	}
	step := r.pl.step
	if step.usage != nil {
		a.SetUsage(*step.usage)
	} else {
		a.SetUsage(r.prompt.usage(estimateTokens(r.pl.outputText())))
	}
	r.limit = -1
	if step.truncate != nil {
		r.limit = *step.truncate
	}
	if r.full() || !r.pause(step.delay) {
		return
	}
	if step.respID != nil {
		a.SetMetadata(providers.Metadata{ResponseID: step.respID})
	}
	if step.kind == kindRaw {
		r.produceRaw(step.raw)
		return
	}
	r.produceReply(step)
}

// resolveFunc runs the factory of a Func step and plans the step it returns.
func (r *run) resolveFunc() (pl *plan, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			pl, err = nil, fmt.Errorf("faux: factory panicked: %v", rec)
		}
	}()
	if r.step.fn == nil {
		return nil, errors.New("faux: Func step has no factory")
	}
	inner, err := r.step.fn(r.ctx, Call{
		Number: r.call.Number, Model: r.call.Model, Options: cloneOptions(r.call.Options),
		Request: cloneTranscript(r.call.Request),
	})
	if err != nil {
		return nil, err
	}
	merged, err := r.step.overlay(inner)
	if err != nil {
		return nil, err
	}
	return r.p.plan(merged, r.call.Number)
}

// setupFail ends the stream with one error event before any start event.
func (r *run) setupFail(u protocol.Usage, err error) {
	r.a.SetUsage(u)
	r.a.Fail(protocol.StopError, err.Error(), err)
}

func (r *run) full() bool { return r.limit >= 0 && r.sent >= r.limit }

// stopped reports that the producer must return without more events.
func (r *run) stopped() bool { return r.full() || r.a.Settled() }

// live reports that the producer can go on: not stopped and not cancelled.
func (r *run) live() bool { return !r.stopped() && r.ctx.Err() == nil }

// pause waits d on the clock. It returns false when the request was cancelled
// before or during the wait, or when the stream is stopped.
func (r *run) pause(d time.Duration) bool {
	if d > 0 {
		select {
		case <-r.p.clock.After(d):
		case <-r.ctx.Done():
			return false
		}
	}
	return r.live()
}

// count takes one event from the truncation budget.
func (r *run) count() bool {
	if r.full() {
		return false
	}
	r.sent++
	return true
}

func (r *run) chunkDelay(chunk string) time.Duration {
	tps := r.p.tps
	if r.pl.step.tps != nil {
		tps = *r.pl.step.tps
	}
	if tps <= 0 || chunk == "" {
		return 0
	}
	return time.Duration(float64(estimateTokens(chunk)) / tps * float64(time.Second))
}

func (r *run) produceReply(step Step) {
	a := r.a
	if !r.count() {
		return
	}
	a.Start()
	for _, b := range r.pl.blocks {
		if !r.live() {
			return
		}
		if !r.produceBlock(b) {
			return
		}
	}
	r.finish(step)
}

// produceBlock streams one block. It returns false when the producer must
// return at once.
func (r *run) produceBlock(b planBlock) bool {
	a := r.a
	if !r.count() {
		return false
	}
	var idx int
	switch b.kind {
	case 't':
		idx = a.TextStart("")
	case 'k':
		idx = a.ThinkingStart("", nil, nil)
	default:
		idx = a.ToolStart(b.id, b.name, nil, nil, nil)
	}
	if idx < 0 || r.stopped() {
		return false
	}
	for _, chunk := range b.chunks {
		if !r.pause(r.chunkDelay(chunk)) || !r.count() {
			return false
		}
		switch b.kind {
		case 't':
			a.TextDelta(idx, chunk)
		case 'k':
			a.ThinkingDelta(idx, chunk)
		default:
			a.ToolDelta(idx, chunk)
		}
		if r.stopped() {
			return false
		}
	}
	if !r.count() {
		return false
	}
	switch b.kind {
	case 't':
		a.TextEnd(idx, b.text, nil)
	case 'k':
		a.ThinkingEnd(idx, b.text, nil, nil)
	default:
		var final *protocol.ToolCall
		if b.final {
			final = &protocol.ToolCall{ID: b.id, Name: b.name, Arguments: []byte(b.args)}
		}
		a.ToolEnd(idx, final)
	}
	return !r.stopped()
}

// finish sends the terminal call that follows the last block.
func (r *run) finish(step Step) {
	a := r.a
	reason := protocol.StopStop
	for _, b := range r.pl.blocks {
		if b.kind == 'c' {
			reason = protocol.StopToolUse
		}
	}
	if step.stop != nil {
		reason = *step.stop
	}
	text := func(def string) string {
		if step.errMsg != nil {
			return *step.errMsg
		}
		return def
	}
	switch reason {
	case protocol.StopPending:
		a.Fail(protocol.StopError, msgPending, nil)
	case protocol.StopError:
		if f := step.failure; f != nil {
			a.Fail(protocol.StopError, text(f.Error()), f)
			return
		}
		msg := text(msgFailed)
		a.Fail(protocol.StopError, msg, errors.New(msg))
	case protocol.StopAborted:
		msg := text(msgAborted)
		a.Fail(protocol.StopAborted, msg, errors.New(msg))
	default:
		a.Done(reason)
	}
}

func (r *run) produceRaw(events []protocol.AssistantMessageEvent) {
	for _, ev := range events {
		if !r.live() || !r.count() {
			return
		}
		err := r.a.Emit(ev)
		if errors.Is(err, providers.ErrStreamClosed) {
			return
		}
		if err != nil {
			r.a.Fail(protocol.StopError, err.Error(), err)
			return
		}
	}
}
