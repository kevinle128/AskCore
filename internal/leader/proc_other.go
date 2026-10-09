//go:build !darwin && !linux

package leader

import (
	"errors"
	"fmt"
)

// processInfo is not available here, so the signal fallback fails closed.
func processInfo(int) (uint32, int64, string, error) {
	return 0, 0, "", errors.New("process identity is not supported on this platform")
}

// There is no stable process handle for this fallback on this platform.
func signalLeaderProcess(Paths, Owner) error {
	return fmt.Errorf("%w: stable process signaling is not available; use the socket shutdown control", ErrNotLeaderProcess)
}
