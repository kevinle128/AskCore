package settings

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	modelCacheFile = "model-cache.json"
	// ModelCacheSchema is the format version that this binary reads and writes.
	ModelCacheSchema  = 1
	modelCacheSaltLen = 32
	// The overlay holds a filtered copy of a models.dev body whose raw size is
	// capped at 32 MB, so the file cap stays well above that. A larger file is
	// treated as corrupt, never as an error that blocks the cache.
	maxModelCacheFileLen = 64 << 20
	maxAccountKeyLen     = 512
	maxAccountMethodLen  = 64
)

// ErrModelCacheNewerSchema means that a newer binary wrote the cache. This
// binary does not know that format, so it never overwrites the file.
var ErrModelCacheNewerSchema = errors.New("model cache has a newer schema")

// ModelCacheStatus says what Read found on disk.
type ModelCacheStatus int

const (
	// ModelCacheMissing means that no cache file exists.
	ModelCacheMissing ModelCacheStatus = iota
	// ModelCacheValid means that the file was read and the data is returned.
	ModelCacheValid
	// ModelCacheCorrupt means that the file is not a valid cache. The next
	// write replaces it.
	ModelCacheCorrupt
	// ModelCacheOlderSchema means that an older binary wrote the file. The
	// next write discards it.
	ModelCacheOlderSchema
	// ModelCacheNewerSchema means that a newer binary wrote the file. It is
	// read-only for this binary.
	ModelCacheNewerSchema
)

// AccountModelsState says whether the model list of an account is known.
type AccountModelsState string

const (
	// AccountModelsKnown means that the provider returned a list for the account.
	AccountModelsKnown AccountModelsState = "known"
	// AccountModelsUnknown means that Ask could not get a list for the account.
	AccountModelsUnknown AccountModelsState = "unknown"
)

// ModelCacheData is the content of model-cache.json. It holds public model
// metadata and plain model ids only. An account key never holds a token, a
// key, or a key-derived value other than KeyedHash with Salt.
type ModelCacheData struct {
	Schema int `json:"schema"`
	// Salt is made once for each home. An Update callback cannot change it,
	// because a new salt would orphan every entry keyed by KeyedHash.
	Salt     []byte                   `json:"salt"`
	Overlay  *ModelOverlay            `json:"overlay,omitempty"`
	Accounts map[string]CachedAccount `json:"accounts"`
}

// ModelOverlay is the last models.dev metadata that Ask fetched.
type ModelOverlay struct {
	ETag string `json:"etag"`
	// Providers is the provider filter that produced Payload. An ETag is only
	// valid for the same filter.
	Providers []string        `json:"providers"`
	FetchedAt time.Time       `json:"fetchedAt"`
	Payload   json.RawMessage `json:"payload"`
}

// CachedAccount is the model list of one account, under a key that the
// caller builds.
type CachedAccount struct {
	Provider   string             `json:"provider"`
	Method     string             `json:"method"`
	FetchedAt  time.Time          `json:"fetchedAt"`
	RetryAfter time.Time          `json:"retryAfter,omitzero"`
	State      AccountModelsState `json:"state"`
	Items      []CachedModel      `json:"items,omitempty"`
}

// CachedModel is one model id of an account list.
type CachedModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
}

// ModelCache reads and writes <home>/model-cache.json. It follows the file
// rules of AuthStore: a lock sidecar, a 0600 file in a 0700 home, a fresh read
// under the lock, and an atomic replace with a directory sync.
type ModelCache struct {
	// files supplies the home path and the atomic commit; it never touches auth.json here.
	files *AuthStore
}

// NewModelCache constructs a cache store. Empty home uses ASK_HOME or ~/.ask.
func NewModelCache(home string) (*ModelCache, error) {
	files, err := NewAuthStore(home)
	if err != nil {
		return nil, err
	}
	return &ModelCache{files: files}, nil
}

// Path returns the path of the cache file. A reader can compare its
// modification time and size to see a write of another process.
func (c *ModelCache) Path() string { return filepath.Join(c.files.home, modelCacheFile) }

// KeyedHash returns a stable identity for a secret value, such as an API key:
// the hex of the first 16 bytes of HMAC-SHA256 with the cache salt as the key.
// The random salt of each home prevents a match against a plain hash of a
// known key. Use the Salt of ModelCacheData that Update supplies.
//
// A salt that is not 32 bytes gives an empty string. Read returns no salt for
// a missing file, and an HMAC with an empty key is the same value in every
// home, so the function fails closed instead of returning a linkable value.
func KeyedHash(salt []byte, value string) string {
	if len(salt) != modelCacheSaltLen {
		return ""
	}
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// Read returns the cache data without the lock: the atomic replace makes each
// read a whole-file snapshot. A missing, corrupt, older or newer file returns
// empty data and its status, not an error. Read never creates the home. A file
// or home with an unsafe owner, type or mode returns ErrUnsafeStore.
func (c *ModelCache) Read(ctx context.Context) (ModelCacheData, ModelCacheStatus, error) {
	if err := ctx.Err(); err != nil {
		return emptyModelCache(), ModelCacheMissing, err
	}
	exists, err := CheckPrivateDir(c.files.home)
	if err != nil {
		return emptyModelCache(), ModelCacheMissing, err
	}
	if !exists {
		return emptyModelCache(), ModelCacheMissing, nil
	}
	return readModelCache(filepath.Join(c.files.home, modelCacheFile))
}

// Update runs fn on a copy of the fresh data under the lock and writes the
// result. fn receives a non-nil Accounts map and a valid Salt. When fn returns
// an error, nothing is written.
//
// fn can use the result of a fetch that started before another writer stored
// a newer result. So, for each account entry and for the overlay, the stored
// value stays when its FetchedAt is after the FetchedAt that fn set. An entry
// that fn deletes is deleted.
//
// A corrupt file or a file of an older schema is replaced by new data with a
// new salt. A file of a newer schema returns ErrModelCacheNewerSchema, and fn
// does not run.
func (c *ModelCache) Update(ctx context.Context, fn func(*ModelCacheData) error) error {
	if fn == nil {
		return errors.New("model cache update: nil function")
	}
	home := c.files.home
	if err := ensureHomeShared(home); err != nil {
		return err
	}
	unlock, err := lockSidecar(ctx, filepath.Join(home, modelCacheFile+".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(home, modelCacheFile)
	base, status, err := readModelCache(path)
	if err != nil {
		return err
	}
	switch status {
	case ModelCacheNewerSchema:
		return ErrModelCacheNewerSchema
	case ModelCacheValid:
	default:
		salt := make([]byte, modelCacheSaltLen)
		if _, err = rand.Read(salt); err != nil {
			return err
		}
		base = ModelCacheData{Schema: ModelCacheSchema, Salt: salt, Accounts: map[string]CachedAccount{}}
	}
	next := base.clone()
	if err = fn(&next); err != nil {
		return err
	}
	merged := mergeModelCache(base, next)
	if err = merged.validate(); err != nil {
		return fmt.Errorf("model cache update: %w", err)
	}
	b, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if len(b) > maxModelCacheFileLen {
		// A file over the cap reads as corrupt, which would discard all data.
		return errors.New("model cache update: data too large")
	}
	if status == ModelCacheValid {
		// Skip a write that changes nothing, so that the modification time of
		// the file changes only when the data changes.
		old, err := json.Marshal(base)
		if err == nil && bytes.Equal(old, b) {
			return nil
		}
	}
	return c.files.commit(ctx, path, b)
}

// DeleteProvider removes every account entry of provider. It keeps the
// overlay, because the overlay is public metadata of all providers.
func (c *ModelCache) DeleteProvider(ctx context.Context, provider string) error {
	if !validProvider(provider) {
		return fmt.Errorf("%w: provider", ErrUnsafeStore)
	}
	return c.Update(ctx, func(d *ModelCacheData) error {
		for key, entry := range d.Accounts {
			if entry.Provider == provider {
				delete(d.Accounts, key)
			}
		}
		return nil
	})
}

func emptyModelCache() ModelCacheData {
	return ModelCacheData{Accounts: map[string]CachedAccount{}}
}

// readModelCache reads and classifies the file. Only an unsafe or unreadable
// file is an error; bad content is a status, because the cache can always be
// fetched again.
func readModelCache(path string) (ModelCacheData, ModelCacheStatus, error) {
	if err := checkEntry(path, 0600); err != nil {
		return emptyModelCache(), ModelCacheMissing, err
	}
	f, err := openNoFollow(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return emptyModelCache(), ModelCacheMissing, nil
	}
	if err != nil {
		return emptyModelCache(), ModelCacheMissing, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return emptyModelCache(), ModelCacheMissing, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owner(info) {
		return emptyModelCache(), ModelCacheMissing, fmt.Errorf("%w: model cache file mode", ErrUnsafeStore)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxModelCacheFileLen+1))
	if err != nil {
		return emptyModelCache(), ModelCacheMissing, err
	}
	if len(b) > maxModelCacheFileLen {
		return emptyModelCache(), ModelCacheCorrupt, nil
	}
	// Read the schema first: the rest of the format depends on it.
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil || fields == nil {
		return emptyModelCache(), ModelCacheCorrupt, nil
	}
	schema := 0
	if raw, ok := fields["schema"]; ok && json.Unmarshal(raw, &schema) != nil {
		return emptyModelCache(), ModelCacheCorrupt, nil
	}
	switch {
	case schema > ModelCacheSchema:
		return emptyModelCache(), ModelCacheNewerSchema, nil
	case schema < ModelCacheSchema:
		return emptyModelCache(), ModelCacheOlderSchema, nil
	}
	var d ModelCacheData
	if json.Unmarshal(b, &d) != nil {
		return emptyModelCache(), ModelCacheCorrupt, nil
	}
	if d.Accounts == nil {
		d.Accounts = map[string]CachedAccount{}
	}
	if d.validate() != nil {
		return emptyModelCache(), ModelCacheCorrupt, nil
	}
	return d, ModelCacheValid, nil
}

// clone makes a deep copy, so that an Update callback cannot change the stored
// data that the merge compares against.
func (d ModelCacheData) clone() ModelCacheData {
	out := d
	out.Salt = slices.Clone(d.Salt)
	if d.Overlay != nil {
		o := *d.Overlay
		o.Providers = slices.Clone(o.Providers)
		o.Payload = slices.Clone(o.Payload)
		out.Overlay = &o
	}
	out.Accounts = make(map[string]CachedAccount, len(d.Accounts))
	for key, entry := range d.Accounts {
		entry.Items = slices.Clone(entry.Items)
		out.Accounts[key] = entry
	}
	return out
}

// mergeModelCache applies the newer-fetchedAt rule. An equal FetchedAt keeps
// the edit, so that a change of RetryAfter alone is not lost.
func mergeModelCache(base, next ModelCacheData) ModelCacheData {
	next.Schema = ModelCacheSchema
	next.Salt = base.Salt
	if next.Accounts == nil {
		next.Accounts = map[string]CachedAccount{}
	}
	for key, stored := range base.Accounts {
		if edited, ok := next.Accounts[key]; ok && stored.FetchedAt.After(edited.FetchedAt) {
			next.Accounts[key] = stored
		}
	}
	if base.Overlay != nil && next.Overlay != nil && base.Overlay.FetchedAt.After(next.Overlay.FetchedAt) {
		next.Overlay = base.Overlay
	}
	return next
}

func (d ModelCacheData) validate() error {
	if len(d.Salt) != modelCacheSaltLen {
		return errors.New("salt length")
	}
	if d.Overlay != nil && len(d.Overlay.Payload) > 0 && !json.Valid(d.Overlay.Payload) {
		return errors.New("overlay payload is not JSON")
	}
	for key, entry := range d.Accounts {
		if key == "" || len(key) > maxAccountKeyLen {
			return errors.New("account key")
		}
		if !validProvider(entry.Provider) {
			return errors.New("account provider")
		}
		if entry.Method == "" || len(entry.Method) > maxAccountMethodLen {
			return errors.New("account method")
		}
		if entry.State != AccountModelsKnown && entry.State != AccountModelsUnknown {
			return errors.New("account state")
		}
		for _, item := range entry.Items {
			if item.ID == "" {
				return errors.New("model id")
			}
		}
	}
	return nil
}
