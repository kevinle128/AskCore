package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrConflict       = errors.New("credential revision conflict")
	ErrPendingRefresh = errors.New("credential refresh pending; sign in again")
	ErrUnsafeStore    = errors.New("unsafe credential store")
	ErrIndeterminate  = errors.New("credential commit indeterminate")
)

// OAuthCredential keeps one provider's token generation together.
type OAuthCredential struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken"`
	IDToken          string    `json:"idToken,omitempty"`
	ExpiresAt        time.Time `json:"expiresAt"`
	Scopes           []string  `json:"scopes,omitempty"`
	RefreshNotBefore time.Time `json:"refreshNotBefore,omitempty"`
	ClientID         string    `json:"clientId,omitempty"`
	Subject          string    `json:"subject,omitempty"`
	Issuer           string    `json:"issuer,omitempty"`
	SignInHint       string    `json:"signInHint,omitempty"`
}

// RefreshState fences a rotating grant before the token request starts.
type RefreshState struct {
	Status     string `json:"status"`
	Generation uint64 `json:"generation"`
	AttemptID  string `json:"attemptId"`
}

// Credential is one tagged provider credential.
type Credential struct {
	Method       string           `json:"method"`
	APIKey       string           `json:"apiKey,omitempty"`
	OAuth        *OAuthCredential `json:"oauth,omitempty"`
	Generation   uint64           `json:"generation,omitempty"`
	RefreshState *RefreshState    `json:"refreshState,omitempty"`
}

// AuthStore manages one owner-only auth.json in home.
type AuthStore struct {
	home string
	ops  fileOps
}

type fileOps struct {
	write     func(*os.File, []byte) (int, error)
	syncFile  func(*os.File) error
	closeFile func(*os.File) error
	rename    func(string, string) error
	syncDir   func(*os.File) error
}

// NewAuthStore constructs a local credential store. Empty home uses ASK_HOME or ~/.ask.
func NewAuthStore(home string) (*AuthStore, error) {
	if home == "" {
		home = os.Getenv("ASK_HOME")
	}
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(dir, ".ask")
	}
	if !filepath.IsAbs(home) {
		return nil, fmt.Errorf("%w: home must be absolute", ErrUnsafeStore)
	}
	return &AuthStore{home: home, ops: fileOps{write: func(f *os.File, b []byte) (int, error) { return f.Write(b) }, syncFile: (*os.File).Sync, closeFile: (*os.File).Close, rename: os.Rename, syncDir: (*os.File).Sync}}, nil
}

type authDocument struct {
	fields    map[string]json.RawMessage
	providers map[string]json.RawMessage
	revision  uint64
}

func validProvider(p string) bool {
	if p == "" || p == "." || p == ".." {
		return false
	}
	for _, r := range p {
		if r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func decodeCredential(raw json.RawMessage) (Credential, error) {
	var c Credential
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if c.Method == "" {
		var old struct {
			Type    string `json:"type"`
			Key     string `json:"key"`
			Access  string `json:"access"`
			Refresh string `json:"refresh"`
		}
		if err := json.Unmarshal(raw, &old); err != nil {
			return c, err
		}
		if old.Type == "api_key" || old.Type == "api-key" || (old.Type == "" && old.Key != "" && old.Access == "" && old.Refresh == "") {
			c.Method = "api-key"
			c.APIKey = old.Key
		}
	}
	if c.Method == "" {
		return c, fmt.Errorf("%w: credential method missing", ErrUnsafeStore)
	}
	if c.Method == "api-key" {
		if c.APIKey == "" || c.OAuth != nil {
			return c, fmt.Errorf("%w: invalid API key record", ErrUnsafeStore)
		}
	} else if c.OAuth == nil || c.APIKey != "" {
		return c, fmt.Errorf("%w: invalid OAuth record", ErrUnsafeStore)
	}
	if c.RefreshState != nil && (c.RefreshState.Status != "pending" || c.RefreshState.AttemptID == "" || c.RefreshState.Generation != c.Generation) {
		return c, fmt.Errorf("%w: invalid refresh fence", ErrUnsafeStore)
	}
	return c, nil
}

func readDocument(path string) (authDocument, error) {
	d := authDocument{fields: map[string]json.RawMessage{}, providers: map[string]json.RawMessage{}}
	f, err := openNoFollow(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return d, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owner(info) {
		return d, fmt.Errorf("%w: credential file mode", ErrUnsafeStore)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal(b, &d.fields); err != nil || d.fields == nil {
		return d, fmt.Errorf("%w: invalid JSON", ErrUnsafeStore)
	}
	if raw := d.fields["schemaVersion"]; len(raw) > 0 {
		var v int
		if json.Unmarshal(raw, &v) != nil || v != 1 {
			return d, fmt.Errorf("%w: unsupported schema", ErrUnsafeStore)
		}
	}
	if raw := d.fields["revision"]; len(raw) > 0 {
		if json.Unmarshal(raw, &d.revision) != nil {
			return d, fmt.Errorf("%w: invalid revision", ErrUnsafeStore)
		}
	}
	if raw := d.fields["providers"]; len(raw) > 0 {
		if json.Unmarshal(raw, &d.providers) != nil || d.providers == nil {
			return d, fmt.Errorf("%w: invalid providers", ErrUnsafeStore)
		}
	}
	// Pi-style files keep provider records at the root. Move them into the
	// envelope in memory so each later write retains unrelated records.
	for key, raw := range d.fields {
		if validProvider(key) && key != "revision" && key != "schemaVersion" && key != "providers" {
			var record map[string]json.RawMessage
			if json.Unmarshal(raw, &record) == nil && (record["type"] != nil || record["method"] != nil) {
				if _, exists := d.providers[key]; exists {
					return d, fmt.Errorf("%w: duplicate provider", ErrUnsafeStore)
				}
				d.providers[key] = raw
				delete(d.fields, key)
			}
		}
	}
	return d, nil
}

func (s *AuthStore) transaction(ctx context.Context, provider string, update func(*authDocument) error) (uint64, error) {
	if !validProvider(provider) {
		return 0, fmt.Errorf("%w: provider", ErrUnsafeStore)
	}
	if err := ensureHome(s.home); err != nil {
		return 0, err
	}
	unlock, err := lockSidecar(ctx, filepath.Join(s.home, "auth.json.lock"))
	if err != nil {
		return 0, err
	}
	defer unlock()
	path := filepath.Join(s.home, "auth.json")
	if err := checkEntry(path, 0600); err != nil {
		return 0, err
	}
	d, err := readDocument(path)
	if err != nil {
		return 0, err
	}
	if err = update(&d); err != nil {
		return d.revision, err
	}
	return s.writeDocument(ctx, path, &d)
}

func (s *AuthStore) writeDocument(ctx context.Context, path string, d *authDocument) (uint64, error) {
	d.revision++
	d.fields["schemaVersion"] = json.RawMessage("1")
	d.fields["revision"], _ = json.Marshal(d.revision)
	providers, err := json.Marshal(d.providers)
	if err != nil {
		return 0, err
	}
	d.fields["providers"] = providers
	b, err := json.Marshal(d.fields)
	if err != nil {
		return 0, err
	}
	if err = s.commit(ctx, path, b); err != nil {
		return 0, err
	}
	return d.revision, nil
}

func (s *AuthStore) commit(ctx context.Context, path string, b []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.home, ".auth-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if err = ctx.Err(); err != nil {
		_ = f.Close()
		return err
	}
	n, err := s.ops.write(f, b)
	if err == nil && n != len(b) {
		err = errors.New("short credential write")
	}
	if err != nil {
		_ = f.Close()
		return err
	}
	if err = ctx.Err(); err != nil {
		_ = f.Close()
		return err
	}
	if err = s.ops.syncFile(f); err != nil {
		_ = f.Close()
		return err
	}
	if err = ctx.Err(); err != nil {
		_ = f.Close()
		return err
	}
	if err = s.ops.closeFile(f); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.ops.rename(name, path); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrIndeterminate, err)
	}
	dir, err := os.Open(s.home)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIndeterminate, err)
	}
	if err = ctx.Err(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("%w: %w", ErrIndeterminate, err)
	}
	err = s.ops.syncDir(dir)
	closeErr := dir.Close()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIndeterminate, err)
	}
	if closeErr != nil {
		return fmt.Errorf("%w: %v", ErrIndeterminate, closeErr)
	}
	if err = ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrIndeterminate, err)
	}
	return nil
}

// Read returns one credential and the store-wide revision.
func (s *AuthStore) Read(ctx context.Context, provider string) (Credential, uint64, error) {
	var c Credential
	if !validProvider(provider) {
		return c, 0, fmt.Errorf("%w: provider", ErrUnsafeStore)
	}
	if err := ensureHome(s.home); err != nil {
		return c, 0, err
	}
	unlock, err := lockSidecar(ctx, filepath.Join(s.home, "auth.json.lock"))
	if err != nil {
		return c, 0, err
	}
	defer unlock()
	path := filepath.Join(s.home, "auth.json")
	if err = checkEntry(path, 0600); err != nil {
		return c, 0, err
	}
	d, err := readDocument(path)
	if err != nil {
		return c, 0, err
	}
	raw := d.providers[provider]
	if len(raw) == 0 {
		return c, d.revision, nil
	}
	c, err = decodeCredential(raw)
	return c, d.revision, err
}

// Replace writes one credential only if the store-wide revision still matches.
func (s *AuthStore) Replace(ctx context.Context, p string, expected uint64, c Credential) (uint64, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	if _, err = decodeCredential(raw); err != nil {
		return 0, err
	}
	return s.transaction(ctx, p, func(d *authDocument) error {
		if d.revision != expected {
			return ErrConflict
		}
		old := d.providers[p]
		if len(old) > 0 {
			prior, e := decodeCredential(old)
			if e != nil {
				return e
			}
			c.Generation = prior.Generation + 1
		} else {
			c.Generation = 1
		}
		c.RefreshState = nil
		raw, e := marshalCredential(d.providers[p], c)
		if e != nil {
			return e
		}
		d.providers[p] = raw
		return nil
	})
}

// Logout deletes one credential and advances revision even if it was absent.
func (s *AuthStore) Logout(ctx context.Context, p string, expected uint64) (uint64, error) {
	return s.transaction(ctx, p, func(d *authDocument) error {
		if d.revision != expected {
			return ErrConflict
		}
		delete(d.providers, p)
		return nil
	})
}

// FenceRefresh durably blocks reuse of the saved rotating grant.
func (s *AuthStore) FenceRefresh(ctx context.Context, p string, generation uint64, attemptID string) (Credential, error) {
	var c Credential
	if strings.TrimSpace(attemptID) == "" {
		return c, fmt.Errorf("%w: empty attempt", ErrUnsafeStore)
	}
	_, err := s.transaction(ctx, p, func(d *authDocument) error {
		raw := d.providers[p]
		if len(raw) == 0 {
			return ErrConflict
		}
		var e error
		c, e = decodeCredential(raw)
		if e != nil {
			return e
		}
		if c.Generation != generation {
			return ErrConflict
		}
		if c.RefreshState != nil {
			return ErrPendingRefresh
		}
		if c.OAuth == nil {
			return ErrConflict
		}
		c.RefreshState = &RefreshState{Status: "pending", Generation: generation, AttemptID: attemptID}
		d.providers[p], e = marshalCredential(d.providers[p], c)
		return e
	})
	return c, err
}

// CommitRefresh accepts only the attempt that owns the durable fence.
func (s *AuthStore) CommitRefresh(ctx context.Context, p string, generation uint64, attemptID string, replacement Credential) (uint64, error) {
	raw, err := json.Marshal(replacement)
	if err != nil {
		return 0, err
	}
	if _, err = decodeCredential(raw); err != nil {
		return 0, err
	}
	return s.transaction(ctx, p, func(d *authDocument) error {
		old := d.providers[p]
		if len(old) == 0 {
			return ErrConflict
		}
		c, e := decodeCredential(old)
		if e != nil {
			return e
		}
		if c.Generation != generation || c.RefreshState == nil || c.RefreshState.AttemptID != attemptID {
			return ErrConflict
		}
		replacement.Generation = generation + 1
		replacement.RefreshState = nil
		d.providers[p], e = marshalCredential(d.providers[p], replacement)
		return e
	})
}

// Refresh holds the lock across the durable fence, exchange and replacement.
// An exchange error leaves the fence for explicit reauthentication.
func (s *AuthStore) Refresh(ctx context.Context, p string, generation uint64, attemptID string, exchange func(context.Context, Credential) (Credential, error)) (Credential, error) {
	var empty Credential
	if !validProvider(p) || strings.TrimSpace(attemptID) == "" || exchange == nil {
		return empty, fmt.Errorf("%w: refresh arguments", ErrUnsafeStore)
	}
	if err := ensureHome(s.home); err != nil {
		return empty, err
	}
	unlock, err := lockSidecar(ctx, filepath.Join(s.home, "auth.json.lock"))
	if err != nil {
		return empty, err
	}
	defer unlock()
	path := filepath.Join(s.home, "auth.json")
	if err = checkEntry(path, 0600); err != nil {
		return empty, err
	}
	d, err := readDocument(path)
	if err != nil {
		return empty, err
	}
	raw := d.providers[p]
	if len(raw) == 0 {
		return empty, ErrConflict
	}
	current, err := decodeCredential(raw)
	if err != nil {
		return empty, err
	}
	if current.RefreshState != nil {
		return empty, ErrPendingRefresh
	}
	if current.OAuth == nil {
		return empty, ErrConflict
	}
	if current.Generation != generation {
		return cloneCredential(current), nil
	}
	current.RefreshState = &RefreshState{Status: "pending", Generation: generation, AttemptID: attemptID}
	d.providers[p], err = marshalCredential(raw, current)
	if err != nil {
		return empty, err
	}
	if _, err = s.writeDocument(ctx, path, &d); err != nil {
		return empty, err
	}
	exchangeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := exchangeCtx.Err(); err != nil {
		return empty, err
	}
	replacement, err := exchange(exchangeCtx, cloneCredential(current))
	if err != nil {
		return empty, err
	}
	if replacement.Method != current.Method || replacement.OAuth == nil || replacement.APIKey != "" {
		return empty, fmt.Errorf("%w: invalid refresh replacement", ErrUnsafeStore)
	}
	commitCtx, commitCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer commitCancel()
	replacement.Generation = generation + 1
	replacement.RefreshState = nil
	raw, err = marshalCredential(d.providers[p], replacement)
	if err != nil {
		return empty, err
	}
	if _, err = decodeCredential(raw); err != nil {
		return empty, err
	}
	d.providers[p] = raw
	if _, err = s.writeDocument(commitCtx, path, &d); err != nil {
		return empty, err
	}
	return cloneCredential(replacement), nil
}

func cloneCredential(c Credential) Credential {
	if c.OAuth != nil {
		oauth := *c.OAuth
		oauth.Scopes = append([]string(nil), oauth.Scopes...)
		c.OAuth = &oauth
	}
	if c.RefreshState != nil {
		state := *c.RefreshState
		c.RefreshState = &state
	}
	return c
}

// marshalCredential replaces owned fields and retains provider extensions.
func marshalCredential(prior json.RawMessage, c Credential) (json.RawMessage, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var old, next map[string]json.RawMessage
	if len(prior) > 0 {
		if err = json.Unmarshal(prior, &old); err != nil {
			return nil, err
		}
	}
	if old == nil {
		old = make(map[string]json.RawMessage)
	}
	if err = json.Unmarshal(raw, &next); err != nil {
		return nil, err
	}
	if c.OAuth != nil && len(old["oauth"]) > 0 {
		var oauth map[string]json.RawMessage
		if err = json.Unmarshal(old["oauth"], &oauth); err != nil {
			return nil, err
		}
		if oauth == nil {
			oauth = make(map[string]json.RawMessage)
		}
		for _, key := range []string{"accessToken", "refreshToken", "idToken", "expiresAt", "scopes", "refreshNotBefore", "clientId", "subject", "issuer", "signInHint"} {
			delete(oauth, key)
		}
		var replacement map[string]json.RawMessage
		if err = json.Unmarshal(next["oauth"], &replacement); err != nil {
			return nil, err
		}
		for key, value := range replacement {
			oauth[key] = value
		}
		next["oauth"], err = json.Marshal(oauth)
		if err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"method", "apiKey", "oauth", "generation", "refreshState", "type", "key", "access", "refresh"} {
		delete(old, key)
	}
	for key, value := range next {
		old[key] = value
	}
	return json.Marshal(old)
}
