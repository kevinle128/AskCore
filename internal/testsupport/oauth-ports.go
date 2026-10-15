package testsupport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// LockOAuthPorts prevents test packages from sharing registered callback ports.
// Product callbacks keep their fixed provider URI.
func LockOAuthPorts(t *testing.T) {
	t.Helper()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("askcore-oauth-test-%d.lock", os.Geteuid()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			_ = f.Close()
			t.Fatalf("callback test lock: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() })
}
