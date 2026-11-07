package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrustWireRoundTrip(t *testing.T) {
	assert.Equal(t, "_ask/trust/state", ACPTrustState)
	assert.Equal(t, "_ask/trust/set", ACPTrustSet)
	assert.Equal(t, "ask.dev/trust", ACPTrustMetaKey)
	assert.Equal(t, ACPTrustOverride("approve"), ACPTrustApprove)
	assert.Equal(t, ACPTrustOverride("deny"), ACPTrustDeny)

	state := ACPTrustStateResult{
		Root: "/p", Trusted: false, Source: "prompt", NeedsDecision: true,
		Gated: []string{".ask/settings.json"}, EntryPath: "/", Warning: "trust store unsafe: /h/trust.json",
	}
	got, raw := wireRoundTrip(t, state)
	assert.Equal(t, state, got)
	assert.JSONEq(t, `{"root":"/p","trusted":false,"source":"prompt","needsDecision":true,"gated":[".ask/settings.json"],"entryPath":"/","warning":"trust store unsafe: /h/trust.json"}`, raw)

	_, raw = wireRoundTrip(t, ACPTrustStateResult{Root: "/p", Trusted: true, Source: "no_gated_files"})
	assert.JSONEq(t, `{"root":"/p","trusted":true,"source":"no_gated_files"}`, raw, "optional fields are omitted when empty")

	set := ACPTrustSetRequest{Cwd: "/p", Target: ACPTrustTargetParent, Decision: ACPTrustDecisionTrust}
	gotSet, raw := wireRoundTrip(t, set)
	assert.Equal(t, set, gotSet)
	assert.JSONEq(t, `{"cwd":"/p","target":"parent","decision":"trust"}`, raw)
	assert.Equal(t, ACPTrustTarget("folder"), ACPTrustTargetFolder)
	assert.Equal(t, ACPTrustDecision("distrust"), ACPTrustDecisionDistrust)
	assert.Equal(t, ACPTrustDecision("clear"), ACPTrustDecisionClear)

	_, raw = wireRoundTrip(t, ACPTrustStateRequest{Cwd: "/p"})
	assert.JSONEq(t, `{"cwd":"/p"}`, raw)

	st := ACPStateResult{
		SessionID: "s", Epoch: "e", Steering: []string{}, FollowUp: []string{},
		Trust:        &ACPSessionTrust{Trusted: true, Source: "flag"},
		Diagnostics:  []ACPDiagnostic{{Layer: "project", Path: "/p/.ask/settings.json", Key: "env", Text: "ignored"}, {Layer: "user", Path: "/h/settings.json", Text: "bad"}},
		ThinkingFold: "collapsed",
	}
	gotState, raw := wireRoundTrip(t, st)
	assert.Equal(t, st, gotState)
	assert.Contains(t, raw, `"trust":{"trusted":true,"source":"flag"}`)
	assert.Contains(t, raw, `"thinkingFold":"collapsed"`)
	assert.Contains(t, raw, `{"layer":"user","path":"/h/settings.json","text":"bad"}`, "an empty key is omitted")

	// An old leader sends none of the new fields, and a new leader omits them
	// when they are empty.
	_, raw = wireRoundTrip(t, ACPStateResult{SessionID: "s"})
	for _, key := range []string{"trust", "diagnostics", "thinkingFold"} {
		assert.NotContains(t, raw, `"`+key+`"`)
	}
	var old ACPStateResult
	require.NoError(t, json.Unmarshal([]byte(`{"sessionId":"s","epoch":"e","running":false,"modelId":"","thinkingLevel":"","messageCount":0,"steering":[],"followUp":[]}`), &old))
	assert.Nil(t, old.Trust)

	res := ACPSetModelResult{ModelID: "a/b@c", DefaultNotSaved: ACPDefaultShadowed}
	gotRes, raw := wireRoundTrip(t, res)
	assert.Equal(t, res, gotRes)
	assert.Contains(t, raw, `"defaultNotSaved":"shadowed"`)
	assert.Equal(t, ACPDefaultNotSaved("unsafe_file"), ACPDefaultUnsafeFile)
	assert.Equal(t, ACPDefaultNotSaved("failed"), ACPDefaultFailed)
	_, raw = wireRoundTrip(t, ACPSetModelResult{ModelID: "a/b@c", DefaultSaved: true})
	assert.NotContains(t, raw, "defaultNotSaved")
}
