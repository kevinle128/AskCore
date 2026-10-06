package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

const lockWait = 5 * time.Second

func lockSidecar(ctx context.Context, path string) (func(), error) {
	if err := checkEntry(path, 0600); err != nil {
		return nil, err
	}
	f, err := openNoFollow(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owner(info) {
		_ = f.Close()
		if statErr != nil {
			return nil, statErr
		}
		return nil, fmt.Errorf("%w: lock file owner, type or mode", ErrUnsafeStore)
	}
	deadline := time.NewTimer(lockWait)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		err = tryFileLock(f)
		if err == nil {
			return func() { _ = unlockFile(f); _ = f.Close() }, nil
		}
		if !errors.Is(err, errLockBusy) {
			_ = f.Close()
			return nil, fmt.Errorf("lock credential store: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			_ = f.Close()
			return nil, fmt.Errorf("credential lock timeout: %w", err)
		case <-tick.C:
		}
	}
}
