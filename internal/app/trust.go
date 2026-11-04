package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"AskCore/internal/acp"
	"AskCore/internal/settings"
	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

// trustFileName is the name of the trust store in the Ask home. The settings
// package owns the file; the name is here only for the warning text.
const trustFileName = "trust.json"

// TrustOutcome is the trust decision for the working directory of one session
// and the facts that the decision used.
type TrustOutcome struct {
	// Root is the canonical project root of the cwd.
	Root string
	// HasLayer is false when <Root>/.ask is the Ask home: the session then has
	// no project layer.
	HasLayer bool
	// Gated lists the gated entries that exist (see workspace.GatedFiles).
	Gated []string
	// Entry is the saved trust entry that applies, if any.
	Entry    workspace.Entry
	Decision workspace.Decision
	// Warning is a fixed text with a path, or empty. It never holds file
	// content.
	Warning string
}

// Project returns the project of the settings layers of the session.
func (o TrustOutcome) Project() settings.Project {
	if !o.HasLayer {
		return settings.Project{}
	}
	return settings.Project{Root: o.Root, Trusted: o.Decision.Trusted}
}

// ResolveTrust decides the project trust of a session cwd. It finds the
// canonical root, checks the gated entries with Lstat, reads the trust store
// and the user settings file, and applies workspace.Decide. ui tells whether
// the caller can ask the user. It never opens a file of the project, and it
// writes nothing.
//
// A trust store that cannot be read gives an untrusted decision and a
// warning, not an error. The error is for a cwd that has no root, and for a
// done ctx.
func ResolveTrust(ctx context.Context, home, userHome, cwd string, override workspace.Override, ui bool) (TrustOutcome, error) {
	askHome, err := settings.ResolveHome(home)
	if err != nil {
		return TrustOutcome{}, err
	}
	root, hasLayer, err := workspace.ProjectRoot(cwd, askHome)
	if err != nil {
		return TrustOutcome{}, err
	}
	out := TrustOutcome{Root: root, HasLayer: hasLayer}
	var warnings []string
	gated, gatedErr := workspace.GatedFiles(root, hasLayer, userHome)
	out.Gated = gated
	if gatedErr != nil {
		// An entry that cannot be checked can exist, so the project counts as gated.
		warnings = append(warnings, "cannot check the project files of "+root+"; the project needs a trust decision")
	}
	entries, storeErr := readTrustStore(ctx, askHome)
	if err := ctx.Err(); err != nil {
		return TrustOutcome{}, err
	}
	if storeErr != nil {
		warnings = append(warnings, trustStoreWarning(askHome))
	} else {
		out.Entry = workspace.NearestEntry(entries, root)
	}
	user, err := settings.NewLayered(askHome, settings.Project{})
	if err != nil {
		return TrustOutcome{}, err
	}
	view, err := user.UserLayer(ctx)
	if err != nil {
		return TrustOutcome{}, err
	}
	out.Decision = workspace.Decide(workspace.DecideInput{
		Override:      override,
		HasGatedFiles: len(gated) > 0 || gatedErr != nil,
		StoreCorrupt:  storeErr != nil,
		Entry:         out.Entry,
		Default:       workspace.DefaultTrust(view.DefaultProjectTrust),
		UIAvailable:   ui,
	})
	out.Warning = strings.Join(warnings, "; ")
	return out, nil
}

// LoadSessionSettings decides the project trust of a session with no UI and
// then loads its settings layers. The decision comes first, so the project
// file is opened only when the project is trusted.
func LoadSessionSettings(ctx context.Context, home, userHome, cwd string, override workspace.Override) (TrustOutcome, settings.Effective, error) {
	decision, err := ResolveTrust(ctx, home, userHome, cwd, override, false)
	if err != nil {
		return TrustOutcome{}, settings.Effective{}, err
	}
	layered, err := settings.NewLayered(home, decision.Project())
	if err != nil {
		return TrustOutcome{}, settings.Effective{}, err
	}
	eff, err := layered.Load(ctx)
	if err != nil {
		return TrustOutcome{}, settings.Effective{}, err
	}
	return decision, eff, nil
}

func readTrustStore(ctx context.Context, askHome string) (map[string]bool, error) {
	store, err := settings.NewTrustStore(askHome)
	if err != nil {
		return nil, err
	}
	return store.Read(ctx)
}

// trustStoreWarning is the warning of a trust store that cannot be read or
// changed. The error text of the store is not used: the warning has a fixed
// text and the path only.
func trustStoreWarning(askHome string) string {
	return "trust store " + filepath.Join(askHome, trustFileName) + " is corrupt or unsafe; no project is trusted until you fix or remove it"
}

// sessionTrust converts the start facts of a session to the adapter value. The
// context and skill diagnostics ride in the same list as the settings ones.
func sessionTrust(s SessionStart) *acp.SessionTrust {
	t := &acp.SessionTrust{
		Trust:        protocol.ACPSessionTrust{Trusted: s.Trust.Decision.Trusted, Source: string(s.Trust.Decision.Source)},
		ThinkingFold: string(s.Settings.ThinkingFold),
	}
	for _, d := range s.Settings.Diagnostics {
		t.Diagnostics = append(t.Diagnostics, protocol.ACPDiagnostic{Layer: string(d.Layer), Path: d.Path, Key: d.Key, Text: d.Text})
	}
	t.Diagnostics = append(t.Diagnostics, s.contextDiagnostics()...)
	t.Diagnostics = append(t.Diagnostics, s.SkillDiagnostics()...)
	return t
}

// stateResult is the wire form of an outcome.
func (o TrustOutcome) stateResult() protocol.ACPTrustStateResult {
	res := protocol.ACPTrustStateResult{
		Root:          o.Root,
		Trusted:       o.Decision.Trusted,
		Source:        string(o.Decision.Source),
		NeedsDecision: o.Decision.NeedsPrompt,
		Gated:         o.Gated,
		Warning:       o.Warning,
	}
	if o.Entry.Found {
		res.EntryPath = o.Entry.Path
	}
	return res
}

// trustSetup serves the trust methods of the leader and of the in-process TUI
// over the trust store of the Ask home.
type trustSetup struct {
	// home is the resolved Ask home.
	home     string
	userHome string
}

var _ acp.TrustSetup = trustSetup{}

// State decides as for a client that can ask the user, so NeedsDecision is
// true only when the user must answer.
func (t trustSetup) State(ctx context.Context, req protocol.ACPTrustStateRequest) (protocol.ACPTrustStateResult, error) {
	out, err := ResolveTrust(ctx, t.home, t.userHome, req.Cwd, workspace.OverrideNone, true)
	if err != nil {
		return protocol.ACPTrustStateResult{}, err
	}
	return out.stateResult(), nil
}

// Set saves a decision for the canonical root of the cwd or for its parent. A
// decision for the parent also removes the entry of the root, so that the
// parent entry applies to it. A corrupt or unsafe store is never changed.
func (t trustSetup) Set(ctx context.Context, req protocol.ACPTrustSetRequest) (protocol.ACPTrustStateResult, error) {
	root, _, err := workspace.ProjectRoot(req.Cwd, t.home)
	if err != nil {
		return protocol.ACPTrustStateResult{}, err
	}
	var value *bool
	switch req.Decision {
	case protocol.ACPTrustDecisionTrust:
		value = new(true)
	case protocol.ACPTrustDecisionDistrust:
		value = new(false)
	case protocol.ACPTrustDecisionClear:
	default:
		return protocol.ACPTrustStateResult{}, fmt.Errorf("trust: unknown decision %q", req.Decision)
	}
	edit := map[string]*bool{}
	switch req.Target {
	case protocol.ACPTrustTargetFolder:
		edit[root] = value
	case protocol.ACPTrustTargetParent:
		parent := filepath.Dir(root)
		if parent == root {
			return protocol.ACPTrustStateResult{}, errors.New("trust: the file system root has no parent folder")
		}
		edit[parent] = value
		if value != nil {
			edit[root] = nil
		}
	default:
		return protocol.ACPTrustStateResult{}, fmt.Errorf("trust: unknown target %q", req.Target)
	}
	store, err := settings.NewTrustStore(t.home)
	if err != nil {
		return protocol.ACPTrustStateResult{}, err
	}
	if err := store.Update(ctx, edit); err != nil {
		if errors.Is(err, settings.ErrUnsafeStore) {
			return protocol.ACPTrustStateResult{}, &acp.TrustStoreError{Warning: trustStoreWarning(t.home)}
		}
		return protocol.ACPTrustStateResult{}, err
	}
	return t.State(ctx, protocol.ACPTrustStateRequest{Cwd: req.Cwd})
}
