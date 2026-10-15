package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// Dev builds are binaries without a VCS stamp. devBinaries compiles two of them
// from the same source: -trimpath changes the paths that the binary embeds, so
// the bytes differ and so do the hashes. Each binary computes its stamp with
// the production code.
var (
	devOnce sync.Once
	devDir  string
	devOld  string
	devNew  string
	devErr  error
)

func devBinaries(t *testing.T) (oldBin, newBin string) {
	t.Helper()
	devOnce.Do(func() {
		devDir, devErr = os.MkdirTemp("", "ask-dev-e2e-")
		if devErr != nil {
			return
		}
		devOld = filepath.Join(devDir, "ask-dev-old")
		devNew = filepath.Join(devDir, "ask-dev-new")
		for _, b := range []struct {
			out  string
			args []string
		}{
			{devOld, []string{"-buildvcs=false"}},
			{devNew, []string{"-buildvcs=false", "-trimpath"}},
		} {
			args := append([]string{"build"}, b.args...)
			args = append(args, "-o", b.out, ".")
			if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
				devErr = &buildError{err: err, out: string(out)}
				return
			}
		}
	})
	require.NoError(t, devErr)
	return devOld, devNew
}

type buildError struct {
	err error
	out string
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }

// removeDevBinaries deletes the binaries that devBinaries built.
func removeDevBinaries() {
	if devDir != "" {
		_ = os.RemoveAll(devDir)
	}
}

// devCopy copies a binary to a new name and sets its modification time.
func devCopy(t *testing.T, src, name string, mtime time.Time) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), name)
	in, err := os.Open(src)
	require.NoError(t, err)
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	require.NoError(t, err)
	_, err = io.Copy(out, in)
	require.NoError(t, err)
	require.NoError(t, out.Close())
	require.NoError(t, os.Chtimes(dst, mtime, mtime))
	return dst
}

// expectedDevStamp computes the dev stamp of a file as the product defines it.
func expectedDevStamp(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	h := sha256.New()
	_, err = io.Copy(h, f)
	require.NoError(t, err)
	return info.ModTime().UTC().Format(time.RFC3339Nano) + " sha256:" + hex.EncodeToString(h.Sum(nil))
}

// versionOf runs `<bin> version --json`.
func versionOf(t *testing.T, bin string) protocol.VersionInfo {
	t.Helper()
	out, err := exec.Command(bin, "version", "--json").Output()
	require.NoError(t, err)
	var info protocol.VersionInfo
	require.NoError(t, json.Unmarshal(out, &info))
	return info
}
