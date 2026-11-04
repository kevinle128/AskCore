package protocol

// ACPSessionOptionsMetaKey is the key in the _meta of session/new that carries
// the per-session flags of ACPSessionOptions. The host applies them to that
// session only and saves nothing. It is additive: a host that does not know the
// key ignores it, so the leader protocol version does not change.
const ACPSessionOptionsMetaKey = "ask.dev/session"

// ACPSessionPromptMaxBytes caps SystemPrompt and all AppendSystemPrompt entries
// together, in bytes.
const ACPSessionPromptMaxBytes = 256 << 10

// ACPSessionOptions are the flags of one session. The client has already read
// any file: SystemPrompt and AppendSystemPrompt hold text, not paths. The zero
// value means no flags.
//
// Tools names the built-in tools to keep. NoTools keeps none. Both empty keeps
// the tools that the settings choose.
type ACPSessionOptions struct {
	SystemPrompt       string   `json:"systemPrompt,omitempty"`
	AppendSystemPrompt []string `json:"appendSystemPrompt,omitempty"`
	Tools              []string `json:"tools,omitempty"`
	NoTools            bool     `json:"noTools,omitempty"`
}

// PromptBytes is the size that ACPSessionPromptMaxBytes limits: the system
// prompt and all appended prompts together.
func (o ACPSessionOptions) PromptBytes() int {
	n := len(o.SystemPrompt)
	for _, a := range o.AppendSystemPrompt {
		n += len(a)
	}
	return n
}

// IsZero reports whether o carries no flag.
func (o ACPSessionOptions) IsZero() bool {
	return o.SystemPrompt == "" && len(o.AppendSystemPrompt) == 0 && len(o.Tools) == 0 && !o.NoTools
}
