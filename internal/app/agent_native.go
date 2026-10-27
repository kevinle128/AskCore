package app

import (
	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/openai"
	"AskCore/internal/settings"
	"AskCore/internal/tasks"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// NativeAgentConfig supplies session ownership and private inference transport.
// Config retains tool, session writer, pipeline, and lifecycle options.
type NativeAgentConfig struct {
	Config agent.Config
	// InitialStream serves InitialAPI, or Config.Model.API when InitialAPI is
	// empty. Its API can differ from the saved model of a new session.
	InitialStream providers.StreamFn
	InitialAPI    providers.API
	Auth          *auth.Service
	Env           func(string) (string, bool)
	HTTPClient    *http.Client
	// Home is the Ask home directory, which holds the task output of the
	// session. Empty selects ASK_HOME (through Env when it is set) or ~/.ask.
	Home string
}

// NewNativeAgent creates one independent Agent with all supported native wires.
func NewNativeAgent(in NativeAgentConfig) (*agent.Agent, error) {
	cfg := in.Config
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("native agent: session ID is required")
	}
	if !filepath.IsAbs(cfg.Cwd) {
		return nil, fmt.Errorf("native agent: cwd must be absolute")
	}
	info, err := os.Stat(cfg.Cwd)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("native agent: cwd must be a directory")
	}
	if cfg.TaskDir == "" {
		cfg.TaskDir = taskDir(in, cfg.SessionID)
		if cfg.TaskDir != "" && cfg.PrepareTaskDir == nil {
			cfg.PrepareTaskDir = ensureTaskDir
		}
	}
	wires := providers.NewRegistry()
	// The initial stream serves the demo model. A session with no demo model
	// has none and uses the native wires only.
	if in.InitialStream != nil {
		api := in.InitialAPI
		if api == "" {
			api = cfg.Model.API
		}
		wires.Register(api, in.InitialStream)
	}
	aopts := []anthropic.Option{anthropic.WithEnv(in.Env)}
	oopts := []openai.Option{openai.WithEnv(in.Env)}
	if in.HTTPClient != nil {
		aopts = append(aopts, anthropic.WithHTTPClient(in.HTTPClient))
		oopts = append(oopts, openai.WithHTTPClient(in.HTTPClient))
	}
	wires.RegisterProvider(anthropic.New(aopts...))
	wires.RegisterProvider(openai.NewCompletions(oopts...))
	wires.RegisterProvider(openai.NewResponses(oopts...))
	cfg.Registry = wires
	cfg.Stream = wires.Stream
	if in.Auth != nil {
		cfg = BindAuth(cfg, in.Auth, wires)
		stream, ready := cfg.Stream, cfg.Ready
		cfg.Stream = func(ctx context.Context, m providers.Model, r providers.TranscriptRequest, o providers.StreamOptions) *providers.Stream {
			if m.Provider == "faux" {
				return wires.Stream(ctx, m, r, o)
			}
			return stream(ctx, m, r, o)
		}
		cfg.Ready = func(ctx context.Context, m providers.Model, key string) error {
			if m.Provider == "faux" {
				return nil
			}
			return markModelAccess(ready(ctx, m, key))
		}
	}
	return agent.New(cfg)
}

// sweepTasksOnce makes the first Agent of the process remove the task output of
// old sessions. It runs once, so no timer or poll is needed.
var sweepTasksOnce sync.Once

// taskDir returns the directory for the task output of session:
// <home>/tasks/<session>. It does not touch the file system for that
// directory: the registry calls ensureTaskDir at the first output, so a
// session without output makes no directory. The first call in the process
// removes session directories older than tasks.DefaultRetention, but only from
// a home and a tasks directory that exist and are private (see
// sweepTaskOutput); it only reads and removes. A failure is
// logged and gives an empty directory: the Agent still works, and only the
// tasks that need an output file cannot start.
func taskDir(in NativeAgentConfig, session string) string {
	if !plainName(session) {
		slog.Warn("task output disabled: session ID is not a plain name", "session", session)
		return ""
	}
	home := in.Home
	if home == "" && in.Env != nil {
		home, _ = in.Env("ASK_HOME")
	}
	home, err := settings.ResolveHome(home)
	if err != nil {
		slog.Warn("task output disabled", "error", err)
		return ""
	}
	root := filepath.Join(home, "tasks")
	sweepTasksOnce.Do(func() { sweepTaskOutput(home, root, session) })
	return filepath.Join(root, session)
}

// sweepTaskOutput removes old session directories under root. The sweep removes
// files, so it runs only when home and root both exist and only the user can use
// them: no link, no foreign owner, mode 0700. Otherwise the directory may hold
// data that is not task output, and the sweep is skipped. It does not create or
// change anything to make the check pass.
func sweepTaskOutput(home, root, session string) {
	for _, dir := range []string{home, root} {
		exists, err := settings.CheckPrivateDir(dir)
		if err != nil {
			slog.Warn("old task output was not removed: unsafe directory", "dir", dir, "error", err)
			return
		}
		if !exists {
			slog.Debug("no old task output to remove", "dir", dir)
			return
		}
	}
	if err := tasks.Sweep(root, tasks.DefaultRetention, time.Now(), session); err != nil {
		slog.Warn("old task output was not removed", "error", err)
	}
}

// ensureTaskDir creates and checks each level of <home>/tasks/<session>, as
// the credential store checks the home: no symbolic link, owned by the user,
// mode 0700.
func ensureTaskDir(dir string) error {
	root := filepath.Dir(dir)
	for _, level := range []string{filepath.Dir(root), root, dir} {
		if err := settings.EnsurePrivateDir(level); err != nil {
			return fmt.Errorf("task output directory: %w", err)
		}
	}
	return nil
}

// plainName reports whether name is safe as one path element.
func plainName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

// NativeAuthReady checks the effective method without changing the selected credential.
// Matching saved OAuth credentials can refresh through the normal auth resolver.
func NativeAuthReady(ctx context.Context, s *auth.Service, m providers.Model, key, requestedMethod string) error {
	if m.Provider == "faux" {
		if requestedMethod != "" {
			return auth.ErrMethod
		}
		return nil
	}
	if s == nil || s.Store == nil {
		return auth.ErrNoCredential
	}
	if requestedMethod != "" {
		effective := "api-key"
		if key == "" {
			c, _, err := s.Store.Read(ctx, m.Provider)
			if err != nil {
				return err
			}
			if c.Method != "" {
				effective = c.Method
			}
		}
		if effective != requestedMethod {
			return fmt.Errorf("%w: requested method is not the configured method", auth.ErrMethod)
		}
	}
	binding, err := s.Resolve(ctx, m, key)
	if err != nil {
		return markModelAccess(err)
	}
	if requestedMethod != "" && binding.Method != requestedMethod {
		return auth.ErrMethod
	}
	return nil
}

// markModelAccess tags an account access denial, so that a client can tell it
// from a missing sign-in. Any other error stays as it is.
func markModelAccess(err error) error {
	if errors.Is(err, auth.ErrAccess) {
		return fmt.Errorf("%w: %w", acp.ErrModelAccess, err)
	}
	return err
}
