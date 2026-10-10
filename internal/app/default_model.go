package app

import (
	"context"

	"AskCore/internal/acp"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// startChoice holds what the choice of the start model of a new session needs.
type startChoice struct {
	// initial is the demo model. It is zero unless the demo provider is enabled.
	initial providers.Model
	// rows lists the catalog in display order.
	rows func() []providers.Model
	// find resolves one catalog row.
	find func(providers.Ref) (providers.Model, error)
	// usable tells whether a provider can serve models now.
	usable func(ctx context.Context, provider string) bool
	// saved returns the saved default model id, or "".
	saved func(ctx context.Context) (string, error)
}

// pick chooses the model of a new session, in this order:
//
//  1. the saved default, when its provider is usable;
//  2. the recommended model of the first usable provider in catalog order;
//  3. the first usable row in catalog order;
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
		return providers.Model{}, protocol.ACPFallbackNotInCatalog
	}
	if !c.usable(ctx, m.Provider) {
		return providers.Model{}, protocol.ACPFallbackNoCredential
	}
	return m, ""
}

// firstUsable returns the recommended model of the first usable provider in
// catalog order, or that provider's first row when it has none.
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

// recommendedModel returns the recommended row of a provider. The demo provider
// has one row, which is its recommended model.
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
