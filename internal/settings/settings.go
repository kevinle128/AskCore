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
	"unicode"
)

const (
	settingsFile       = "settings.json"
	defaultModelKey    = "defaultModel"
	maxModelIDLen      = 256
	maxSettingsFileLen = 1 << 20
)

// SettingsStore reads and writes the user settings file in one owner-only home.
// It follows the same file rules as AuthStore: a lock sidecar, a 0600 file in a
// 0700 home, read-merge-write that keeps fields it does not own, and an atomic
// replace with a directory sync.
type SettingsStore struct {
	// files supplies the home path and the atomic commit; it never touches auth.json here.
	files *AuthStore
}

// NewSettingsStore constructs a store. Empty home uses ASK_HOME or ~/.ask.
func NewSettingsStore(home string) (*SettingsStore, error) {
	files, err := NewAuthStore(home)
	if err != nil {
		return nil, err
	}
	return &SettingsStore{files: files}, nil
}

// DefaultModel returns the saved default model id (provider/id@api), or an
// empty string when no default was saved.
func (s *SettingsStore) DefaultModel(ctx context.Context) (string, error) {
	var id string
	err := s.locked(ctx, func(fields map[string]json.RawMessage) (bool, error) {
		raw, ok := fields[defaultModelKey]
		if !ok {
			return false, nil
		}
		if err := json.Unmarshal(raw, &id); err != nil {
			return false, fmt.Errorf("%w: invalid default model", ErrUnsafeStore)
		}
		return false, nil
	})
	return id, err
}

// SetDefaultModel saves the default model id and keeps every other field.
func (s *SettingsStore) SetDefaultModel(ctx context.Context, modelID string) error {
	if !validModelID(modelID) {
		return errors.New("invalid default model id")
	}
	value, err := json.Marshal(modelID)
	if err != nil {
		return err
	}
	return s.locked(ctx, func(fields map[string]json.RawMessage) (bool, error) {
		fields[defaultModelKey] = value
		return true, nil
	})
}

func validModelID(id string) bool {
	if id == "" || len(id) > maxModelIDLen || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// locked runs fn on the fresh fields under the lock. fn returns true when the
// fields changed and must be written.
func (s *SettingsStore) locked(ctx context.Context, fn func(map[string]json.RawMessage) (bool, error)) error {
	home := s.files.home
	if err := ensureHomeShared(home); err != nil {
		return err
	}
	unlock, err := lockSidecar(ctx, filepath.Join(home, settingsFile+".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(home, settingsFile)
	if err = checkEntry(path, 0600); err != nil {
		return err
	}
	fields, err := readSettings(path)
	if err != nil {
		return err
	}
	write, err := fn(fields)
	if err != nil || !write {
		return err
	}
	b, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return s.files.commit(ctx, path, b)
}

// ensureHomeShared creates the home when it is missing. Two processes can both
// see it missing and race to create it; the loser sees "exists" and checks the
// folder again, which now passes or fails on its real owner and mode.
func ensureHomeShared(home string) error {
	err := ensureHome(home)
	if errors.Is(err, os.ErrExist) {
		err = ensureHome(home)
	}
	return err
}

func readSettings(path string) (map[string]json.RawMessage, error) {
	f, err := openNoFollow(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owner(info) {
		return nil, fmt.Errorf("%w: settings file mode", ErrUnsafeStore)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSettingsFileLen+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxSettingsFileLen {
		return nil, fmt.Errorf("%w: settings file too large", ErrUnsafeStore)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%w: invalid JSON", ErrUnsafeStore)
	}
	return fields, nil
}
