package sandbox

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

// secretNamePattern matches the names of variables that can hold a credential.
// It is a name filter only: a secret with an unusual name passes, and a name
// such as MY_KEYRING_PATH is dropped although it holds no secret.
const secretNamePattern = `(?i)(KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL)`

// askVarPrefix marks the variables of the harness. A child gets only the
// session variables that the caller passes, never the ones of the leader.
const askVarPrefix = "ASK_"

var secretName = regexp.MustCompile(secretNamePattern)

// nonInteractiveEnv keeps a command from waiting for a person: no pager, no
// editor, no color, no prompt.
var nonInteractiveEnv = map[string]string{
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

// ChildEnv builds the environment of a command from base (KEY=VALUE entries).
// It drops credential-shaped names and every ASK_* name, sets the
// non-interactive variables, and then sets the session variables. The result
// is sorted by name and has no repeated name; an entry without "=" is dropped.
func ChildEnv(base []string, session map[string]string) []string {
	env := make(map[string]string, len(base)+len(nonInteractiveEnv)+len(session))
	for _, kv := range base {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		if strings.HasPrefix(name, askVarPrefix) || secretName.MatchString(name) {
			continue
		}
		env[name] = value
	}
	maps.Copy(env, nonInteractiveEnv)
	maps.Copy(env, session)

	names := slices.Sorted(maps.Keys(env))
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, name+"="+env[name])
	}
	return out
}
