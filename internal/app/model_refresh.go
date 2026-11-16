package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

const (
	// modelListTTL is the age at which a cached model list or overlay is
	// stale. A stale entry is still used; it only starts a refresh.
	modelListTTL = 2 * time.Hour
	// modelRetryDelay is the time after a failed refresh before the next
	// stale check tries again. No timer runs: the next check reads it.
	modelRetryDelay = 5 * time.Minute
	// modelFetchTimeout bounds one fetch of a background refresh. The root
	// context of the refresher has no deadline, so a server that never
	// answers would otherwise hold the refresh until Stop.
	modelFetchTimeout = time.Minute
	// overlayFlight is the in-flight key of the overlay. An account key always
	// holds a '/', so the two key spaces cannot meet.
	overlayFlight = "overlay"
)

// errStaleResult means that the credential changed while a list was fetched,
// so the result belongs to another account and is dropped.
var errStaleResult = errors.New("model list: credential changed during the fetch")

// errModelCache marks a failure of the local model cache file, as opposed to
// a failed vendor request. A vendor failure only keeps the last list, but a
// cache failure means that no result can be kept, so an explicit refresh
// command reports it as a local error.
var errModelCache = errors.New("model cache")

// cacheError marks err as a failure of the local cache. A stale result and
// the end of the context pass through unmarked: an Update callback returns
// the first, and the caller ends the second.
func cacheError(err error) error {
	if err == nil || errors.Is(err, errStaleResult) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: %w", errModelCache, err)
}

// ModelRefresh configures the network refresh of the model list: the
// models.dev metadata overlay and the model lists of the signed-in accounts.
// The zero value turns the refresh off; the runtime then reads the cache only
// and sends no request. Tests keep it off.
type ModelRefresh struct {
	// Enabled turns the refresh on.
	Enabled bool
	// HTTP carries the models.dev request. It is required when Enabled. The
	// account lists use the native sign-in HTTP (ACPParams.AuthHTTP), because
	// only the auth layer holds credentials.
	HTTP *http.Client
	// Now is the clock of the cache age. Nil uses time.Now.
	Now func() time.Time
	// Logger receives the cause of a failed refresh. Nil discards it.
	Logger *zap.Logger
}

// modelSnapshot is one immutable view of the model list.
type modelSnapshot struct {
	// set is the list that a session can select: the catalog, the overlay,
	// the account lists and the user models.json.
	set *providers.ModelSet
	// catalog is the same list without the account lists. A model in catalog
	// and not in set is a model that the account list does not have.
	catalog *providers.ModelSet
	// user holds the key and header tables of the user models.json and its
	// problems.
	user userModels
}

// fileStamp is what a reader compares to see a write of another process.
type fileStamp struct {
	exists  bool
	modTime time.Time
	size    int64
}

// flight is one running refresh of one key. err is set before done closes.
type flight struct {
	generation uint64
	done       chan struct{}
	err        error
}

// modelRefresher owns the model list of a runtime. It loads the list from the
// cache, swaps it atomically, and refreshes it on demand: at start and on a
// model list request when the cache is stale, after a credential change, and
// on an explicit request. It runs no timer and no polling loop. Its goroutines
// run on its own root context, never on the context of a client request, and
// Stop ends them.
type modelRefresher struct {
	cfg     ModelRefresh
	now     func() time.Time
	log     *zap.Logger
	cache   *settings.ModelCache // nil when the home cannot hold a cache
	service *auth.Service
	// base is the built-in and generated catalog, mapped once.
	base []providers.Model
	// providers is the models.dev provider filter, sorted.
	providers []string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	snap atomic.Pointer[modelSnapshot]
	// loadMu orders the loads, so that an older load never replaces the
	// snapshot of a newer one.
	loadMu sync.Mutex
	stamp  fileStamp
	// files is the one loader of the user models.json of the process, and
	// fileGen the generation of the file in the current snapshot.
	files   *settings.ModelsFiles
	fileGen uint64
	// existing is the result of existingProviders, made once.
	existing []string

	mu           sync.Mutex
	stopped      bool
	checking     bool
	flights      map[string]*flight
	overlayRetry time.Time
	generation   func() uint64
}

// newModelRefresher builds a refresher and loads the cache. It makes no
// network call.
func newModelRefresher(home string, service *auth.Service, cfg ModelRefresh) (*modelRefresher, error) {
	if cfg.Enabled && cfg.HTTP == nil {
		return nil, errors.New("model refresh: an HTTP client is required when the refresh is on")
	}
	r := &modelRefresher{
		cfg:       cfg,
		now:       cfg.Now,
		log:       cfg.Logger,
		service:   service,
		base:      providers.GeneratedCatalog(),
		providers: slices.Sorted(slices.Values(providers.ModelsDevProviders())),
		flights:   map[string]*flight{},
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.log == nil {
		r.log = zap.NewNop()
	}
	if cache, err := settings.NewModelCache(home); err == nil {
		r.cache = cache
	} else {
		r.log.Warn("model cache unavailable", zap.Error(err))
	}
	r.existing = existingProviders(r.base, service)
	files, err := settings.NewModelsFiles(home, r.existing)
	if err != nil {
		return nil, err
	}
	r.files = files
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.Load()
	return r, nil
}

// setGeneration installs the change counter of the credential broker. A
// runtime without a broker keeps a constant counter: its credentials change
// only in another process, and that process deletes the cache entries of the
// provider.
func (r *modelRefresher) setGeneration(fn func() uint64) {
	r.mu.Lock()
	r.generation = fn
	r.mu.Unlock()
}

func (r *modelRefresher) currentGeneration() uint64 {
	r.mu.Lock()
	fn := r.generation
	r.mu.Unlock()
	if fn == nil {
		return 0
	}
	return fn()
}

// Current returns the snapshot. It builds the snapshot again first when
// another writer changed the cache file, or when the user models.json changed,
// since the last build. Only the rebuild points call it: the session start,
// the model list and the model switch. A request reads Snapshot.
func (r *modelRefresher) Current() modelSnapshot {
	r.load(false)
	return *r.snap.Load()
}

// Snapshot returns the stored snapshot with an atomic load. It reads no file,
// takes no lock and builds nothing, so the request path uses it.
func (r *modelRefresher) Snapshot() *modelSnapshot { return r.snap.Load() }

func (r *modelRefresher) view() *ModelView { return &ModelView{load: r.Snapshot} }

// Load builds the snapshot from the cache and the user models.json and swaps
// it in. It runs after each write of the cache. It makes no network call. A
// missing, unreadable or unsafe cache counts as empty: the catalog still gives
// a list.
func (r *modelRefresher) Load() { r.load(true) }

// load builds the snapshot when force is set or when an input changed. When
// the user file changed, the cached value commands are forgotten, so a removed
// or changed command does not run again.
func (r *modelRefresher) load(force bool) {
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	// Stat before the read: a write between the two makes the stamp older than
	// the data, so the next check loads again rather than missing the write.
	stamp := r.statCache()
	file, problems, generation := r.files.Load()
	if !force && stamp == r.stamp && generation == r.fileGen {
		return
	}
	if r.fileGen != 0 && generation != r.fileGen && r.service != nil {
		r.service.Values().ForgetAll()
	}
	r.stamp, r.fileGen = stamp, generation
	data := r.readCache()
	var former map[modelKey]bool
	if prev := r.snap.Load(); prev != nil {
		former = prev.user.former
	}
	f := userFile{path: r.files.Path(), file: file, problems: problems, existing: r.existing}
	snap := buildModelSnapshot(context.WithoutCancel(r.ctx), r.base, data, r.service, r.log, f, former)
	r.snap.Store(&snap)
}

func (r *modelRefresher) statCache() fileStamp {
	if r.cache == nil {
		return fileStamp{}
	}
	info, err := os.Stat(r.cache.Path())
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{exists: true, modTime: info.ModTime(), size: info.Size()}
}

func (r *modelRefresher) readCache() settings.ModelCacheData {
	return readModelCache(context.WithoutCancel(r.ctx), r.cache, r.log)
}

// readModelCache reads the cache data. Any failure gives empty data, because
// the cache only narrows and adds to the catalog.
func readModelCache(ctx context.Context, cache *settings.ModelCache, log *zap.Logger) settings.ModelCacheData {
	empty := settings.ModelCacheData{Accounts: map[string]settings.CachedAccount{}}
	if cache == nil {
		return empty
	}
	data, _, err := cache.Read(ctx)
	if err != nil {
		log.Warn("model cache read failed", zap.Error(err))
		return empty
	}
	return data
}

// buildModelSnapshot merges the catalog, the overlay of data, for each
// provider the cached list of its current credential, and the user file f. A
// provider whose current credential has no known list keeps all its rows. A
// saved record that waits for recovery also keeps all rows: its cached list
// does not tell the current access, and a request then gives the recovery
// error instead of an unknown model. Both
// lists get the user file, so a row of the file is in the catalog too and its
// overrides win after each refresh. former is the set of endpoints that the
// tables of earlier snapshots had, or nil.
func buildModelSnapshot(ctx context.Context, base []providers.Model, data settings.ModelCacheData, service *auth.Service, log *zap.Logger, f userFile, former map[modelKey]bool) modelSnapshot {
	var overlay []providers.Model
	if data.Overlay != nil && len(data.Overlay.Payload) > 0 {
		rows, err := providers.MapModelsDev(data.Overlay.Payload)
		if err != nil {
			log.Warn("model cache overlay is not usable", zap.Error(err))
		} else {
			overlay = rows
		}
	}
	accounts := map[string]providers.AccountView{}
	for _, provider := range providerIDs(base) {
		if service.WaitsForRecovery(ctx, provider) {
			continue
		}
		key := service.CurrentAccountKey(ctx, provider, data.Salt)
		entry, ok := data.Accounts[key]
		if key == "" || !ok || entry.Provider != provider || entry.State != settings.AccountModelsKnown {
			continue
		}
		ids := make([]string, 0, len(entry.Items))
		for _, item := range entry.Items {
			ids = append(ids, item.ID)
		}
		accounts[provider] = providers.AccountView{Known: true, IDs: ids}
	}
	mapping := mapModelsFile(f)
	set := providers.NewModelSet(base, overlay, accounts, mapping.config)
	catalog := providers.NewModelSet(base, overlay, nil, mapping.config)
	user := userTables(f, slices.Concat(set.All(), catalog.All()), former)
	user.problems = slices.Concat(f.problems, mapping.problems, mapping.configProblems(set.Problems()))
	return modelSnapshot{set: set, catalog: catalog, user: user}
}

// providerIDs lists the providers of rows once each, in row order.
func providerIDs(rows []providers.Model) []string {
	var out []string
	for _, m := range rows {
		if !slices.Contains(out, m.Provider) {
			out = append(out, m.Provider)
		}
	}
	return out
}

// goRun starts fn on the root context unless the refresher stopped. Stop
// waits for it.
func (r *modelRefresher) goRun(fn func(ctx context.Context)) bool {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return false
	}
	r.wg.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.wg.Done()
		fn(r.ctx)
	}()
	return true
}

// Schedule handles a credential change of provider without blocking: the
// credential broker calls it under its change lock. In the background it
// deletes the cached lists of the provider, because a list under a key with
// no identity part could belong to the old account, loads the snapshot again
// for the new credential and, when the refresh is on, fetches the new list.
func (r *modelRefresher) Schedule(provider string) {
	r.goRun(func(ctx context.Context) {
		// The broker counted the change before it called Schedule, so a fetch
		// that starts here records the new generation.
		generation := r.currentGeneration()
		if r.cache != nil {
			if err := forgetAccountModels(ctx, r.cache, provider); err != nil {
				r.log.Warn("model cache: delete provider lists failed", zap.String("provider", provider), zap.Error(err))
			}
		}
		r.Load()
		if r.cfg.Enabled {
			r.logRefresh(r.refreshAccount(ctx, provider, generation, true), provider)
		}
	})
}

// RefreshIfStale starts, in the background, a refresh of each part of the
// cache that is older than the TTL and not in its retry delay. It returns at
// once and never uses the context of the caller.
func (r *modelRefresher) RefreshIfStale() {
	if !r.cfg.Enabled {
		return
	}
	r.mu.Lock()
	if r.checking {
		r.mu.Unlock()
		return
	}
	r.checking = true
	r.mu.Unlock()
	started := r.goRun(func(ctx context.Context) {
		defer func() {
			r.mu.Lock()
			r.checking = false
			r.mu.Unlock()
		}()
		r.refreshStale(ctx)
	})
	if !started {
		r.mu.Lock()
		r.checking = false
		r.mu.Unlock()
	}
}

func (r *modelRefresher) refreshStale(ctx context.Context) {
	data, err := r.lockedData(ctx)
	if err != nil {
		r.log.Warn("model cache unavailable for refresh", zap.Error(err))
		return
	}
	now := r.now()
	generation := r.currentGeneration()
	var wg sync.WaitGroup
	r.mu.Lock()
	overlayWait := now.Before(r.overlayRetry)
	r.mu.Unlock()
	if !overlayWait && r.overlayStale(data.Overlay, now) {
		wg.Go(func() { r.logRefresh(r.refreshOverlay(ctx, false), "") })
	}
	for _, provider := range providerIDs(r.base) {
		key := r.service.CurrentAccountKey(ctx, provider, data.Salt)
		if key == "" {
			continue
		}
		entry, ok := data.Accounts[key]
		if ok && (now.Sub(entry.FetchedAt) < modelListTTL || now.Before(entry.RetryAfter)) {
			continue
		}
		wg.Go(func() { r.logRefresh(r.refreshAccount(ctx, provider, generation, false), provider) })
	}
	wg.Wait()
}

func (r *modelRefresher) overlayStale(o *settings.ModelOverlay, now time.Time) bool {
	return o == nil || !slices.Equal(o.Providers, r.providers) || now.Sub(o.FetchedAt) >= modelListTTL
}

// Refresh fetches the overlay and the account lists of the named providers
// (every catalog provider when none is named) now, without the TTL and the
// retry delay. It returns when they are done or when ctx or Stop ends them.
func (r *modelRefresher) Refresh(ctx context.Context, names ...string) error {
	if !r.cfg.Enabled {
		return errors.New("model refresh is off")
	}
	// Stop cancels the refresh and waits for it, so no write follows Stop.
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return context.Canceled
	}
	r.wg.Add(1)
	r.mu.Unlock()
	defer r.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	if len(names) == 0 {
		names = providerIDs(r.base)
	}
	generation := r.currentGeneration()
	errs := make([]error, len(names)+1)
	var wg sync.WaitGroup
	wg.Go(func() { errs[0] = r.refreshOverlay(ctx, true) })
	for i, provider := range names {
		wg.Go(func() { errs[i+1] = r.refreshAccount(ctx, provider, generation, true) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// Stop cancels every refresh and waits until each has ended. Later calls do
// nothing.
func (r *modelRefresher) Stop() {
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
	r.cancel()
	r.wg.Wait()
}

func (r *modelRefresher) logRefresh(err error, provider string) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	if errors.Is(err, errStaleResult) {
		r.log.Info("model list dropped after a credential change", zap.String("provider", provider))
		return
	}
	if provider == "" {
		r.log.Warn("model metadata refresh failed", zap.Error(err))
		return
	}
	r.log.Warn("account model list refresh failed", zap.String("provider", provider), zap.Error(err))
}

// lockedData reads the cache under its lock with a no-op update. The update
// creates the file and its salt when there is none, and the salt is needed
// for the key of an API key.
func (r *modelRefresher) lockedData(ctx context.Context) (settings.ModelCacheData, error) {
	return lockedModelCache(ctx, r.cache)
}

func lockedModelCache(ctx context.Context, cache *settings.ModelCache) (settings.ModelCacheData, error) {
	if cache == nil {
		return settings.ModelCacheData{}, fmt.Errorf("%w: unavailable", errModelCache)
	}
	var data settings.ModelCacheData
	err := cache.Update(ctx, func(d *settings.ModelCacheData) error {
		data = *d
		return nil
	})
	return data, cacheError(err)
}

// inFlight runs fn as the one refresh of key. A caller that finds a refresh
// of key that started at the same credential generation waits for it and
// shares its result. A refresh of an older generation can carry the list of
// the old account, so the caller waits for it to end and then runs its own.
func (r *modelRefresher) inFlight(ctx context.Context, key string, generation uint64, fn func() error) error {
	for {
		r.mu.Lock()
		f := r.flights[key]
		if f == nil {
			f = &flight{generation: generation, done: make(chan struct{})}
			r.flights[key] = f
			r.mu.Unlock()
			err := fn()
			r.mu.Lock()
			f.err = err
			delete(r.flights, key)
			r.mu.Unlock()
			close(f.done)
			return err
		}
		r.mu.Unlock()
		select {
		case <-f.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if f.generation == generation {
			return f.err
		}
	}
}

// refreshOverlay fetches the models.dev metadata. It sends the stored ETag
// only when the stored payload has the same provider filter. A failure keeps
// the last payload and starts the retry delay.
func (r *modelRefresher) refreshOverlay(ctx context.Context, force bool) error {
	return r.inFlight(ctx, overlayFlight, 0, func() error {
		data, err := r.lockedData(ctx)
		if err != nil {
			return err
		}
		started := r.now()
		if !force && !r.overlayStale(data.Overlay, started) {
			// Another process refreshed it while this one waited.
			return nil
		}
		etag := ""
		if data.Overlay != nil && slices.Equal(data.Overlay.Providers, r.providers) {
			etag = data.Overlay.ETag
		}
		fetchCtx, cancel := context.WithTimeout(ctx, modelFetchTimeout)
		payload, newETag, notModified, err := providers.FetchModelsDev(fetchCtx, r.cfg.HTTP, etag, r.providers)
		cancel()
		if err == nil && !notModified {
			// Store only a payload that this binary can map.
			_, err = providers.MapModelsDev(payload)
		}
		if err != nil {
			if ctx.Err() == nil {
				r.mu.Lock()
				r.overlayRetry = r.now().Add(modelRetryDelay)
				r.mu.Unlock()
			}
			return err
		}
		err = r.cache.Update(ctx, func(d *settings.ModelCacheData) error {
			if notModified {
				if d.Overlay == nil || !slices.Equal(d.Overlay.Providers, r.providers) || d.Overlay.ETag != etag {
					// The 304 confirms a payload that is no longer stored.
					return errStaleResult
				}
				d.Overlay.FetchedAt = started
				d.Overlay.ETag = newETag
				return nil
			}
			d.Overlay = &settings.ModelOverlay{ETag: newETag, Providers: slices.Clone(r.providers), FetchedAt: started, Payload: payload}
			return nil
		})
		if err != nil {
			return cacheError(err)
		}
		r.Load()
		return nil
	})
}

// refreshAccount fetches the model list of the current credential of
// provider and stores it under the account key. One fetch runs for each key.
// The result is dropped when the broker counted a credential change after
// generation, or when the key of the credential changed.
func (r *modelRefresher) refreshAccount(ctx context.Context, provider string, generation uint64, force bool) error {
	data, err := r.lockedData(ctx)
	if err != nil {
		return err
	}
	key := r.service.CurrentAccountKey(ctx, provider, data.Salt)
	if key == "" {
		return nil
	}
	return r.inFlight(ctx, key, generation, func() error {
		fresh := func() bool { return r.currentGeneration() == generation }
		err := fetchAccountModels(ctx, r.cache, r.service, provider, data.Salt, key, r.now, force, fresh)
		if err == nil {
			r.Load()
		}
		return err
	})
}

// fetchAccountModels runs one account list refresh: fetch outside the cache
// lock, then store under it. salt and key come from the locked read before
// the fetch. fresh reports, under the cache lock, whether the result still
// belongs to the current credential. Without force, a list that another
// writer refreshed meanwhile is not fetched again.
//
// A result is stored with the start time of the fetch, so a newer result of
// another process wins the merge. A method with no list stores an unknown
// entry, so that the TTL also holds for it. A failure keeps the last entry and
// starts the retry delay.
func fetchAccountModels(ctx context.Context, cache *settings.ModelCache, service *auth.Service, provider string, salt []byte, key string, now func() time.Time, force bool, fresh func() bool) error {
	started := now()
	if !force {
		data, _, err := cache.Read(ctx)
		if err == nil {
			if e, ok := data.Accounts[key]; ok && started.Sub(e.FetchedAt) < modelListTTL {
				return nil
			}
		}
	}
	fetchCtx, cancel := context.WithTimeout(ctx, modelFetchTimeout)
	res, fetchErr := service.ListModels(fetchCtx, provider, salt)
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if res.Key != key {
		return errStaleResult
	}
	if res.Method == "" {
		return fetchErr
	}
	err := cache.Update(ctx, func(d *settings.ModelCacheData) error {
		if !bytes.Equal(d.Salt, salt) || (fresh != nil && !fresh()) {
			return errStaleResult
		}
		if fetchErr != nil {
			entry, ok := d.Accounts[key]
			if !ok {
				entry = settings.CachedAccount{Provider: provider, Method: res.Method, State: settings.AccountModelsUnknown}
			}
			entry.RetryAfter = now().Add(modelRetryDelay)
			d.Accounts[key] = entry
			return nil
		}
		entry := settings.CachedAccount{Provider: provider, Method: res.Method, FetchedAt: started, State: res.State}
		for _, item := range res.Items {
			entry.Items = append(entry.Items, settings.CachedModel{ID: item.ID, DisplayName: item.DisplayName})
		}
		d.Accounts[key] = entry
		return nil
	})
	if err != nil {
		return cacheError(err)
	}
	if fetchErr != nil {
		return fmt.Errorf("list %s models: %w", provider, fetchErr)
	}
	return nil
}

// RefreshAccountModels fetches the model list of the current credential of
// provider once and stores it in the cache of home. `ask auth login` uses it
// in its own process, with no leader. The caller deletes the old lists of the
// provider first.
func RefreshAccountModels(ctx context.Context, home string, service *auth.Service, provider string) error {
	cache, err := settings.NewModelCache(home)
	if err != nil {
		return err
	}
	data, err := lockedModelCache(ctx, cache)
	if err != nil {
		return err
	}
	key := service.CurrentAccountKey(ctx, provider, data.Salt)
	if key == "" {
		return nil
	}
	return fetchAccountModels(ctx, cache, service, provider, data.Salt, key, time.Now, true, nil)
}

// ForgetAccountModels deletes the cached model lists of provider in home. A
// sign-in or a sign-out calls it, because a key with no identity part does
// not tell two accounts apart.
func ForgetAccountModels(ctx context.Context, home, provider string) error {
	cache, err := settings.NewModelCache(home)
	if err != nil {
		return err
	}
	return forgetAccountModels(ctx, cache, provider)
}

// forgetAccountModels deletes the lists of provider. With no cache file there
// is nothing to delete, and a sign-out must not create the file.
func forgetAccountModels(ctx context.Context, cache *settings.ModelCache, provider string) error {
	if _, status, err := cache.Read(ctx); err == nil && status == settings.ModelCacheMissing {
		return nil
	}
	return cache.DeleteProvider(ctx, provider)
}
