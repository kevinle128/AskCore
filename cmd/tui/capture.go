package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"AskCore/internal/providers/cassette"
	"AskCore/internal/providers/tokenplan"
)

// captureEnv names a directory. When it is set, ask records the provider
// HTTP of the run to a new cassette there, so a run seen in real use can
// become a replay test.
const captureEnv = "ASK_CAPTURE"

// startCapture sets a recording transport on o when ASK_CAPTURE is set and
// tells stderr where the cassette goes. It returns the cassette path, or ""
// when capture is off or the provider makes no HTTP requests. real sends the requests; nil means
// http.DefaultTransport. The cassette is saved after each response, so it
// exists only after the first provider request.
func startCapture(o *options, getenv func(string) string, stderr io.Writer, real http.RoundTripper) (string, error) {
	dir := getenv(captureEnv)
	if dir == "" {
		return "", nil
	}
	if o.provider != tokenplan.ProviderID {
		report(stderr, "ask: "+captureEnv+" ignored: provider", o.provider, "makes no HTTP requests")
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("%s: %w", captureEnv, err)
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405.000")+".yaml")
	var opts []cassette.Option
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
