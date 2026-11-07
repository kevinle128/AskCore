package protocol

// ACPSessionOptionsMetaKey is the key in the _meta of session/new that carries
// the per-session flags of ACPSessionOptions. The host applies them to that
// session only and saves nothing. It is additive: a host that does not know the
// key ignores it, so the leader protocol version does not change.
const ACPSessionOptionsMetaKey = "ask.dev/session"

// ACPSessionPromptMaxBytes caps SystemPrompt and all AppendSystemPrompt entries
// together, in bytes.
const ACPSessionPromptMaxBytes = 256 << 10

// Bounds of the model options of a session.
const (
	// ACPSessionModelMaxBytes caps Model and each entry of Models, in bytes.
	ACPSessionModelMaxBytes = 256
	// ACPSessionModelsMax caps the number of entries of Models.
	ACPSessionModelsMax = 64
)

// ACPAppliedOptionsMetaKey is the key in the _meta of the session/new result
// that echoes the model options that the host applied. A host that does not
// know the model options sends no echo, so a client that sent one of them can
// tell that an older host dropped it.
const ACPAppliedOptionsMetaKey = "ask.dev/appliedOptions"

// ACPSessionOptions are the flags of one session. The client has already read
// any file: SystemPrompt and AppendSystemPrompt hold text, not paths. The zero
// value means no flags.
//
// Tools names the built-in tools to keep. NoTools keeps none. Both empty keeps
// the tools that the settings choose.
//
// Model is a model pattern ([provider/]id[@api][:level]) for the start model.
// Thinking is the start thinking level; it wins over the level of Model.
// Models are the scope patterns and globs; they choose the start model when
// Model is empty and give the cycle list. They replace the enabledModels
// setting for the session.
type ACPSessionOptions struct {
	SystemPrompt       string        `json:"systemPrompt,omitempty"`
	AppendSystemPrompt []string      `json:"appendSystemPrompt,omitempty"`
	Tools              []string      `json:"tools,omitempty"`
	NoTools            bool          `json:"noTools,omitempty"`
	Model              string        `json:"model,omitempty"`
	Thinking           ThinkingLevel `json:"thinking,omitempty"`
	Models             []string      `json:"models,omitempty"`
}

// ACPAppliedOptions is the echo of the model options that the host applied to
// a new session. Each field holds the value of the request.
type ACPAppliedOptions struct {
	Model    string        `json:"model,omitempty"`
	Thinking ThinkingLevel `json:"thinking,omitempty"`
	Models   []string      `json:"models,omitempty"`
}

func (o ACPSessionOptions) ModelOptions() ACPAppliedOptions {
	return ACPAppliedOptions{Model: o.Model, Thinking: o.Thinking, Models: o.Models}
}

// IsZero reports whether a carries no option.
func (a ACPAppliedOptions) IsZero() bool {
	return a.Model == "" && a.Thinking == "" && len(a.Models) == 0
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
	return o.SystemPrompt == "" && len(o.AppendSystemPrompt) == 0 && len(o.Tools) == 0 && !o.NoTools &&
		o.ModelOptions().IsZero()
}
