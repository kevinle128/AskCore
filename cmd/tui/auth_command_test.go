package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type authCommandRT func(*http.Request) (*http.Response, error)

func (f authCommandRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The helper runs the same command composition as the production wrapper.
func TestAuthCommandSubprocessHelper(t *testing.T) {
	if os.Getenv("ASK_AUTH_COMMAND_HELPER") != "1" {
		return
	}
	index := 0
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index == 0 {
		os.Exit(2)
	}
	if os.Getenv("ASK_AUTH_EXTERNAL_SERVER") != "" {
		os.Exit(runOAuthCommandHelper(os.Args[index:]))
	}
	external := authCommandRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.anthropic.com/v1/messages" {
			return nil, fmt.Errorf("unexpected inference destination")
		}
		if r.Header.Get("X-Api-Key") != "test-private-key" {
			return nil, fmt.Errorf("wrong inference credential")
		}
		body := `event: message_start
 data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}

 event: content_block_start
 data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

 event: content_block_delta
 data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"connected"}}

 event: content_block_stop
 data: {"type":"content_block_stop","index":0}

 event: message_delta
 data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

 event: message_stop
 data: {"type":"message_stop"}

`
		body = strings.ReplaceAll(body, "\n ", "\n")
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	private := authCommandRT(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("key command must not use auth HTTP")
	})
	code := runWithDependencies(os.Args[index:], os.Stdin, os.Stdout, os.Stderr, runDependencies{getenv: os.Getenv, authHTTP: &http.Client{Transport: private}, inferenceHTTP: &http.Client{Transport: external}})
	os.Exit(code)
}

func authCommand(t *testing.T, home, input string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestAuthCommandSubprocessHelper$", "--"}, args...)...)
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "ASK_HOME=") && !strings.HasPrefix(v, "ASK_AUTH_COMMAND_HELPER=") && !strings.HasPrefix(v, "ANTHROPIC_API_KEY=") {
			env = append(env, v)
		}
	}
	cmd.Env = append(env, "ASK_AUTH_COMMAND_HELPER=1", "ASK_HOME="+home, "ANTHROPIC_API_KEY=")
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestAuthKeyCommandNextPromptAndLogout(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask")
	out, errout, err := authCommand(t, home, "test-private-key\n", "auth", "login", "--provider", "anthropic", "--method", "api-key")
	if err != nil {
		t.Fatalf("login: %v %s", err, errout)
	}
	if strings.Contains(out+errout, "test-private-key") {
		t.Fatal("private input leaked")
	}
	out, errout, err = authCommand(t, home, "", "--provider", "anthropic", "-p", "hello")
	if err != nil || !strings.Contains(out, "connected") {
		t.Fatalf("next prompt: %v stdout=%s stderr=%s", err, out, errout)
	}
	_, errout, err = authCommand(t, home, "", "auth", "logout", "--provider", "anthropic")
	if err != nil {
		t.Fatalf("logout: %v %s", err, errout)
	}
	_, _, err = authCommand(t, home, "", "--provider", "anthropic", "-p", "hello")
	if err == nil {
		t.Fatal("logged-out prompt used old credential")
	}
}

func TestAuthCommandMissingMethodAndOversizeRetainRecord(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask")
	_, _, err := authCommand(t, home, "test-private-key\n", "auth", "login", "--provider", "anthropic", "--method", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		args  []string
	}{
		{"", []string{"auth", "login", "--provider", "anthropic"}},
		{strings.Repeat("a", authInputLimit+1), []string{"auth", "login", "--provider", "anthropic", "--method", "api-key"}},
	} {
		_, _, err := authCommand(t, home, tc.input, tc.args...)
		if err == nil {
			t.Fatal("invalid command succeeded")
		}
	}
	after, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed command replaced saved credential")
	}
}
