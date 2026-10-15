package openai

import (
	"net/http"
	"os"
	"time"

	"AskCore/internal/providers"
)

const (
	userAgent = "AskCore"
	streamBuf = 64
)

type Option func(*config)

type config struct {
	env       func(string) (string, bool)
	http      *http.Client
	now       func() int64
	normalize providers.NormalizeToolCallID
	idle      time.Duration
}

func WithEnv(lookup func(string) (string, bool)) Option {
	return func(c *config) { c.env = lookup }
}

func WithHTTPClient(c *http.Client) Option {
	return func(cfg *config) { cfg.http = c }
}

func WithNow(now func() int64) Option {
	return func(c *config) { c.now = now }
}

func WithNormalizeID(fn providers.NormalizeToolCallID) Option {
	return func(c *config) { c.normalize = fn }
}

func WithIdle(d time.Duration) Option {
	return func(c *config) { c.idle = d }
}

func newConfig(opts []Option, normalize providers.NormalizeToolCallID) config {
	cfg := config{
		env:       os.LookupEnv,
		now:       func() int64 { return time.Now().UnixMilli() },
		normalize: normalize,
		idle:      5 * time.Minute,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.env == nil {
		cfg.env = os.LookupEnv
	}
	if cfg.now == nil {
		cfg.now = func() int64 { return time.Now().UnixMilli() }
	}
	if cfg.normalize == nil {
		cfg.normalize = normalize
	}
	return cfg
}
