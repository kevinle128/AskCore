package app

import (
	"context"

	"go.uber.org/zap"

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
// returns. initial is the demo model, or zero. The demo provider is always
// usable; another provider is usable when service has a credential for it.
func newStartChoice(initial providers.Model, current func() modelSnapshot, service *auth.Service) startChoice {
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
		usable: func(ctx context.Context, provider string) bool {
			if initial.ID != "" && provider == initial.Provider {
				return true
			}
			return providerUsable(ctx, service, provider)
		},
	}
}

// ChooseSavedModel chooses the start model of a run that has no UI and no
// model list refresh, such as print mode. It uses the rules of a new session:
// the saved default when it can be used, else the first usable model in
// display order. The model list comes from the cache in home and the catalog,
// with no network call. initial is the demo model; it is first in display
// order and always usable, so it is the fallback.
//
// warning is one line for stderr when the saved default cannot be used, or
// empty. It names the saved default, the reason and the model that is used. It
// never holds a credential.
func ChooseSavedModel(ctx context.Context, home string, service *auth.Service, initial providers.Model, saved string) (m providers.Model, warning string) {
	cache, _ := settings.NewModelCache(home)
	snap := buildModelSnapshot(ctx, providers.GeneratedCatalog(), readModelCache(ctx, cache, zap.NewNop()), service, zap.NewNop())
	c := newStartChoice(initial, func() modelSnapshot { return snap }, service)
	c.saved = func(context.Context) (string, error) { return saved, nil }
	m, fb := c.pick(ctx)
	if fb != nil {
		warning = "Warning: default model " + fb.SavedDefault + " is not usable (" + fallbackReasonText(fb.Reason) + "); using " + acp.ModelID(m) + "."
	}
	return m, warning
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
