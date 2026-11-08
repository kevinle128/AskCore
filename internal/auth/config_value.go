package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	commandTimeout = 10 * time.Second
	// commandBackoff is the time after a failed run during which the same
	// command gives the cached error and does not run again.
	commandBackoff = 30 * time.Second
)

// ErrValue marks a models.json value that cannot give a secret: a bad
// template, an unset variable, or a command that failed. The error text names
// a variable or a label, never a value or command output.
var ErrValue = errors.New("auth: models.json value")

// ConfigKey is the models.json apiKey of one provider. Endpoint is the base
// URL of that provider in the user file; the key binds only to a model row
// with the same BaseURL.
type ConfigKey struct {
	Value    string
	Endpoint string
}

// RunCommand runs a value command and returns its stdout. A non-zero exit
// must return an *ExitError. When ctx ends, the runner must stop the process
// and return ctx.Err().
type RunCommand func(ctx context.Context, command string) (stdout string, err error)

// ExitError is the error a RunCommand returns for a non-zero exit. It holds
// only the exit code, so no command output can reach an error text.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return "exit " + strconv.Itoa(e.Code) }

type valueError struct{ msg string }

func (e *valueError) Error() string { return e.msg }
func (e *valueError) Unwrap() error { return ErrValue }

func valueErrorf(format string, args ...any) error {
	return &valueError{msg: fmt.Sprintf(format, args...)}
}

type parsedValue struct {
	command string   // set when the whole value is a command
	text    string   // the expanded text when command is empty
	names   []string // variable names the template read, in order
}

// parseValue applies the value grammar. env reads a variable.
//
//   - A value that starts with "!" is a command: the rest of the string.
//   - "$!" at the start gives a literal "!".
//   - "$NAME" and "${NAME}" read env; NAME is [A-Za-z_][A-Za-z0-9_]*.
//   - "$$" gives a literal "$". Any other "$" form is an error.
//   - A variable that is unset or blank is an error that names the variable.
//   - Any other text is a literal.
func parseValue(raw string, env func(string) (string, bool)) (parsedValue, error) {
	if strings.HasPrefix(raw, "!") {
		command := raw[1:]
		if strings.TrimSpace(command) == "" {
			return parsedValue{}, valueErrorf("empty command")
		}
		return parsedValue{command: command}, nil
	}
	var b strings.Builder
	var names []string
	i := 0
	if strings.HasPrefix(raw, "$!") {
		b.WriteByte('!')
		i = 2
	}
	for i < len(raw) {
		c := raw[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(raw) {
			return parsedValue{}, valueErrorf("bad $ at position %d", i)
		}
		next := raw[i+1]
		var name string
		switch {
		case next == '$':
			b.WriteByte('$')
			i += 2
			continue
		case next == '{':
			end := strings.IndexByte(raw[i+2:], '}')
			if end < 0 || !validName(raw[i+2:i+2+end]) {
				return parsedValue{}, valueErrorf("bad ${...} at position %d", i)
			}
			name = raw[i+2 : i+2+end]
			i += 3 + end
		case nameStart(next):
			j := i + 2
			for j < len(raw) && namePart(raw[j]) {
				j++
			}
			name = raw[i+1 : j]
			i = j
		default:
			return parsedValue{}, valueErrorf("bad $ at position %d", i)
		}
		v, ok := env(name)
		if !ok || strings.TrimSpace(v) == "" {
			return parsedValue{}, valueErrorf("variable %s is not set", name)
		}
		b.WriteString(v)
		names = append(names, name)
	}
	return parsedValue{text: b.String(), names: names}, nil
}

func nameStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func namePart(c byte) bool { return nameStart(c) || c >= '0' && c <= '9' }

func validName(s string) bool {
	if s == "" || !nameStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !namePart(s[i]) {
			return false
		}
	}
	return true
}

// sourceName is the safe display name of a parsed value: "command",
// "$NAME" (several names joined by ","), or "literal".
func (p parsedValue) sourceName() string {
	switch {
	case p.command != "":
		return "command"
	case len(p.names) > 0:
		return "$" + strings.Join(p.names, ",$")
	default:
		return "literal"
	}
}

// commandEntry is one run of a command and, after done closes, its result.
// The fields other than done are written once, before done closes.
type commandEntry struct {
	done     chan struct{}
	value    string
	reason   string // empty on success
	failedAt time.Time
}

// Values resolves models.json values (apiKey and header values) and caches
// the stdout of value commands for the life of the process. Use one Values
// for each Service, through Service.Values.
type Values struct {
	run     RunCommand
	env     func(string) (string, bool)
	now     func() time.Time
	timeout time.Duration

	mu      sync.Mutex
	entries map[string]*commandEntry
}

// NewValues returns a value resolver. A nil env reads the process
// environment, a nil now uses time.Now, and a nil run makes every command
// fail.
func NewValues(run RunCommand, env func(string) (string, bool), now func() time.Time) *Values {
	if env == nil {
		env = os.LookupEnv
	}
	if now == nil {
		now = time.Now
	}
	return &Values{run: run, env: env, now: now, timeout: commandTimeout, entries: map[string]*commandEntry{}}
}

// Expand returns the secret of raw. label names the value in an error, for
// example "local apiKey". A command runs at most once at a time; its trimmed
// stdout stays cached until Forget. After a failed run the same error returns
// for 30 s with no new run. A caller whose ctx ends gets ctx.Err() and the run
// continues for the other callers.
func (v *Values) Expand(ctx context.Context, label, raw string) (string, error) {
	p, err := v.parse(label, raw)
	if err != nil {
		return "", err
	}
	if p.command == "" {
		return p.text, nil
	}
	return v.command(ctx, label, p.command)
}

// describe returns the safe source name of raw. It reads the environment but
// never runs a command. A value that is not a command and expands to blank
// text gives ErrNoCredential, as the resolution does.
func (v *Values) describe(label, raw string) (string, error) {
	p, err := v.parse(label, raw)
	if err != nil {
		return "", err
	}
	if p.command == "" && strings.TrimSpace(p.text) == "" {
		return "", ErrNoCredential
	}
	return p.sourceName(), nil
}

func (v *Values) parse(label, raw string) (parsedValue, error) {
	p, err := parseValue(raw, v.env)
	if err != nil {
		return parsedValue{}, valueErrorf("%s: %v", label, err)
	}
	return p, nil
}

func (v *Values) command(ctx context.Context, label, command string) (string, error) {
	if v.run == nil {
		return "", valueErrorf("command for %s failed (no command runner)", label)
	}
	v.mu.Lock()
	e := v.entries[command]
	if e != nil {
		select {
		case <-e.done:
			if e.reason == "" {
				v.mu.Unlock()
				return e.value, nil
			}
			if v.now().Before(e.failedAt.Add(commandBackoff)) {
				v.mu.Unlock()
				return "", valueErrorf("command for %s %s", label, e.reason)
			}
			e = nil
		default:
		}
	}
	if e == nil {
		e = &commandEntry{done: make(chan struct{})}
		v.entries[command] = e
		go v.runEntry(context.WithoutCancel(ctx), command, e)
	}
	v.mu.Unlock()
	select {
	case <-e.done:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if e.reason != "" {
		return "", valueErrorf("command for %s %s", label, e.reason)
	}
	return e.value, nil
}

// runEntry owns one run. Its context outlives every caller, so no caller
// cancellation ends the run or becomes a cached failure.
func (v *Values) runEntry(base context.Context, command string, e *commandEntry) {
	defer close(e.done)
	ctx, cancel := context.WithTimeout(base, v.timeout)
	defer cancel()
	out, err := v.safeRun(ctx, command)
	value := strings.TrimSpace(out)
	var reason string
	var exit *ExitError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		reason = "timed out"
	case errors.As(err, &exit):
		reason = fmt.Sprintf("failed (exit %d)", exit.Code)
	case err != nil:
		reason = "failed"
	case value == "":
		reason = "failed (no output)"
	}
	// The result goes into the entry, which the callers that wait read. When
	// Forget ran during the run, the entry is no longer in the map, so the
	// cache keeps nothing and the next call runs the command again.
	v.mu.Lock()
	defer v.mu.Unlock()
	if reason != "" {
		e.reason = reason
		e.failedAt = v.now()
	} else {
		e.value = value
	}
}

// safeRun turns a panic of the runner into a plain failure, so the waiters
// always get a result.
func (v *Values) safeRun(ctx context.Context, command string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", errors.New("command runner panic")
		}
	}()
	return v.run(ctx, command)
}

// Forget drops the cached result of command. The next Expand runs it again.
// A run that is still going keeps its result out of the cache.
func (v *Values) Forget(command string) {
	v.mu.Lock()
	delete(v.entries, command)
	v.mu.Unlock()
}

// ForgetAll drops every cached command result.
func (v *Values) ForgetAll() {
	v.mu.Lock()
	clear(v.entries)
	v.mu.Unlock()
}
