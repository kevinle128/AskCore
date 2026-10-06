package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HostID returns the durable UUID of this local installation.
func (s *AuthStore) HostID(ctx context.Context) (string, error) {
	if err := ensureHome(s.home); err != nil {
		return "", err
	}
	unlock, err := lockSidecar(ctx, filepath.Join(s.home, "auth.json.lock"))
	if err != nil {
		return "", err
	}
	defer unlock()
	path := filepath.Join(s.home, "host-id")
	if err := checkEntry(path, 0600); err != nil {
		return "", err
	}
	f, err := openNoFollow(path, os.O_RDONLY, 0600)
	if err == nil {
		raw, readErr := io.ReadAll(io.LimitReader(f, 38))
		closeErr := f.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		id := strings.TrimSpace(string(raw))
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			return "", fmt.Errorf("%w: host identity", ErrUnsafeStore)
		}
		if _, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err != nil {
			return "", fmt.Errorf("%w: host identity", ErrUnsafeStore)
		}
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
	if err := s.commit(ctx, path, []byte(id+"\n")); err != nil {
		return "", err
	}
	return id, nil
}
