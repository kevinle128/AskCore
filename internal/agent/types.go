package agent

import (
	"errors"
	"time"

	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// ErrBusy is returned by Prompt, Continue and Reset while a run is active.
var ErrBusy = errors.New("agent: a run is active")

// Status is the run state of an Agent.
type Status uint8

const (
	Idle Status = iota
	Running
)

// Config is the fixed configuration of an Agent. Stream is required.
type Config struct {
	// LoopConfig goes to every run. The Agent installs its own
	// PrepareRequest in front of Hooks.PrepareRequest.
	LoopConfig
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
	Status   Status
	Messages []protocol.Message
}
