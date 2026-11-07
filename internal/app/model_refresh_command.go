package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

// The states of one provider after an explicit refresh.
const (
	// ModelListKnown means that this refresh read the account list.
	ModelListKnown = "known"
	// ModelListUnknown means that this refresh could not read the account
	// list, or that the sign-in method has no list. Every catalog model of the
	// provider stays in the list.
	ModelListUnknown = "unknown"
	// ModelListNoCredential means that the provider has no credential, so no
	// list request was sent.
	ModelListNoCredential = "no credential"
)

// The states of the models.dev metadata after an explicit refresh.
const (
	MetadataUpdated     = "updated"
	MetadataNotModified = "not modified"
	MetadataFailed      = "failed"
)

// ProviderModels is the result of an explicit refresh for one provider.
type ProviderModels struct {
	Provider string
	// Models is the number of models of the provider in the model list after
	// the refresh: the list that /model shows when the provider is signed in.
	Models int
	// State is ModelListKnown, ModelListUnknown or ModelListNoCredential.
	State string
}

// ModelListReport is the result of RefreshModelLists. It holds model counts
// and states only: no credential, no key and no vendor message text.
type ModelListReport struct {
	Providers []ProviderModels
	// Metadata is MetadataUpdated, MetadataNotModified or MetadataFailed.
	Metadata string
}

// ModelListProviders returns the providers of the model catalog, in catalog
// order. These are the providers that RefreshModelLists accepts.
func ModelListProviders() []string {
	return providerIDs(providers.GeneratedCatalog())
}

// RefreshModelLists runs one explicit refresh of the model cache in home: the
// models.dev metadata and the account lists of the named providers (every
// catalog provider when none is named), without the TTL and the retry delay.
// It runs in the process of the caller, with no leader: a leader sees the new
// cache file at its next model list.
//
// A failed vendor request is not an error; its provider is ModelListUnknown or
// the metadata is MetadataFailed, and the cache keeps the last good data. The
// error is for a local failure only, such as a cache file that cannot be
// written, or for the end of ctx.
func RefreshModelLists(ctx context.Context, home string, service *auth.Service, cfg ModelRefresh, names ...string) (ModelListReport, error) {
	all := ModelListProviders()
	for _, name := range names {
		if !slices.Contains(all, name) {
			return ModelListReport{}, fmt.Errorf("unknown model provider %q", name)
		}
	}
	if len(names) == 0 {
		names = all
	}
	r, err := newModelRefresher(home, service, cfg)
	if err != nil {
		return ModelListReport{}, err
	}
	defer r.Stop()
	// The locked read creates the file and its salt, so a home that cannot
	// hold the cache fails here, before any request.
	before, err := r.lockedData(ctx)
	if err != nil {
		return ModelListReport{}, err
	}
	// Each write of this refresh records a fetch start at or after start, so
	// an entry older than start was not written by this refresh.
	start := r.now()
	err = r.Refresh(ctx, names...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ModelListReport{}, ctxErr
	}
	if errors.Is(err, errModelCache) {
		return ModelListReport{}, err
	}
	after, _, err := r.cache.Read(ctx)
	if err != nil {
		return ModelListReport{}, cacheError(err)
	}
	set := buildModelSnapshot(ctx, r.base, after, service, r.log, userFile{}, nil).set
	report := ModelListReport{Metadata: overlayState(before.Overlay, after.Overlay, start)}
	for _, provider := range names {
		report.Providers = append(report.Providers, ProviderModels{
			Provider: provider,
			Models:   len(set.Models(provider)),
			State:    accountState(ctx, service, provider, after, start),
		})
	}
	return report, nil
}

// accountState reads the result of this refresh for provider from the cache.
// A failed request keeps the last list of the account, so only an entry that
// this refresh fetched counts as known.
func accountState(ctx context.Context, service *auth.Service, provider string, data settings.ModelCacheData, start time.Time) string {
	key := service.CurrentAccountKey(ctx, provider, data.Salt)
	if key == "" {
		return ModelListNoCredential
	}
	entry, ok := data.Accounts[key]
	if ok && entry.State == settings.AccountModelsKnown && !entry.FetchedAt.Before(start) {
		return ModelListKnown
	}
	return ModelListUnknown
}

// overlayState compares the metadata before and after this refresh. A 304
// answer and a 200 answer with the same payload both leave the data as it was.
func overlayState(before, after *settings.ModelOverlay, start time.Time) string {
	if after == nil || after.FetchedAt.Before(start) {
		return MetadataFailed
	}
	if before != nil && before.ETag == after.ETag && bytes.Equal(before.Payload, after.Payload) {
		return MetadataNotModified
	}
	return MetadataUpdated
}
