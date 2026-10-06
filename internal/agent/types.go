package agent

import (
	"errors"
	"time"

	"AskCore/internal/bus"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// ErrBusy is returned by Prompt, Continue, Reset, SetModel and
// SetThinkingLevel while a run is active. Steer and FollowUp never return it:
// they queue.
var ErrBusy = errors.New("agent: a run is active")

// ErrOutputFailure is the cancel cause for a run that its owner stops because
// the output of the run failed. The cycle ends aborted with cause "output".
var ErrOutputFailure = errors.New("agent: output failed")

// ErrNoAPIKey is returned by SetModel when ResolveKey finds no secret
// for the new model's provider. The stored model and thinking level stay.
var ErrNoAPIKey = errors.New("agent: no API key for provider")

// Status is the run state of an Agent.
type Status uint8

const (
	Idle Status = iota
	Running
)

// Config is the fixed configuration of an Agent. Stream is required
// unless Registry is set; New then uses Registry.Stream.
type Config struct {
	// LoopConfig goes to every run. The Agent registers its own PrepareRequest
	// handler in front of the handlers of LoopConfig.Pipeline.
	LoopConfig
	// Application holds control handlers that every Agent with the same
	// registry shares. Its handlers run before the handlers of
	// LoopConfig.Pipeline, which belong to this Agent alone. A handler added
	// to or removed from it takes effect at the next dispatch. Nil means none.
	Application *pipeline.Registry
	// Registry supplies Stream as Registry.Stream when Stream is nil.
	Registry     *providers.Registry
	SystemPrompt string
	// Tools is nil when the agent has no tools. Its declarations are read
	// at the start of each run.
	Tools *tools.Registry
	// SessionID goes into every event envelope, and into
	// Options.SessionID when that is empty.
	SessionID string
	// NewContext returns the log of a fresh conversation. New calls it
	// once and Reset again. Nil means an in-memory sessions.MemoryLog. The
	// Agent driver is the only caller of its Append.
	NewContext func() sessions.Writer
	// ListenerError receives the error or the panic of a listener. The failure
	// is contained: the listener stays subscribed, the later listeners still get
	// the event, and the run goes on. Nil drops the failure. It runs on the
	// goroutine that published the event, so it must return quickly.
	ListenerError func(error)
	// FollowLimits bound the replay ring and the followers of Follow. Zero
	// fields take the defaults of bus.Limits.
	FollowLimits bus.Limits
	// MaxQueuedInputs bounds the steering and follow-up messages together. A
	// send beyond it fails with ErrQueueFull. Zero means
	// DefaultMaxQueuedInputs.
	MaxQueuedInputs int
	// Clock stamps the event envelope and the run-failure message. Nil
	// means time.Now.
	Clock func() time.Time
}

// State is a snapshot of an Agent.
type State struct {
	Status        Status
	Messages      []protocol.Message
	Model         providers.Model
	ThinkingLevel protocol.ThinkingLevel
}
