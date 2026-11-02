package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/pkg/protocol"
)

// fakeVendor answers the OpenAI model list and the models.dev payload in
// memory. A list request can be held until the test releases it; it also
// ends when its request context ends, as a real transport does.
type fakeVendor struct {
	mu        sync.Mutex
	lists     int
	active    int
	maxActive int
	listIDs   []string
	listCode  int
	etags     []string
	devCode   int

	hold    chan struct{} // nil: answer at once
	started chan struct{} // gets one value for each list request
}

func newFakeVendor(ids ...string) *fakeVendor {
	return &fakeVendor{listIDs: ids, listCode: http.StatusOK, devCode: http.StatusOK, started: make(chan struct{}, 64)}
}

func (f *fakeVendor) RoundTrip(req *http.Request) (*http.Response, error) {
	switch {
	case req.URL.Host == "api.openai.com" && req.URL.Path == "/v1/models":
		return f.list(req)
	case req.URL.Host == "models.dev":
		return f.modelsDev(req)
	}
	return nil, io.ErrUnexpectedEOF
}

func (f *fakeVendor) list(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.lists++
	f.active++
	f.maxActive = max(f.maxActive, f.active)
	hold, code := f.hold, f.listCode
	data := make([]map[string]string, 0, len(f.listIDs))
	for _, id := range f.listIDs {
		data = append(data, map[string]string{"id": id})
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	f.started <- struct{}{}
	if hold != nil {
		select {
		case <-hold:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	body, _ := json.Marshal(map[string]any{"data": data})
	return response(code, string(body), ""), nil
}

func (f *fakeVendor) modelsDev(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.etags = append(f.etags, req.Header.Get("If-None-Match"))
	if f.devCode != http.StatusOK {
		return response(f.devCode, "", ""), nil
	}
	if req.Header.Get("If-None-Match") == `"v1"` {
		return response(http.StatusNotModified, "", `"v1"`), nil
	}
	return response(http.StatusOK, `{"openai":{"models":{}}}`, `"v1"`), nil
}

func (f *fakeVendor) counts() (lists, maxActive int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists, f.maxActive
}

func response(code int, body, etag string) *http.Response {
	h := http.Header{"Content-Type": {"application/json"}}
	if etag != "" {
		h.Set("ETag", etag)
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

// testClock is a clock that a test moves.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// refreshRuntime builds a runtime with the refresh on, over fake vendors. A
// list request is held until the test closes hold (nil: not held).
func refreshRuntime(t *testing.T, home string, env map[string]string, vendor *fakeVendor, clock *testClock, log *zap.Logger) *ACPRuntime {
	t.Helper()
	p := testACPParams(t)
	p.Home = home
	p.Initial = providers.Model{}
	p.Env = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	p.AuthHTTP = &http.Client{Transport: vendor}
	p.ModelRefresh = ModelRefresh{Enabled: true, HTTP: &http.Client{Transport: vendor}, Now: clock.Now, Logger: log}
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Cleanup(context.Background()) })
	return rt
}

func privateHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(home, 0o700))
	return home
}

func openAIRows(set *providers.ModelSet) []string {
	var ids []string
	for _, m := range set.Models(providers.ProviderOpenAI) {
		ids = append(ids, m.ID)
	}
	return ids
}

// seedAccount stores a known list for an OpenAI API key, as another process
// would.
func seedAccount(t *testing.T, home, apiKey string, ids ...string) {
	t.Helper()
	cache, err := settings.NewModelCache(home)
	require.NoError(t, err)
	require.NoError(t, cache.Update(context.Background(), func(d *settings.ModelCacheData) error {
		key := auth.AccountKey(providers.ProviderOpenAI, "api-key", settings.Credential{Method: "api-key", APIKey: apiKey}, d.Salt)
		entry := settings.CachedAccount{Provider: providers.ProviderOpenAI, Method: "api-key", FetchedAt: time.Now(), State: settings.AccountModelsKnown}
		for _, id := range ids {
			entry.Items = append(entry.Items, settings.CachedModel{ID: id})
		}
		d.Accounts[key] = entry
		return nil
	}))
}

func TestModelRefreshLoadEmptyCacheGivesCatalog(t *testing.T) {
	p := testACPParams(t)
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	got := rt.models.Current().set.All()
	require.Len(t, got, len(providers.GeneratedCatalog()), "no cache: every catalog row")
	_, err = os.Stat(filepath.Join(p.Home, "model-cache.json"))
	require.ErrorIs(t, err, os.ErrNotExist, "a load with the refresh off writes nothing")
}

func TestModelRefreshLoadUnsafeCacheIsEmpty(t *testing.T) {
	home := privateHome(t)
	seedAccount(t, home, "sk-a", "gpt-5.5")
	require.NoError(t, os.Chmod(filepath.Join(home, "model-cache.json"), 0o644))
	p := testACPParams(t)
	p.Home = home
	p.Env = func(k string) (string, bool) { return "sk-a", k == "OPENAI_API_KEY" }
	rt, err := NewACPRuntime(p)
	require.NoError(t, err, "an unsafe cache file is not an error")
	require.Greater(t, len(openAIRows(rt.models.Current().set)), 1, "the unsafe file is ignored: all OpenAI rows stay")
}

func TestModelRefreshKnownListNarrowsRowsAndReloadsOnWrite(t *testing.T) {
	home := privateHome(t)
	p := testACPParams(t)
	p.Home = home
	p.Initial = providers.Model{}
	p.Env = func(k string) (string, bool) { return "sk-a", k == "OPENAI_API_KEY" }
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	require.Greater(t, len(openAIRows(rt.models.Current().set)), 1)

	// Another process (ask auth login, ask models refresh) writes the list.
	seedAccount(t, home, "sk-a", "gpt-5.5")
	require.Equal(t, []string{"gpt-5.5"}, openAIRows(rt.models.Current().set), "the write of another process is read again")
	rows := rt.Rows()
	require.Len(t, rows, len(rt.models.Current().set.All()), "rows come from the model set")

	// A list of another key does not apply.
	seedAccount(t, home, "sk-other", "gpt-5.6-sol")
	require.Equal(t, []string{"gpt-5.5"}, openAIRows(rt.models.Current().set))
}

func TestModelRefreshConcurrentCallersShareOneFetch(t *testing.T) {
	home := privateHome(t)
	vendor := newFakeVendor("gpt-5.5")
	vendor.hold = make(chan struct{})
	clock := &testClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	rt := refreshRuntime(t, home, map[string]string{"OPENAI_API_KEY": "sk-a"}, vendor, clock, nil)
	r := rt.models
	<-vendor.started // the start path began the one fetch

	var ready, done sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		ready.Add(1)
		done.Go(func() {
			ready.Done()
			errs <- r.refreshAccount(context.Background(), providers.ProviderOpenAI, r.currentGeneration(), false)
		})
	}
	ready.Wait()
	close(vendor.hold)
	done.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	lists, maxActive := vendor.counts()
	require.Equal(t, 1, lists, "one fetch for the account key")
	require.Equal(t, 1, maxActive)
	require.Equal(t, []string{"gpt-5.5"}, openAIRows(r.Current().set))
}

func TestModelRefreshDropsResultAfterLogout(t *testing.T) {
	home := privateHome(t)
	store, err := settings.NewAuthStore(home)
	require.NoError(t, err)
	ctx := context.Background()
	_, err = store.Replace(ctx, providers.ProviderOpenAI, 0, settings.Credential{Method: "api-key", APIKey: "sk-saved"})
	require.NoError(t, err)

	vendor := newFakeVendor("gpt-5.5")
	vendor.hold = make(chan struct{})
	clock := &testClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	core, logs := observer.New(zapcore.InfoLevel)
	dropped := make(chan struct{}, 1)
	log := zap.New(zapcore.RegisterHooks(core, func(e zapcore.Entry) error {
		if strings.Contains(e.Message, "dropped") {
			dropped <- struct{}{}
		}
		return nil
	}))
	rt := refreshRuntime(t, home, nil, vendor, clock, log)
	setup := newProviderSetup(rt, func(uint64, string, protocol.ACPProviderChange) {})
	t.Cleanup(setup.Close)

	<-vendor.started // the fetch for the saved key is in flight
	_, err = setup.Logout(ctx, protocol.ACPLogoutRequest{ProviderID: providers.ProviderOpenAI, MethodID: "api-key"})
	require.NoError(t, err)
	close(vendor.hold)
	<-dropped

	cache, err := settings.NewModelCache(home)
	require.NoError(t, err)
	data, _, err := cache.Read(ctx)
	require.NoError(t, err)
	for _, entry := range data.Accounts {
		require.NotEqual(t, providers.ProviderOpenAI, entry.Provider, "the list of the old credential is not stored")
	}
	require.Greater(t, len(openAIRows(rt.models.Current().set)), 1, "access is unknown again: all rows")
	require.NotEmpty(t, logs.FilterMessageSnippet("dropped").All())
}

func TestModelRefreshStopEndsInFlightFetch(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	home := privateHome(t)
	vendor := newFakeVendor("gpt-5.5")
	vendor.hold = make(chan struct{}) // never released
	clock := &testClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	p := testACPParams(t)
	p.Home = home
	p.Env = func(k string) (string, bool) { return "sk-a", k == "OPENAI_API_KEY" }
	p.AuthHTTP = &http.Client{Transport: vendor}
	p.ModelRefresh = ModelRefresh{Enabled: true, HTTP: &http.Client{Transport: vendor}, Now: clock.Now}
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	<-vendor.started
	require.NoError(t, rt.Cleanup(context.Background()), "cleanup returns while a fetch is held")
	rt.models.RefreshIfStale() // after Stop: starts nothing
	rt.models.Schedule(providers.ProviderOpenAI)
	lists, _ := vendor.counts()
	require.Equal(t, 1, lists)
}

func TestModelRefreshFailureKeepsListAndWaitsRetryDelay(t *testing.T) {
	home := privateHome(t)
	seedAccount(t, home, "sk-a", "gpt-5.5")
	vendor := newFakeVendor("gpt-5.6-sol")
	vendor.listCode = http.StatusInternalServerError
	vendor.devCode = http.StatusInternalServerError
	clock := &testClock{now: time.Now().Add(3 * time.Hour)} // the seeded list is stale
	p := testACPParams(t)
	p.Home = home
	p.Env = func(k string) (string, bool) { return "sk-a", k == "OPENAI_API_KEY" }
	p.AuthHTTP = &http.Client{Transport: vendor}
	p.ModelRefresh = ModelRefresh{Enabled: true, HTTP: &http.Client{Transport: vendor}, Now: clock.Now}
	// Build the refresher alone, so that the start path does not race the
	// synchronous checks below.
	service, err := NewNativeAuth(p.Home, p.Env, auth.NativeOptions{HTTPClient: p.AuthHTTP})
	require.NoError(t, err)
	r, err := newModelRefresher(p.Home, service, p.ModelRefresh)
	require.NoError(t, err)
	t.Cleanup(r.Stop)
	ctx := context.Background()

	r.refreshStale(ctx)
	lists, _ := vendor.counts()
	require.Equal(t, 1, lists)
	require.Equal(t, []string{"gpt-5.5"}, openAIRows(r.Current().set), "a failed refresh keeps the last list")

	r.refreshStale(ctx)
	lists, _ = vendor.counts()
	require.Equal(t, 1, lists, "no new fetch in the retry delay")
	require.Len(t, vendor.etags, 1, "the overlay also waits")

	clock.add(modelRetryDelay + time.Second)
	vendor.mu.Lock()
	vendor.listCode = http.StatusOK
	vendor.mu.Unlock()
	r.refreshStale(ctx)
	lists, _ = vendor.counts()
	require.Equal(t, 2, lists)
	require.Equal(t, []string{"gpt-5.6-sol"}, openAIRows(r.Current().set))
}

func TestModelRefreshOverlayETagAndFailure(t *testing.T) {
	home := privateHome(t)
	vendor := newFakeVendor()
	clock := &testClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	service, err := NewNativeAuth(home, nil, auth.NativeOptions{HTTPClient: &http.Client{Transport: vendor}})
	require.NoError(t, err)
	r, err := newModelRefresher(home, service, ModelRefresh{Enabled: true, HTTP: &http.Client{Transport: vendor}, Now: clock.Now})
	require.NoError(t, err)
	t.Cleanup(r.Stop)
	ctx := context.Background()
	cache, err := settings.NewModelCache(home)
	require.NoError(t, err)

	require.NoError(t, r.refreshOverlay(ctx, true))
	clock.add(time.Minute)
	require.NoError(t, r.refreshOverlay(ctx, true))
	data, _, err := cache.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"", `"v1"`}, vendor.etags, "the stored ETag is sent for the same provider list")
	require.True(t, clock.Now().Equal(data.Overlay.FetchedAt), "a 304 renews the fetch time")
	payload := string(data.Overlay.Payload)

	// A payload stored for another provider list gets no ETag.
	require.NoError(t, cache.Update(ctx, func(d *settings.ModelCacheData) error {
		d.Overlay.Providers = []string{providers.ProviderOpenAI}
		return nil
	}))
	require.NoError(t, r.refreshOverlay(ctx, true))
	require.Equal(t, "", vendor.etags[2])

	vendor.mu.Lock()
	vendor.devCode = http.StatusBadGateway
	vendor.mu.Unlock()
	require.Error(t, r.refreshOverlay(ctx, true))
	data, _, err = cache.Read(ctx)
	require.NoError(t, err)
	require.JSONEq(t, payload, string(data.Overlay.Payload), "a failure keeps the last payload")
}

func TestModelRefreshNeedsHTTPClientWhenOn(t *testing.T) {
	p := testACPParams(t)
	p.ModelRefresh = ModelRefresh{Enabled: true}
	_, err := NewACPRuntime(p)
	require.Error(t, err)
}

func TestStartFallbackNotInAccount(t *testing.T) {
	home := privateHome(t)
	seedAccount(t, home, "sk-a", "gpt-5.6-sol")
	prefs, err := settings.NewSettingsStore(home)
	require.NoError(t, err)
	require.NoError(t, prefs.SetDefaultModel(context.Background(), "openai/gpt-5.5@openai-responses"))

	p := testACPParams(t)
	p.Home = home
	p.Initial = providers.Model{}
	p.Env = func(k string) (string, bool) { return "sk-a", k == "OPENAI_API_KEY" }
	var start providers.Model
	p.NewAgent = func(_ *auth.Service, _, _ string, m providers.Model) (*agent.Agent, error) {
		start = m
		return nil, nil
	}
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	_, err = rt.Config.Factory(context.Background(), "s1", t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", start.ID, "the recommended model is not in the list either: the first row of the provider")
	fb := rt.Config.StartFallback("s1")
	require.NotNil(t, fb)
	require.Equal(t, protocol.ACPFallbackNotInAccount, fb.Reason)

	// A model that no source has keeps the old reason.
	require.NoError(t, prefs.SetDefaultModel(context.Background(), "openai/gpt-0-none@openai-responses"))
	_, err = rt.Config.Factory(context.Background(), "s2", t.TempDir())
	require.NoError(t, err)
	require.Equal(t, protocol.ACPFallbackNotInCatalog, rt.Config.StartFallback("s2").Reason)
}

func TestAuthenticateChecksOneRowPerMethod(t *testing.T) {
	p := testACPParams(t)
	p.Initial = providers.Model{}
	p.Env = func(k string) (string, bool) { return "sk-a", k == "OPENAI_API_KEY" }
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	require.NoError(t, rt.Config.Authenticate(context.Background(), "api-key"))
	require.Error(t, rt.Config.Authenticate(context.Background(), "openai-chatgpt"))
}

func TestModelRefreshExplicitRefreshIgnoresTTL(t *testing.T) {
	home := privateHome(t)
	seedAccount(t, home, "sk-a", "gpt-5.5") // fresh: a stale check would skip it
	vendor := newFakeVendor("gpt-5.6-sol")
	clock := &testClock{now: time.Now()}
	service, err := NewNativeAuth(home, func(k string) (string, bool) { return "sk-a", k == "OPENAI_API_KEY" }, auth.NativeOptions{HTTPClient: &http.Client{Transport: vendor}})
	require.NoError(t, err)
	r, err := newModelRefresher(home, service, ModelRefresh{Enabled: true, HTTP: &http.Client{Transport: vendor}, Now: clock.Now})
	require.NoError(t, err)

	require.NoError(t, r.Refresh(context.Background(), providers.ProviderOpenAI))
	require.Equal(t, []string{"gpt-5.6-sol"}, openAIRows(r.Current().set))
	require.Len(t, vendor.etags, 1, "the overlay is fetched too")

	r.Stop()
	require.ErrorIs(t, r.Refresh(context.Background()), context.Canceled, "no refresh after Stop")
}
