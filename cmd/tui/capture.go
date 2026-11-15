package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"AskCore/internal/providers/cassette"
)

// captureEnv names a directory. When it is set, ask records the provider
// HTTP of the run to a new cassette there, so a run seen in real use can
// become a replay test.
const captureEnv = "ASK_CAPTURE"

// startCapture sets a recording transport on o when ASK_CAPTURE is set and
// tells stderr where the cassette goes. It returns the cassette path, or ""
// when capture is off or the provider makes no HTTP requests. real sends the requests; nil means
// http.DefaultTransport. The cassette file is made empty now and filled after each response.
func startCapture(o *options, getenv func(string) string, stderr io.Writer, real http.RoundTripper) (string, error) {
	dir := getenv(captureEnv)
	if dir == "" {
		return "", nil
	}
	// Every provider but faux sends HTTP, custom providers of models.json too.
	if o.provider == "" || o.provider == defaultProvider {
		report(stderr, "ask: "+captureEnv+" ignored: provider", o.provider, "makes no HTTP requests")
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("%s: %w", captureEnv, err)
	}
	// MkdirAll leaves the mode of an existing folder alone, and both it and
	// chmod follow a symlink. Capture writes only to a real folder that the
	// user owns, then closes that folder to others.
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", captureEnv, err)
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !owned || stat.Uid != uint32(os.Geteuid()) {
		return "", fmt.Errorf("%s: %s must be a folder that you own, not a symlink", captureEnv, dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("%s: %w", captureEnv, err)
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405.000")+".yaml")
	var opts []cassette.Option
	if real == nil {
		real = o.transport
	}
	if real != nil {
		opts = append(opts, cassette.WithRealTransport(real))
	}
	rec, err := cassette.New(path, cassette.Record, opts...)
	if err != nil {
		return "", err
	}
	o.transport = rec
	report(stderr, "ask: capturing provider HTTP to", path)
	return path, nil
}
