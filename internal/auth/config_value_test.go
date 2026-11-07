package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock { return &testClock{now: time.Unix(1_800_000_000, 0)} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func mapEnv(vars map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

type blockingRunner struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	out     string
	err     error
	ctxErr  atomic.Value // the run ctx error at release time
}

func newBlockingRunner(out string, err error) *blockingRunner {
	return &blockingRunner{started: make(chan struct{}, 8), release: make(chan struct{}), out: out, err: err}
}

func (r *blockingRunner) Run(ctx context.Context, _ string) (string, error) {
	r.calls.Add(1)
	r.started <- struct{}{}
	<-r.release
	if err := ctx.Err(); err != nil {
		r.ctxErr.Store(err)
		return "", err
	}
	return r.out, r.err
}

func TestConfigValueGrammar(t *testing.T) {
	env := mapEnv(map[string]string{"HOME_KEY": "home-secret", "A": "aa", "BLANK": "  "})
	cases := []struct {
		raw, text, command, name, errPart string
	}{
		{raw: "literal", text: "literal", name: "literal"},
		{raw: "$HOME_KEY", text: "home-secret", name: "$HOME_KEY"},
		{raw: "${A}b", text: "aab", name: "$A"},
		{raw: "x-${A}-$HOME_KEY", text: "x-aa-home-secret", name: "$A,$HOME_KEY"},
		{raw: "$$", text: "$", name: "literal"},
		{raw: "a$$b", text: "a$b", name: "literal"},
		{raw: "$!x", text: "!x", name: "literal"},
		{raw: "!cmd --flag $X", command: "cmd --flag $X", name: "command"},
		{raw: "$1", errPart: "bad $"},
		{raw: "tail$", errPart: "bad $"},
		{raw: "${A", errPart: "bad ${"},
		{raw: "${1A}", errPart: "bad ${"},
		{raw: "a$!b", errPart: "bad $"},
		{raw: "!", errPart: "empty command"},
		{raw: "$MISSING_KEY", errPart: "variable MISSING_KEY is not set"},
		{raw: "${BLANK}", errPart: "variable BLANK is not set"},
	}
	for _, tc := range cases {
		p, err := parseValue(tc.raw, env)
		if tc.errPart != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errPart) {
				t.Fatalf("%q: want error with %q, got %v", tc.raw, tc.errPart, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.raw, err)
		}
		if p.text != tc.text || p.command != tc.command || p.sourceName() != tc.name {
			t.Fatalf("%q: got text %q command %q name %q", tc.raw, p.text, p.command, p.sourceName())
		}
	}
	v := NewValues(nil, env, nil)
	_, err := v.Expand(context.Background(), "local apiKey", "${BLANK}")
	if !errors.Is(err, ErrValue) || !strings.Contains(err.Error(), "local apiKey") || strings.Contains(err.Error(), "  ") {
		t.Fatalf("unset variable error: %v", err)
	}
	if _, err := v.Expand(context.Background(), "local apiKey", "!op read x"); !errors.Is(err, ErrValue) {
		t.Fatalf("command with no runner: %v", err)
	}
}

func TestConfigValueCommandRunsOnce(t *testing.T) {
	r := newBlockingRunner("  secret-out \n", nil)
	var deadline time.Duration
	run := func(ctx context.Context, cmd string) (string, error) {
		if d, ok := ctx.Deadline(); ok {
			deadline = time.Until(d)
		}
		return r.Run(ctx, cmd)
	}
	v := NewValues(run, mapEnv(nil), newTestClock().Now)
	var wg sync.WaitGroup
	results := make([]string, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = v.Expand(context.Background(), "local apiKey", "!print key")
		}(i)
	}
	<-r.started
	close(r.release)
	wg.Wait()
	for i := range results {
		if errs[i] != nil || results[i] != "secret-out" {
			t.Fatalf("caller %d: %q, %v", i, results[i], errs[i])
		}
	}
	if got, err := v.Expand(context.Background(), "local apiKey", "!print key"); err != nil || got != "secret-out" {
		t.Fatalf("cached: %q, %v", got, err)
	}
	if n := r.calls.Load(); n != 1 {
		t.Fatalf("runs: %d", n)
	}
	if deadline <= 0 || deadline > commandTimeout {
		t.Fatalf("run deadline: %v", deadline)
	}
}

func TestConfigValueCallerCancelDoesNotPoisonCache(t *testing.T) {
	r := newBlockingRunner("value", nil)
	v := NewValues(r.Run, mapEnv(nil), newTestClock().Now)
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := v.Expand(ctx, "local apiKey", "!get")
		first <- err
	}()
	<-r.started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller: %v", err)
	}
	second := make(chan string, 1)
	go func() {
		got, err := v.Expand(context.Background(), "local apiKey", "!get")
		if err != nil {
			got = "error: " + err.Error()
		}
		second <- got
	}()
	close(r.release)
	if got := <-second; got != "value" {
		t.Fatalf("second caller: %q", got)
	}
	if got, err := v.Expand(context.Background(), "local apiKey", "!get"); err != nil || got != "value" {
		t.Fatalf("after cancel: %q, %v", got, err)
	}
	if n := r.calls.Load(); n != 1 {
		t.Fatalf("runs: %d", n)
	}
	if err := r.ctxErr.Load(); err != nil {
		t.Fatalf("run context ended with the caller: %v", err)
	}
}

func TestConfigValueCommandBackoff(t *testing.T) {
	clock := newTestClock()
	var calls atomic.Int32
	s := &Service{Now: clock.Now, RunCommand: func(context.Context, string) (string, error) {
		calls.Add(1)
		return "", &ExitError{Code: 1}
	}}
	v := s.Values()
	want := "command for local apiKey failed (exit 1)"
	if _, err := v.Expand(context.Background(), "local apiKey", "!fail"); err == nil || err.Error() != want {
		t.Fatalf("first: %v", err)
	}
	clock.Add(10 * time.Second)
	if _, err := v.Expand(context.Background(), "local apiKey", "!fail"); err == nil || err.Error() != want || calls.Load() != 1 {
		t.Fatalf("in backoff: %v, runs %d", err, calls.Load())
	}
	clock.Add(21 * time.Second)
	if _, err := v.Expand(context.Background(), "local apiKey", "!fail"); err == nil || calls.Load() != 2 {
		t.Fatalf("after backoff: %v, runs %d", err, calls.Load())
	}
}

func TestConfigValueCommandFailures(t *testing.T) {
	cases := []struct {
		name string
		run  RunCommand
		want string
	}{
		{"empty output", func(context.Context, string) (string, error) { return " \n", nil }, "command for p apiKey failed (no output)"},
		{"start error", func(context.Context, string) (string, error) { return "", errors.New("exec: not found") }, "command for p apiKey failed"},
		{"panic", func(context.Context, string) (string, error) { panic("boom") }, "command for p apiKey failed"},
		{"timeout", func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}, "command for p apiKey timed out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewValues(tc.run, mapEnv(nil), newTestClock().Now)
			v.timeout = time.Millisecond
			if _, err := v.Expand(context.Background(), "p apiKey", "!x"); err == nil || err.Error() != tc.want || !errors.Is(err, ErrValue) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestConfigValueForgetRunsAgain(t *testing.T) {
	var calls atomic.Int32
	v := NewValues(func(context.Context, string) (string, error) {
		calls.Add(1)
		return "k", nil
	}, mapEnv(nil), newTestClock().Now)
	for _, forget := range []func(){func() { v.Forget("get") }, v.ForgetAll} {
		if _, err := v.Expand(context.Background(), "p apiKey", "!get"); err != nil {
			t.Fatal(err)
		}
		forget()
	}
	if _, err := v.Expand(context.Background(), "p apiKey", "!get"); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("runs: %d", n)
	}
}

func TestConfigValueForgetDuringRunDropsResult(t *testing.T) {
	r := newBlockingRunner("old", nil)
	v := NewValues(r.Run, mapEnv(nil), newTestClock().Now)
	first := make(chan string, 1)
	go func() {
		got, _ := v.Expand(context.Background(), "p apiKey", "!get")
		first <- got
	}()
	<-r.started
	v.Forget("get")
	r.release <- struct{}{}
	if got := <-first; got != "old" {
		t.Fatalf("waiting caller: %q", got)
	}
	second := make(chan string, 1)
	go func() {
		got, _ := v.Expand(context.Background(), "p apiKey", "!get")
		second <- got
	}()
	<-r.started
	close(r.release)
	<-second
	if n := r.calls.Load(); n != 2 {
		t.Fatalf("runs: %d", n)
	}
}

func TestConfigValueOutputNotInError(t *testing.T) {
	const marker = "MARKER-7f3a"
	runs := []RunCommand{
		func(context.Context, string) (string, error) { return marker, &ExitError{Code: 2} },
		func(context.Context, string) (string, error) { return marker, errors.New("stderr: " + marker) },
	}
	for i, run := range runs {
		v := NewValues(run, mapEnv(nil), newTestClock().Now)
		for range 2 {
			_, err := v.Expand(context.Background(), "p apiKey", "!echo "+marker)
			if err == nil || strings.Contains(err.Error(), marker) {
				t.Fatalf("run %d: error text holds output or command: %v", i, err)
			}
		}
	}
}
