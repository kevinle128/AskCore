package protocol

// Project trust methods. The leader and the in-process TUI host answer them;
// an editor link (ask acp) answers unsupported. They name no session: the
// trust of a folder applies to the sessions that start after a change.
const (
	ACPTrustState = "_ask/trust/state"
	ACPTrustSet   = "_ask/trust/set"

	// ACPTrustMetaKey is the key in the _meta of session/new that carries a
	// trust override for that session only. The value is an ACPTrustOverride.
	// The host saves nothing for it.
	ACPTrustMetaKey = "ask.dev/trust"
)

// ACPTrustOverride is the trust choice of one session that comes before every
// saved decision.
type ACPTrustOverride string

const (
	ACPTrustApprove ACPTrustOverride = "approve"
	ACPTrustDeny    ACPTrustOverride = "deny"
)

// ACPTrustStateRequest names the working directory of a future session.
type ACPTrustStateRequest struct {
	Cwd string `json:"cwd"`
}

// ACPTrustStateResult is the trust decision for a working directory, as a
// session that starts now would get it from a client that can ask the user.
//
// Source is one of the words "flag", "no_gated_files", "store_error", "saved",
// "saved_ancestor", "default_always", "default_never", "prompt" and "no_ui".
// NeedsDecision is true only for "prompt". Gated lists the gated entries that
// exist: relative to Root in the root, absolute in an ancestor. EntryPath is
// the folder of the saved entry that applies. Warning is a fixed text with a
// path, for example for a trust store that cannot be read; it never holds
// file content.
type ACPTrustStateResult struct {
	Root          string   `json:"root"`
	Trusted       bool     `json:"trusted"`
	Source        string   `json:"source"`
	NeedsDecision bool     `json:"needsDecision,omitempty"`
	Gated         []string `json:"gated,omitempty"`
	EntryPath     string   `json:"entryPath,omitempty"`
	Warning       string   `json:"warning,omitempty"`
}

// ACPTrustTarget names the folder that a trust change applies to.
type ACPTrustTarget string

const (
	// ACPTrustTargetFolder is the canonical project root of the cwd.
	ACPTrustTargetFolder ACPTrustTarget = "folder"
	// ACPTrustTargetParent is the parent folder of that root.
	ACPTrustTargetParent ACPTrustTarget = "parent"
)

// ACPTrustDecision is the change to the saved trust of a folder.
type ACPTrustDecision string

const (
	ACPTrustDecisionTrust    ACPTrustDecision = "trust"
	ACPTrustDecisionDistrust ACPTrustDecision = "distrust"
	// ACPTrustDecisionClear removes the saved entry of the folder.
	ACPTrustDecisionClear ACPTrustDecision = "clear"
)

// ACPTrustSetRequest saves a trust decision. A decision on the parent also
// removes the entry of the folder, so that the parent entry applies to it.
// The result is the new ACPTrustStateResult of Cwd.
type ACPTrustSetRequest struct {
	Cwd      string           `json:"cwd"`
	Target   ACPTrustTarget   `json:"target"`
	Decision ACPTrustDecision `json:"decision"`
}

// ACPSessionTrust is the trust decision that a session started with. Source
// uses the words of ACPTrustStateResult.
type ACPSessionTrust struct {
	Trusted bool   `json:"trusted"`
	Source  string `json:"source"`
}

// ACPDiagnostic is one settings problem of a session: the layer ("user" or
// "project"), the file, the key (empty for the whole file) and a fixed text.
// It never holds file content.
type ACPDiagnostic struct {
	Layer string `json:"layer"`
	Path  string `json:"path"`
	Key   string `json:"key,omitempty"`
	Text  string `json:"text"`
}

// ACPDefaultNotSaved is the reason why a set_model with saveDefault did not
// save the default.
type ACPDefaultNotSaved string

const (
	// ACPDefaultShadowed means that the trusted project settings set another
	// default model, so a user default would not apply in this project.
	ACPDefaultShadowed ACPDefaultNotSaved = "shadowed"
	// ACPDefaultUnsafeFile means that the user settings file is not valid or
	// not safe; it keeps its bytes.
	ACPDefaultUnsafeFile ACPDefaultNotSaved = "unsafe_file"
	// ACPDefaultFailed is any other failure.
	ACPDefaultFailed ACPDefaultNotSaved = "failed"
)
