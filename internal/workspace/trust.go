package workspace

import "path/filepath"

// Override is a trust choice for one session that comes before every other
// rule: a command-line flag or a session-only choice. It is never saved.
type Override string

const (
	// OverrideNone means that no override applies.
	OverrideNone Override = ""
	// OverrideApprove trusts the project for this session.
	OverrideApprove Override = "approve"
	// OverrideDeny does not trust the project for this session.
	OverrideDeny Override = "deny"
)

// DefaultTrust is the defaultProjectTrust value of the user settings. An empty
// or unknown value acts as DefaultTrustAsk.
type DefaultTrust string

const (
	// DefaultTrustAsk asks the user when a UI is available.
	DefaultTrustAsk DefaultTrust = "ask"
	// DefaultTrustAlways trusts a project that has no saved decision.
	DefaultTrustAlways DefaultTrust = "always"
	// DefaultTrustNever does not trust a project that has no saved decision.
	DefaultTrustNever DefaultTrust = "never"
)

// TrustSource names the rule that made a trust decision. The values are
// stable words of the wire protocol; do not change them.
type TrustSource string

const (
	SourceFlag          TrustSource = "flag"
	SourceNoGatedFiles  TrustSource = "no_gated_files"
	SourceStoreError    TrustSource = "store_error"
	SourceSaved         TrustSource = "saved"
	SourceSavedAncestor TrustSource = "saved_ancestor"
	SourceDefaultAlways TrustSource = "default_always"
	SourceDefaultNever  TrustSource = "default_never"
	SourcePrompt        TrustSource = "prompt"
	SourceNoUI          TrustSource = "no_ui"
)

// Entry is the saved trust entry that applies to a project root.
type Entry struct {
	// Found is false when no saved entry applies.
	Found bool
	// Trusted is the saved value.
	Trusted bool
	// Path is the key of the entry in the trust store.
	Path string
	// Ancestor is true when the entry is on a parent folder of the root.
	Ancestor bool
}

// NearestEntry returns the saved entry that applies to a canonical project
// root. It looks at the root, then at each parent folder up to the file
// system root. The first folder that has a key wins, so a false entry on a
// child folder beats a true entry on its parent. A key must be equal to a
// folder path; it is not a prefix.
func NearestEntry(entries map[string]bool, root string) Entry {
	for dir := root; ; {
		if trusted, ok := entries[dir]; ok {
			return Entry{Found: true, Trusted: trusted, Path: dir, Ancestor: dir != root}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Entry{}
		}
		dir = parent
	}
}

// DecideInput holds every fact that a trust decision uses.
type DecideInput struct {
	Override      Override
	HasGatedFiles bool
	// StoreCorrupt is true when the trust store cannot be read or is not safe.
	StoreCorrupt bool
	// Entry is the result of NearestEntry; it is ignored when StoreCorrupt is true.
	Entry       Entry
	Default     DefaultTrust
	UIAvailable bool
}

// Decision is the result of Decide.
type Decision struct {
	Trusted bool
	Source  TrustSource
	// NeedsPrompt is true when the UI must ask the user. The project is
	// untrusted until the user answers.
	NeedsPrompt bool
}

// Decide returns the trust decision for one session. The rules apply in this
// order, and the first rule that matches decides:
//
//  1. An override approves or denies.
//  2. A project with no gated files is trusted.
//  3. A corrupt or unsafe trust store gives untrusted. The default does not
//     apply, because the store cannot show which folders the user refused.
//  4. A saved entry on the root or on an ancestor decides.
//  5. The default "always" or "never" decides.
//  6. With a UI, the user must answer a prompt; without a UI, the project is
//     untrusted.
//
// Decide never saves a decision: a project that is trusted because it has no
// gated files must be checked again at the next session.
func Decide(in DecideInput) Decision {
	switch in.Override {
	case OverrideApprove:
		return Decision{Trusted: true, Source: SourceFlag}
	case OverrideDeny:
		return Decision{Trusted: false, Source: SourceFlag}
	}
	if !in.HasGatedFiles {
		return Decision{Trusted: true, Source: SourceNoGatedFiles}
	}
	if in.StoreCorrupt {
		return Decision{Trusted: false, Source: SourceStoreError}
	}
	if in.Entry.Found {
		source := SourceSaved
		if in.Entry.Ancestor {
			source = SourceSavedAncestor
		}
		return Decision{Trusted: in.Entry.Trusted, Source: source}
	}
	switch in.Default {
	case DefaultTrustAlways:
		return Decision{Trusted: true, Source: SourceDefaultAlways}
	case DefaultTrustNever:
		return Decision{Trusted: false, Source: SourceDefaultNever}
	}
	if in.UIAvailable {
		return Decision{Trusted: false, Source: SourcePrompt, NeedsPrompt: true}
	}
	return Decision{Trusted: false, Source: SourceNoUI}
}
