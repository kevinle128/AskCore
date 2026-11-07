package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const trustFile = "trust.json"

// ErrTrustStoreCorrupt means that trust.json is not valid or not safe: bad
// JSON, a value that is not a bool, a file that is not an owner-only regular
// file, or an unsafe home. The caller must then treat every project as
// untrusted. Update never overwrites such a file.
var ErrTrustStoreCorrupt = fmt.Errorf("trust store corrupt or unsafe: %w", ErrUnsafeStore)

// TrustStore reads and writes trust.json in one owner-only home. The file is
// one JSON object: a canonical project path to true (trusted) or false (not
// trusted). A null value is the same as no entry.
type TrustStore struct {
	// files supplies the home path and the atomic commit; it never touches auth.json here.
	files *AuthStore
}

// NewTrustStore constructs a store. Empty home uses ASK_HOME or ~/.ask.
func NewTrustStore(home string) (*TrustStore, error) {
	files, err := NewAuthStore(home)
	if err != nil {
		return nil, err
	}
	return &TrustStore{files: files}, nil
}

func (s *TrustStore) path() string { return filepath.Join(s.files.home, trustFile) }

// Read returns the saved entries. It takes no lock, because each replace is
// atomic, and it creates nothing. A missing home or a missing file gives an
// empty map and no error. A corrupt or unsafe file, or an unsafe home, gives
// an error that wraps ErrTrustStoreCorrupt and names the path, and no
// entries.
func (s *TrustStore) Read(ctx context.Context) (map[string]bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	exists, err := CheckPrivateDir(s.files.home)
	if err != nil {
		return nil, s.corrupt(err)
	}
	if !exists {
		return map[string]bool{}, nil
	}
	return s.read()
}

// Update applies edit to the saved entries and writes the file. A nil value
// deletes the key. Each key must be an absolute, clean path. Update takes the
// lock trust.json.lock and reads fresh data, so entries that another process
// wrote stay. It refuses a corrupt or unsafe file with ErrTrustStoreCorrupt
// and does not change it. When the edit changes nothing, nothing is written.
func (s *TrustStore) Update(ctx context.Context, edit map[string]*bool) error {
	for key := range edit {
		if !filepath.IsAbs(key) || filepath.Clean(key) != key {
			return fmt.Errorf("trust store: key %q is not an absolute clean path", key)
		}
	}
	if len(edit) == 0 {
		return nil
	}
	home := s.files.home
	if err := ensureHomeShared(home); err != nil {
		return err
	}
	unlock, err := lockSidecar(ctx, filepath.Join(home, trustFile+".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := s.read()
	if err != nil {
		return err
	}
	changed := false
	for key, value := range edit {
		old, ok := entries[key]
		switch {
		case value == nil && ok:
			delete(entries, key)
			changed = true
		case value != nil && (!ok || old != *value):
			entries[key] = *value
			changed = true
		}
	}
	if !changed {
		return nil
	}
	// json.MarshalIndent writes map keys in sorted order.
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return s.files.commit(ctx, s.path(), append(b, '\n'))
}

// read reads trust.json. It checks the entry with Lstat before it opens the
// file, so a symbolic link, a FIFO or a file with a wrong mode is refused and
// never opened.
func (s *TrustStore) read() (map[string]bool, error) {
	path := s.path()
	if err := checkEntry(path, 0600); err != nil {
		return nil, s.corrupt(err)
	}
	f, err := openNoFollow(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, s.corrupt(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, s.corrupt(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owner(info) {
		return nil, s.corrupt(errors.New("file owner, type or mode"))
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSettingsFileLen+1))
	if err != nil {
		return nil, s.corrupt(err)
	}
	if len(b) > maxSettingsFileLen {
		return nil, s.corrupt(errors.New("file too large"))
	}
	var raw map[string]*bool
	if err = json.Unmarshal(b, &raw); err != nil || raw == nil {
		return nil, s.corrupt(errors.New("not a JSON object of true, false or null values"))
	}
	entries := make(map[string]bool, len(raw))
	for key, value := range raw {
		if value != nil {
			entries[key] = *value
		}
	}
	return entries, nil
}

func (s *TrustStore) corrupt(cause error) error {
	return fmt.Errorf("%w: %s: %v", ErrTrustStoreCorrupt, s.path(), cause)
}
