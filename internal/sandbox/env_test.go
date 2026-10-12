package sandbox

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envMap turns a KEY=VALUE list into a map and fails on a duplicate name.
func envMap(t *testing.T, env []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(env))
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		require.True(t, ok, "entry without '=': %q", kv)
		_, dup := out[name]
		require.False(t, dup, "duplicate name %q", name)
		out[name] = value
	}
	return out
}

func TestChildEnvDropsCredentialShapedNames(t *testing.T) {
	base := []string{
		"OPENAI_API_KEY=sk-test",
		"GITHUB_TOKEN=ghp-test",
		"AWS_SECRET_ACCESS_KEY=aws-test",
		"DB_PASSWORD=pw",
		"PGPASSWD=pw",
		"GOOGLE_APPLICATION_CREDENTIALS=/tmp/cred.json",
		"npm_config__authToken=npm-test",
		// A documented false positive: the name has KEY, so it is dropped.
		"MY_KEYRING_PATH=/tmp/ring",
	}
	env := envMap(t, ChildEnv(base, nil))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		assert.NotContains(t, env, name)
	}
}

func TestChildEnvKeepsOrdinaryNames(t *testing.T) {
	base := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/test",
		"LANG=en_US.UTF-8",
		"GOPATH=/go",
		"SSH_AUTH_SOCK=/tmp/agent.sock",
		"USER=test",
	}
	env := envMap(t, ChildEnv(base, nil))
	for _, kv := range base {
		name, value, _ := strings.Cut(kv, "=")
		assert.Equal(t, value, env[name], name)
	}
}

func TestChildEnvReplacesInheritedAskVars(t *testing.T) {
	base := []string{
		"ASK_HOME=/home/test/.ask",
		"ASK_SESSION_ID=old",
		"ASK_COMMAND_GUARD=1",
		"PATH=/bin",
	}
	session := map[string]string{
		"ASK_SESSION_ID": "new",
		"ASK_MODEL":      "m1",
	}
	env := envMap(t, ChildEnv(base, session))
	assert.NotContains(t, env, "ASK_HOME")
	assert.NotContains(t, env, "ASK_COMMAND_GUARD")
	assert.Equal(t, "new", env["ASK_SESSION_ID"])
	assert.Equal(t, "m1", env["ASK_MODEL"])
	assert.Equal(t, "/bin", env["PATH"])
}

func TestChildEnvAddsNonInteractiveVars(t *testing.T) {
	env := envMap(t, ChildEnv([]string{"TERM=xterm-256color", "PAGER=less", "EDITOR=vim"}, nil))
	want := map[string]string{
		"TERM":                "dumb",
		"NO_COLOR":            "1",
		"CI":                  "true",
		"PAGER":               "cat",
		"GIT_PAGER":           "cat",
		"MANPAGER":            "cat",
		"GIT_TERMINAL_PROMPT": "0",
		"EDITOR":              "true",
		"VISUAL":              "true",
		"GIT_EDITOR":          "true",
		"SSH_ASKPASS":         "",
	}
	for name, value := range want {
		got, ok := env[name]
		assert.True(t, ok, "%s missing", name)
		assert.Equal(t, value, got, name)
	}
}

func TestChildEnvIsSortedAndSkipsMalformedEntries(t *testing.T) {
	out := ChildEnv([]string{"ZED=1", "noequals", "=empty", "ALPHA=2", "ZED=3"}, nil)
	names := make([]string, 0, len(out))
	for _, kv := range out {
		name, _, _ := strings.Cut(kv, "=")
		names = append(names, name)
	}
	assert.IsIncreasing(t, names)
	env := envMap(t, out)
	assert.Equal(t, "3", env["ZED"], "the last value of a repeated name wins")
	assert.NotContains(t, env, "noequals")
	assert.NotContains(t, env, "")
}
