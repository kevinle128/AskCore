package agent

import (
	"errors"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// ErrBusy is returned by Prompt, Continue, Reset, SetModel and
// SetThinkingLevel while a run is active.
var ErrBusy = errors.New("agent: a run is active")

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
	// LoopConfig goes to every run. The Agent composes its own PrepareRequest
	// in front of Hooks.PrepareRequest.
	LoopConfig
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
	// once and Reset again. Nil means an in-memory sessions.MemoryLog.
	NewContext func() ContextSource
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
