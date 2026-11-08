package settings

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func newTestModelCache(t *testing.T) (*ModelCache, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	c, err := NewModelCache(home)
	if err != nil {
		t.Fatal(err)
	}
	return c, home
}

func knownEntry(provider, method string, at time.Time, ids ...string) CachedAccount {
	items := make([]CachedModel, 0, len(ids))
	for _, id := range ids {
		items = append(items, CachedModel{ID: id, DisplayName: "Name " + id})
	}
	return CachedAccount{Provider: provider, Method: method, FetchedAt: at, State: AccountModelsKnown, Items: items}
}

func putEntry(ctx context.Context, c *ModelCache, key string, e CachedAccount) error {
	return c.Update(ctx, func(d *ModelCacheData) error {
		d.Accounts[key] = e
		return nil
	})
}

func TestModelCacheMissingReadsEmptyAndCreatesNothing(t *testing.T) {
	c, home := newTestModelCache(t)
	d, status, err := c.Read(context.Background())
	if err != nil || status != ModelCacheMissing {
		t.Fatalf("read of a missing file: %v %v", status, err)
	}
	if len(d.Accounts) != 0 || d.Overlay != nil || len(d.Salt) != 0 {
		t.Fatalf("missing file data: %+v", d)
	}
	if _, err := os.Lstat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read must not create the home: %v", err)
	}
}

func TestModelCacheRoundTrip(t *testing.T) {
	c, _ := newTestModelCache(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 28, 12, 0, 0, 123456789, time.UTC)
	retry := at.Add(time.Hour)
	entry := knownEntry("openai", "api-key", at, "gpt-5.5", "gpt-5.5-mini")
	entry.RetryAfter = retry
	unknown := CachedAccount{Provider: "anthropic", Method: "anthropic-oauth", FetchedAt: at, State: AccountModelsUnknown}
	payload := json.RawMessage(`{"openai":{"models":{"gpt-5.5":{"name":"GPT-5.5"}}}}`)
	err := c.Update(ctx, func(d *ModelCacheData) error {
		d.Overlay = &ModelOverlay{ETag: `"abc"`, Providers: []string{"anthropic", "openai"}, FetchedAt: at, Payload: payload}
		d.Accounts["openai/api-key/k1"] = entry
		d.Accounts["anthropic/anthropic-oauth/sub"] = unknown
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	d, status, err := c.Read(ctx)
	if err != nil || status != ModelCacheValid {
		t.Fatalf("read: %v %v", status, err)
	}
	if d.Schema != ModelCacheSchema || len(d.Salt) != modelCacheSaltLen {
		t.Fatalf("schema %d salt %d", d.Schema, len(d.Salt))
	}
	if d.Overlay == nil || d.Overlay.ETag != `"abc"` || !d.Overlay.FetchedAt.Equal(at) || len(d.Overlay.Providers) != 2 {
		t.Fatalf("overlay: %+v", d.Overlay)
	}
	var want, got any
	if json.Unmarshal(payload, &want) != nil || json.Unmarshal(d.Overlay.Payload, &got) != nil || fmt.Sprint(want) != fmt.Sprint(got) {
		t.Fatalf("payload: %s", d.Overlay.Payload)
	}
	e := d.Accounts["openai/api-key/k1"]
	if e.Provider != "openai" || e.Method != "api-key" || e.State != AccountModelsKnown || !e.FetchedAt.Equal(at) || !e.RetryAfter.Equal(retry) {
		t.Fatalf("entry: %+v", e)
	}
	if len(e.Items) != 2 || e.Items[0].ID != "gpt-5.5" || e.Items[1].DisplayName != "Name gpt-5.5-mini" {
		t.Fatalf("items: %+v", e.Items)
	}
	if u := d.Accounts["anthropic/anthropic-oauth/sub"]; u.State != AccountModelsUnknown || len(u.Items) != 0 {
		t.Fatalf("unknown entry: %+v", u)
	}
	// The salt is made once: a second write keeps it.
	first := d.Salt
	if err = putEntry(ctx, c, "xai/api-key/k2", knownEntry("xai", "api-key", at, "grok-5")); err != nil {
		t.Fatal(err)
	}
	d, _, err = c.Read(ctx)
	if err != nil || !bytes.Equal(d.Salt, first) {
		t.Fatalf("salt changed: %v", err)
	}
}

func TestModelCacheFileModes(t *testing.T) {
	c, home := newTestModelCache(t)
	if err := putEntry(context.Background(), c, "k", knownEntry("openai", "api-key", time.Now(), "a")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"model-cache.json", "model-cache.json.lock"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode %v", name, info.Mode().Perm())
		}
	}
	info, err := os.Stat(home)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("home mode: %v %v", info, err)
	}
}

func TestModelCacheRefusesUnsafeFile(t *testing.T) {
	c, home := newTestModelCache(t)
	ctx := context.Background()
	if err := putEntry(ctx, c, "k", knownEntry("openai", "api-key", time.Now(), "a")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "model-cache.json")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Read(ctx); !errors.Is(err, ErrUnsafeStore) {
		t.Fatalf("read of a 0644 file: %v", err)
	}
	if err := putEntry(ctx, c, "k2", knownEntry("openai", "api-key", time.Now(), "b")); !errors.Is(err, ErrUnsafeStore) {
		t.Fatalf("update of a 0644 file: %v", err)
	}
}

func TestModelCacheConcurrentGoroutinesKeepEveryChange(t *testing.T) {
	_, home := newTestModelCache(t)
	ctx := context.Background()
	const writers, each = 8, 5
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// One store for each writer: the file lock, not shared memory, must serialize them.
			c, err := NewModelCache(home)
			if err != nil {
				errs <- err
				return
			}
			for i := 0; i < each; i++ {
				key := fmt.Sprintf("openai/api-key/w%d-%d", w, i)
				if err := putEntry(ctx, c, key, knownEntry("openai", "api-key", time.Now(), "m")); err != nil {
					errs <- err
					return
				}
			}
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent update: %v", err)
		}
	}
	c, err := NewModelCache(home)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := c.Read(ctx)
	if err != nil || len(d.Accounts) != writers*each {
		t.Fatalf("entries after concurrent updates: %d %v", len(d.Accounts), err)
	}
}

func TestModelCacheProcessHelper(t *testing.T) {
	home := os.Getenv("ASK_MODEL_CACHE_PROCESS_HOME")
	if home == "" {
		return
	}
	prefix := os.Getenv("ASK_MODEL_CACHE_PROCESS_PREFIX")
	count, err := strconv.Atoi(os.Getenv("ASK_MODEL_CACHE_PROCESS_COUNT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	c, err := NewModelCache(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("xai/api-key/%s-%d", prefix, i)
		if err := putEntry(context.Background(), c, key, knownEntry("xai", "api-key", time.Now(), "grok")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func TestModelCacheTwoProcessesKeepEveryChange(t *testing.T) {
	_, home := newTestModelCache(t)
	const each = 15
	child := func(prefix string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestModelCacheProcessHelper$")
		cmd.Env = append(os.Environ(),
			"ASK_MODEL_CACHE_PROCESS_HOME="+home,
			"ASK_MODEL_CACHE_PROCESS_PREFIX="+prefix,
			"ASK_MODEL_CACHE_PROCESS_COUNT="+strconv.Itoa(each))
		return cmd
	}
	first, second := child("a"), child("b")
	var firstOut, secondOut bytes.Buffer
	first.Stderr, second.Stderr = &firstOut, &secondOut
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err != nil {
		_ = first.Process.Kill()
		_ = first.Wait()
		t.Fatal(err)
	}
	firstErr, secondErr := first.Wait(), second.Wait()
	if firstErr != nil || secondErr != nil {
		t.Fatalf("children: %v %q; %v %q", firstErr, firstOut.String(), secondErr, secondOut.String())
	}
	c, err := NewModelCache(home)
	if err != nil {
		t.Fatal(err)
	}
	d, status, err := c.Read(context.Background())
	if err != nil || status != ModelCacheValid {
		t.Fatalf("read: %v %v", status, err)
	}
	for _, prefix := range []string{"a", "b"} {
		for i := 0; i < each; i++ {
			if _, ok := d.Accounts[fmt.Sprintf("xai/api-key/%s-%d", prefix, i)]; !ok {
				t.Fatalf("lost entry %s-%d of %d entries", prefix, i, len(d.Accounts))
			}
		}
	}
}

func TestModelCacheNewerFetchedAtWinsPerEntry(t *testing.T) {
	c, _ := newTestModelCache(t)
	ctx := context.Background()
	older := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	// Another writer stored the result of a fetch that began later.
	err := c.Update(ctx, func(d *ModelCacheData) error {
		d.Accounts["shared"] = knownEntry("openai", "api-key", newer, "new-model")
		d.Overlay = &ModelOverlay{ETag: "new", Providers: []string{"openai"}, FetchedAt: newer, Payload: json.RawMessage(`{}`)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A slow fetch that began earlier commits last. Its own new entry stays;
	// the entries with a newer stored fetchedAt stay as they are.
	err = c.Update(ctx, func(d *ModelCacheData) error {
		d.Accounts["shared"] = knownEntry("openai", "api-key", older, "old-model")
		d.Accounts["other"] = knownEntry("xai", "api-key", older, "grok")
		d.Overlay = &ModelOverlay{ETag: "old", Providers: []string{"openai"}, FetchedAt: older, Payload: json.RawMessage(`{}`)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Accounts["shared"]; !got.FetchedAt.Equal(newer) || got.Items[0].ID != "new-model" {
		t.Fatalf("older fetch replaced a newer entry: %+v", got)
	}
	if _, ok := d.Accounts["other"]; !ok {
		t.Fatal("new entry of the slow writer lost")
	}
	if d.Overlay == nil || d.Overlay.ETag != "new" {
		t.Fatalf("older overlay replaced a newer one: %+v", d.Overlay)
	}
	// An edit with the same fetchedAt (a new retryAfter) is kept.
	retry := newer.Add(time.Hour)
	err = c.Update(ctx, func(d *ModelCacheData) error {
		e := d.Accounts["shared"]
		e.RetryAfter = retry
		d.Accounts["shared"] = e
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _, err = c.Read(ctx)
	if err != nil || !d.Accounts["shared"].RetryAfter.Equal(retry) {
		t.Fatalf("same fetchedAt edit lost: %+v %v", d.Accounts["shared"], err)
	}
}

func TestModelCacheCallbackCannotChangeSaltOrSchema(t *testing.T) {
	c, _ := newTestModelCache(t)
	ctx := context.Background()
	if err := putEntry(ctx, c, "k", knownEntry("openai", "api-key", time.Now(), "a")); err != nil {
		t.Fatal(err)
	}
	before, _, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Update(ctx, func(d *ModelCacheData) error {
		d.Salt[0] ^= 0xff
		d.Salt = append(d.Salt, 1)
		d.Schema = 99
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	after, status, err := c.Read(ctx)
	if err != nil || status != ModelCacheValid || !bytes.Equal(before.Salt, after.Salt) || after.Schema != ModelCacheSchema {
		t.Fatalf("salt or schema changed: %v %v", status, err)
	}
}

func TestModelCacheCallbackErrorWritesNothing(t *testing.T) {
	c, home := newTestModelCache(t)
	ctx := context.Background()
	if err := putEntry(ctx, c, "k", knownEntry("openai", "api-key", time.Now(), "a")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "model-cache.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err = c.Update(ctx, func(d *ModelCacheData) error {
		delete(d.Accounts, "k")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("callback error: %v", err)
	}
	err = putEntry(ctx, c, "bad", CachedAccount{Provider: "openai", Method: "api-key", State: "denied"})
	if err == nil {
		t.Fatal("invalid state must be refused")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("file changed after a refused update: %v", err)
	}
}

func TestModelCacheCorruptFileReadsEmptyAndIsReplaced(t *testing.T) {
	cases := map[string]string{
		"not json":   "{not json",
		"bad salt":   fmt.Sprintf(`{"schema":%d,"salt":"AAEC","accounts":{}}`, ModelCacheSchema),
		"bad state":  fmt.Sprintf(`{"schema":%d,"salt":%q,"accounts":{"k":{"provider":"openai","method":"api-key","state":"denied"}}}`, ModelCacheSchema, testSaltB64()),
		"json array": "[]",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			c, home := newTestModelCache(t)
			ctx := context.Background()
			if err := ensureHome(home); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, "model-cache.json")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			d, status, err := c.Read(ctx)
			if err != nil || status != ModelCacheCorrupt || len(d.Accounts) != 0 || d.Overlay != nil {
				t.Fatalf("corrupt read: %v %v %+v", status, err, d)
			}
			if err = putEntry(ctx, c, "k", knownEntry("openai", "api-key", time.Now(), "a")); err != nil {
				t.Fatalf("write over a corrupt file: %v", err)
			}
			d, status, err = c.Read(ctx)
			if err != nil || status != ModelCacheValid || len(d.Accounts) != 1 || len(d.Salt) != modelCacheSaltLen {
				t.Fatalf("after replace: %v %v %d", status, err, len(d.Accounts))
			}
		})
	}
}

func testSaltB64() string {
	b, _ := json.Marshal(bytes.Repeat([]byte{7}, modelCacheSaltLen))
	var s string
	_ = json.Unmarshal(b, &s)
	return s
}

func TestModelCacheNewerSchemaIsReadOnly(t *testing.T) {
	c, home := newTestModelCache(t)
	ctx := context.Background()
	if err := ensureHome(home); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "model-cache.json")
	content := []byte(fmt.Sprintf(`{"schema":%d,"future":true}`, ModelCacheSchema+1))
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	d, status, err := c.Read(ctx)
	if err != nil || status != ModelCacheNewerSchema || len(d.Accounts) != 0 {
		t.Fatalf("newer schema read: %v %v", status, err)
	}
	called := false
	err = c.Update(ctx, func(d *ModelCacheData) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrModelCacheNewerSchema) || called {
		t.Fatalf("update of a newer schema: %v called=%v", err, called)
	}
	if err = c.DeleteProvider(ctx, "openai"); !errors.Is(err, ErrModelCacheNewerSchema) {
		t.Fatalf("delete of a newer schema: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(content, after) {
		t.Fatalf("newer schema file changed: %v", err)
	}
}

func TestModelCacheOlderSchemaIsDiscardedOnWrite(t *testing.T) {
	for name, content := range map[string]string{
		"no schema":    `{"accounts":{"old":{"provider":"openai","method":"api-key","state":"known"}}}`,
		"older schema": fmt.Sprintf(`{"schema":%d,"accounts":{"old":{}}}`, ModelCacheSchema-1),
	} {
		t.Run(name, func(t *testing.T) {
			c, home := newTestModelCache(t)
			ctx := context.Background()
			if err := ensureHome(home); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "model-cache.json"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			d, status, err := c.Read(ctx)
			if err != nil || status != ModelCacheOlderSchema || len(d.Accounts) != 0 {
				t.Fatalf("older schema read: %v %v %+v", status, err, d)
			}
			err = c.Update(ctx, func(d *ModelCacheData) error {
				if len(d.Accounts) != 0 {
					return errors.New("older schema data reached the callback")
				}
				d.Accounts["new"] = knownEntry("openai", "api-key", time.Now(), "a")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			d, status, err = c.Read(ctx)
			if err != nil || status != ModelCacheValid || d.Schema != ModelCacheSchema || len(d.Accounts) != 1 {
				t.Fatalf("after write: %v %v %+v", status, err, d.Accounts)
			}
			if _, ok := d.Accounts["old"]; ok {
				t.Fatal("older schema entry kept")
			}
		})
	}
}

func TestModelCacheDeleteProvider(t *testing.T) {
	c, _ := newTestModelCache(t)
	ctx := context.Background()
	at := time.Now()
	err := c.Update(ctx, func(d *ModelCacheData) error {
		d.Accounts["openai/api-key/a"] = knownEntry("openai", "api-key", at, "a")
		d.Accounts["openai/openai-chatgpt/s"] = knownEntry("openai", "openai-chatgpt", at, "b")
		d.Accounts["xai/api-key/c"] = knownEntry("xai", "api-key", at, "c")
		d.Overlay = &ModelOverlay{ETag: "e", Providers: []string{"openai", "xai"}, FetchedAt: at, Payload: json.RawMessage(`{}`)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.DeleteProvider(ctx, "openai"); err != nil {
		t.Fatal(err)
	}
	d, _, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Accounts) != 1 || d.Accounts["xai/api-key/c"].Provider != "xai" {
		t.Fatalf("after delete: %+v", d.Accounts)
	}
	if d.Overlay == nil || d.Overlay.ETag != "e" {
		t.Fatalf("delete of a provider changed the shared overlay: %+v", d.Overlay)
	}
	if err = c.DeleteProvider(ctx, "../x"); !errors.Is(err, ErrUnsafeStore) {
		t.Fatalf("invalid provider: %v", err)
	}
}

func TestKeyedHash(t *testing.T) {
	salt := bytes.Repeat([]byte{1}, modelCacheSaltLen)
	other := bytes.Repeat([]byte{2}, modelCacheSaltLen)
	h := KeyedHash(salt, "sk-test")
	if len(h) != 32 {
		t.Fatalf("hash length %d", len(h))
	}
	if _, err := hex.DecodeString(h); err != nil {
		t.Fatal(err)
	}
	if KeyedHash(salt, "sk-test") != h {
		t.Fatal("hash is not stable")
	}
	if KeyedHash(other, "sk-test") == h || KeyedHash(salt, "sk-other") == h {
		t.Fatal("hash ignores the salt or the value")
	}
	for _, bad := range [][]byte{nil, {}, salt[:16]} {
		if got := KeyedHash(bad, "sk-test"); got != "" {
			t.Fatalf("salt of %d bytes gave %q", len(bad), got)
		}
	}
}

func TestModelCacheUpdateWithoutChangeKeepsFile(t *testing.T) {
	c, home := newTestModelCache(t)
	ctx := context.Background()
	if err := putEntry(ctx, c, "k", knownEntry("openai", "api-key", time.Now(), "a")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "model-cache.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Update(ctx, func(*ModelCacheData) error { return nil }); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A replace makes a new inode, so the same file means that no write happened.
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("an update without a change wrote the file")
	}
}

func TestModelCacheHoldsNoRawOrPlainHashedSecret(t *testing.T) {
	c, home := newTestModelCache(t)
	ctx := context.Background()
	secrets := []string{
		"sk-proj-FAKEtestKEY0123456789abcdefABCDEF",
		"sk-ant-api03-FAKE-anthropic-test-key-000000",
		"xai-FAKEtestkeyvalue9876543210",
	}
	err := c.Update(ctx, func(d *ModelCacheData) error {
		for i, secret := range secrets {
			key := "openai/api-key/" + KeyedHash(d.Salt, secret)
			d.Accounts[key] = knownEntry("openai", "api-key", time.Now(), fmt.Sprintf("model-%d", i))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "model-cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		sum := sha256.Sum256([]byte(secret))
		plain := hex.EncodeToString(sum[:])
		for _, needle := range []string{secret, plain, plain[:32]} {
			if bytes.Contains(raw, []byte(needle)) {
				t.Fatalf("cache file holds a credential-derived value %q", needle)
			}
		}
	}
	d, _, err := c.Read(ctx)
	if err != nil || len(d.Accounts) != len(secrets) {
		t.Fatalf("entries: %d %v", len(d.Accounts), err)
	}
}
