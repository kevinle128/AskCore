package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"AskCore/internal/acp"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/pkg/protocol"
)

// startChoice holds what the choice of the start model of a new session needs.
type startChoice struct {
	// initial is the demo model. It is zero unless the demo provider is enabled.
	initial providers.Model
	// rows lists the model list in display order.
	rows func() []providers.Model
	// find resolves one row of the model list.
	find func(providers.Ref) (providers.Model, error)
	// inCatalog tells whether any source has the model, also when the account
	// list does not. Nil means only the list counts.
	inCatalog func(providers.Ref) bool
	// usable tells whether a provider can serve models now.
	usable func(ctx context.Context, provider string) bool
	// saved returns the saved default model id, or "".
	saved func(ctx context.Context) (string, error)
}

// newStartChoice builds the start choice over the model list that current
// returns. initial is the demo model, or zero. usable is the usable rule (see
// usableRule).
func newStartChoice(initial providers.Model, current func() modelSnapshot, usable func(ctx context.Context, provider string) bool) startChoice {
	isInitial := func(ref providers.Ref) bool {
		return initial.ID != "" && ref.Provider == initial.Provider && ref.ID == initial.ID && (ref.API == "" || ref.API == initial.API)
	}
	return startChoice{
		initial: initial,
		rows: func() []providers.Model {
			set := current().set.All()
			if initial.ID == "" {
				return set
			}
			return append([]providers.Model{initial}, set...)
		},
		find: func(ref providers.Ref) (providers.Model, error) {
			if isInitial(ref) {
				return initial, nil
			}
			return current().set.Find(ref)
		},
		// inCatalog tells a model that the account list removed from a model
		// that no source has.
		inCatalog: func(ref providers.Ref) bool {
			return isInitial(ref) || current().catalog.Contains(ref)
		},
		usable: usable,
	}
}

// ModelOptions are the model options of a session start: the command line
// flags of print mode, or the session options of a client.
type ModelOptions struct {
	// Pattern is the start model pattern, [provider/]id[@api][:level], or
	// empty. It wins over the scope and the saved default.
	Pattern string
	// Thinking is the explicit start thinking level, or empty. It wins over
	// the level of Pattern and of a scope entry.
	Thinking protocol.ThinkingLevel
	// Scope are the scope entries of --models. Empty uses the enabledModels
	// setting.
	Scope []string
	// PreferAPI picks the row of this API when Pattern has no "@api" and
	// names one id on more than one API. Empty keeps such a pattern
	// ambiguous.
	PreferAPI providers.API
}

// ModelChoice is the start model of a session and what the choice found.
type ModelChoice struct {
	// Model is the start model; the zero model means no provider is usable.
	Model providers.Model
	// Level is the start thinking level, or empty to keep the default of the
	// session. A level that the model drops is already off here.
	Level protocol.ThinkingLevel
	// Fallback names the saved default that could not be used, or nil.
	Fallback *protocol.ACPModelFallback
	// Dropped is the requested level that the model did not use because it
	// has no thinking, or nil.
	Dropped *protocol.ACPLevelDropped
	// Scope are the scope entries of the session: --models, else
	// enabledModels.
	Scope []string
	// Diagnostics are the problems of the scope entries.
	Diagnostics []settings.Diagnostic
}

// FallbackWarning is one line for stderr when the saved default could not be
// used, or "". It names the saved default, the reason and the model that is
// used. It never holds a credential.
func (c ModelChoice) FallbackWarning() string {
	if c.Fallback == nil {
		return ""
	}
	return "Warning: default model " + c.Fallback.SavedDefault + " is not usable (" + fallbackReasonText(c.Fallback.Reason) + "); using " + acp.ModelID(c.Model) + "."
}

// ChooseStartModel chooses the start model of a run that has no UI and no
// model list refresh, such as print mode, with the rules of a new session
// (see startChoice.choose). The model list comes from view, which
// LoadModelView built with no network call; a nil view has the demo model
// only. initial is the demo model; it is first in display order and always
// usable, so it is the last fallback. eff gives the saved default and the
// enabledModels setting.
func ChooseStartModel(ctx context.Context, view *ModelView, service *auth.Service, initial providers.Model, eff settings.Effective, opts ModelOptions) (ModelChoice, error) {
	snap := view.snapshot()
	if snap == nil {
		snap = &modelSnapshot{}
	}
	c := newStartChoice(initial, func() modelSnapshot { return *snap }, usableRule(initial, service, func() *modelSnapshot { return snap }))
	c.saved = func(context.Context) (string, error) { return eff.DefaultModel, nil }
	return c.choose(ctx, opts, eff)
}

// fallbackReasonText words why the saved default was not used. The TUI uses
// the same words in its start notice.
func fallbackReasonText(r protocol.ACPFallbackReason) string {
	switch r {
	case protocol.ACPFallbackNoCredential:
		return "no credential"
	case protocol.ACPFallbackNotInCatalog:
		return "not in the catalog"
	case protocol.ACPFallbackNotInAccount:
		return "not in account list"
	}
	return "not available"
}

// choose chooses the start model of a new session, in this order:
//
//  1. the model of opts.Pattern; a pattern that does not resolve is an error.
//     A thinking level suffix that is not a level is an unknown model here;
//  2. the scope (opts.Scope, else the enabledModels setting): the saved
//     default when it is in the scope and usable, else the first scope row
//     of a usable provider;
//  3. the saved default, then the first usable model (see pick).
//
// The level is opts.Thinking, else the level of the pattern or of the scope
// entry. When the chosen model has no thinking, a requested level other than
// off is dropped: Level is off and Dropped names it.
func (c startChoice) choose(ctx context.Context, opts ModelOptions, eff settings.Effective) (ModelChoice, error) {
	rows := c.rows()
	out := ModelChoice{Scope: slices.Clone(opts.Scope)}
	fromFlag := len(opts.Scope) > 0
	if !fromFlag {
		out.Scope = slices.Clone(eff.EnabledModels)
	}
	var scoped []providers.ScopedModel
	if len(out.Scope) > 0 {
		var problems []providers.ScopeProblem
		scoped, problems = providers.ExpandScope(rows, out.Scope)
		out.Diagnostics = scopeDiagnostics(rows, out.Scope, problems, fromFlag, eff)
	}
	var level protocol.ThinkingLevel
	switch {
	case opts.Pattern != "":
		r, err := resolveStartPattern(rows, opts.Pattern, opts.PreferAPI)
		if err != nil {
			return ModelChoice{}, err
		}
		out.Model, level = r.Model, r.Level
	default:
		if sm, ok := c.pickScoped(ctx, scoped); ok {
			out.Model, level = sm.Model, sm.Level
			break
		}
		out.Model, out.Fallback = c.pick(ctx)
	}
	if opts.Thinking != "" {
		level = opts.Thinking
	}
	if out.Model.ID != "" && level != "" && level != protocol.ThinkingOff && !out.Model.Reasoning {
		out.Dropped = &protocol.ACPLevelDropped{ModelID: acp.ModelID(out.Model), Level: level}
		level = protocol.ThinkingOff
	}
	out.Level = level
	return out, nil
}

// pickScoped returns the saved default when it is in the scope and can be
// used, else the first scope row of a usable provider.
func (c startChoice) pickScoped(ctx context.Context, scoped []providers.ScopedModel) (providers.ScopedModel, bool) {
	if len(scoped) == 0 {
		return providers.ScopedModel{}, false
	}
	if c.saved != nil {
		if id, err := c.saved(ctx); err == nil && id != "" {
			if saved, reason := c.savedModel(ctx, id); reason == "" {
				for _, sm := range scoped {
					if acp.ModelID(sm.Model) == acp.ModelID(saved) {
						return sm, true
					}
				}
			}
		}
	}
	for _, sm := range scoped {
		if c.usable(ctx, sm.Model.Provider) {
			return sm, true
		}
	}
	return providers.ScopedModel{}, false
}

// resolveStartPattern resolves the start pattern with no invalid-level
// fallback, so a typo in the level never starts a session with another level.
// When the pattern names one id on two APIs and prefer is set, the row of
// prefer wins.
func resolveStartPattern(rows []providers.Model, pattern string, prefer providers.API) (providers.Resolved, error) {
	r, err := providers.ResolvePattern(rows, pattern, providers.PatternOptions{})
	var me *providers.ModelError
	if prefer == "" || !errors.As(err, &me) || !errors.Is(err, providers.ErrAmbiguousModel) || strings.Contains(pattern, "@") {
		return r, err
	}
	// The choices are "provider/id@api" names; one id on two APIs is the only
	// ambiguity that prefer settles.
	base := ""
	for _, choice := range me.Choices {
		name, _, _ := strings.Cut(choice, "@")
		if base != "" && name != base {
			return r, err
		}
		base = name
	}
	retry := pattern + "@" + string(prefer)
	if i := strings.LastIndex(pattern, ":"); i >= 0 && validLevel(protocol.ThinkingLevel(pattern[i+1:])) {
		retry = pattern[:i] + "@" + string(prefer) + pattern[i:]
	}
	if again, rerr := providers.ResolvePattern(rows, retry, providers.PatternOptions{}); rerr == nil {
		return again, nil
	}
	return r, err
}

func validLevel(l protocol.ThinkingLevel) bool {
	switch l {
	case protocol.ThinkingOff, protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
		protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax:
		return true
	}
	return false
}

// scopeDiagnostics turns the problems of the scope entries into settings
// diagnostics. An entry of --models is the text of the user, so its own text
// is shown under the key "--models". An entry of the enabledModels setting is
// file content, so the diagnostic names its position and a fixed text only.
func scopeDiagnostics(rows []providers.Model, entries []string, problems []providers.ScopeProblem, fromFlag bool, eff settings.Effective) []settings.Diagnostic {
	var out []settings.Diagnostic
	for _, p := range problems {
		if fromFlag {
			out = append(out, settings.Diagnostic{Key: "--models", Text: p.Text})
			continue
		}
		out = append(out, settings.Diagnostic{
			Layer: eff.Sources[settings.EnabledModelsKey],
			Path:  eff.KeyPath(settings.EnabledModelsKey),
			Key:   fmt.Sprintf("%s[%d]", settings.EnabledModelsKey, slices.Index(entries, p.Entry)),
			Text:  scopeProblemText(rows, p),
		})
	}
	return out
}

// scopeProblemText is the fixed text of one scope problem. A glob problem has
// a fixed text already; a pattern problem is read again to learn its class.
func scopeProblemText(rows []providers.Model, p providers.ScopeProblem) string {
	if strings.ContainsAny(p.Entry, "*?[") {
		return p.Text
	}
	r, err := providers.ResolvePattern(rows, p.Entry, providers.PatternOptions{AllowInvalidLevel: true})
	switch {
	case errors.Is(err, providers.ErrAmbiguousModel):
		return "names more than one model; it is ignored"
	case err != nil:
		return "matches no model; it is ignored"
	case r.Warning != "":
		return "the thinking level is not valid; the default level is used"
	}
	return "is not valid; it is ignored"
}

// pick chooses the model of a new session, in this order:
//
//  1. the saved default, when it is in the model list and its provider is
//     usable;
//  2. the recommended model of the first usable provider in display order,
//     when the model list has it;
//  3. the first row of that provider in display order;
//  4. no model.
//
// When a saved default exists but cannot be used and another model was chosen,
// the fallback names the saved default and why it was not used.
func (c startChoice) pick(ctx context.Context) (providers.Model, *protocol.ACPModelFallback) {
	rows := c.rows()
	var fallback *protocol.ACPModelFallback
	if c.saved != nil {
		if id, err := c.saved(ctx); err == nil && id != "" {
			m, reason := c.savedModel(ctx, id)
			if reason == "" {
				return m, nil
			}
			fallback = &protocol.ACPModelFallback{SavedDefault: id, Reason: reason}
		}
	}
	if m, ok := c.firstUsable(ctx, rows); ok {
		return m, fallback
	}
	return providers.Model{}, nil
}

// savedModel resolves the saved default. reason is empty when the model can be
// used.
func (c startChoice) savedModel(ctx context.Context, id string) (providers.Model, protocol.ACPFallbackReason) {
	ref, err := acp.ParseModelID(id)
	if err != nil {
		return providers.Model{}, protocol.ACPFallbackNotInCatalog
	}
	m, err := c.find(ref)
	if err != nil {
		if c.inCatalog != nil && c.inCatalog(ref) {
			return providers.Model{}, protocol.ACPFallbackNotInAccount
		}
		return providers.Model{}, protocol.ACPFallbackNotInCatalog
	}
	if !c.usable(ctx, m.Provider) {
		return providers.Model{}, protocol.ACPFallbackNoCredential
	}
	return m, ""
}

// firstUsable returns the recommended model of the first usable provider in
// display order, or that provider's first row when the list does not have the
// recommended model.
func (c startChoice) firstUsable(ctx context.Context, rows []providers.Model) (providers.Model, bool) {
	for _, m := range rows {
		if !c.usable(ctx, m.Provider) {
			continue
		}
		if rec, ok := c.recommendedModel(m.Provider); ok {
			return rec, true
		}
		return m, true
	}
	return providers.Model{}, false
}

// recommendedModel returns the recommended row of a provider when the model
// list has it. The demo provider has one row, which is its recommended model.
func (c startChoice) recommendedModel(provider string) (providers.Model, bool) {
	if c.initial.ID != "" && c.initial.Provider == provider {
		return c.initial, true
	}
	ref, ok := providers.RecommendedModel(provider)
	if !ok {
		return providers.Model{}, false
	}
	m, err := c.find(ref)
	if err != nil {
		return providers.Model{}, false
	}
	return m, true
}
