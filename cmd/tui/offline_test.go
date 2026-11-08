package main

import (
	"bytes"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingTransport fails every request and counts it, so a test sees a model
// refresh without a network call.
type countingTransport struct{ n atomic.Int32 }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, http.ErrHandlerTimeout
}

func TestOfflineTurnsOffTheModelRefresh(t *testing.T) {
	on := runDependencies{getenv: func(string) string { return "" }, modelRefresh: true}
	require.True(t, modelRefreshOn(on))
	off := offlineDeps(on)
	assert.False(t, modelRefreshOn(off))
	assert.Equal(t, "1", off.getenv(disableModelRefreshEnv))
	assert.Empty(t, off.getenv("ASK_HOME"), "other variables are unchanged")
	params, err := acpParams(off, "")
	require.NoError(t, err)
	assert.False(t, params.ModelRefresh.Enabled)
}

func TestACPCommandAcceptsOffline(t *testing.T) {
	args, err := parseACPArgs([]string{"--offline", "-a"})
	require.NoError(t, err)
	assert.True(t, args.offline)
	assert.Equal(t, "approve", string(args.trust))
	_, err = parseACPArgs([]string{"--models", "a"})
	require.Error(t, err, "the scope of ask acp comes per session")
	assert.Contains(t, acpUsage, "--offline")

	// The real entry: the input ends at once, and the refresh never starts.
	home := privateHome(t)
	models := &countingTransport{}
	var out, errb bytes.Buffer
	deps := runDependencies{
		getenv:       func(k string) string { return map[string]string{"ASK_HOME": home}[k] },
		modelRefresh: true, modelHTTP: &http.Client{Transport: models},
	}
	code := runWithDependencies([]string{"acp", "--offline"}, strings.NewReader(""), &out, &errb, deps)
	assert.Zero(t, code, errb.String())
	assert.Zero(t, models.n.Load(), "no model list request")
}

func TestLeaderAcceptsOffline(t *testing.T) {
	o, err := parseLeaderArgs([]string{"--offline"})
	require.NoError(t, err)
	assert.Equal(t, leaderOptions{offline: true}, o)
	o, err = parseLeaderArgs([]string{"--offline", "--spawned-by-client"})
	require.NoError(t, err)
	assert.True(t, o.offline && o.spawned)
	for _, argv := range [][]string{{"status", "--offline"}, {"stop", "--offline"}} {
		_, err := parseLeaderArgs(argv)
		require.Error(t, err, "argv %v", argv)
	}
	assert.Contains(t, leaderUsage, "--offline")
}

func TestTUIStartsAnOfflineLeader(t *testing.T) {
	environ := func() []string { return []string{"A=1", disableModelRefreshEnv + "=0"} }
	assert.Nil(t, spawnLeaderEnv(false, environ), "the leader inherits this environment")
	env := spawnLeaderEnv(true, environ)
	assert.Equal(t, disableModelRefreshEnv+"=1", env[len(env)-1], "the last value wins in the child")
	assert.Contains(t, env, "A=1")
}
