package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wireRoundTrip[T any](t *testing.T, in T) (T, string) {
	t.Helper()
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(b, &out))
	return out, string(b)
}

func TestProviderShapesRoundTrip(t *testing.T) {
	list := ACPProvidersResult{
		Generation: 18446744073709551615,
		Options: []ACPLoginOption{
			{ProviderID: "anthropic", MethodID: "api_key", Name: "Anthropic", State: ACPLoginStateEnv, EnvVar: "ANTHROPIC_API_KEY"},
			{ProviderID: "openai", MethodID: "oauth", Name: "OpenAI", State: ACPLoginStateNone},
		},
		Providers: []ACPProviderStatus{{ProviderID: "anthropic", Usable: true, ModelCount: 3}},
	}
	got, raw := wireRoundTrip(t, list)
	assert.Equal(t, list, got)
	assert.Contains(t, raw, `"generation":"18446744073709551615"`, "counters are decimal text")

	assert.Contains(t, ACPProvidersList, "_ask/providers/")

	start := ACPLoginStartResult{LoginID: "l1", Kind: ACPLoginKindDevice, URL: "https://example.test/device", UserCode: "AB-CD", ExpiresAt: "2026-10-10T10:00:00Z"}
	gotStart, _ := wireRoundTrip(t, start)
	assert.Equal(t, start, gotStart)

	for _, in := range []any{
		ACPAPIKeyRequest{LoginID: "l1", ProviderID: "openai", Key: "k"},
		ACPLoginStartRequest{ProviderID: "openai", MethodID: "oauth"},
		ACPLoginInputRequest{LoginID: "l1", Code: "c"},
		ACPLoginIDRequest{LoginID: "l1"},
		ACPLoginCancelResult{Cancelled: true},
		ACPLogoutRequest{ProviderID: "openai", MethodID: "oauth", SessionID: "s1"},
		ACPLogoutResult{Removed: true, EnvVar: "OPENAI_API_KEY", ClearedModel: "openai/gpt@openai-responses", ClearPending: true, Generation: 7},
		ACPProvidersChangedNotification{Generation: 9, ProviderID: "openai", Change: ACPProviderChangeRemoved},
		ACPLoginOutcome{LoginID: "l1", Status: ACPLoginStatusRefused, Reason: ACPLoginReasonAccessDenied, HTTPStatus: 403, ProviderCode: "forbidden", Account: "a@example.test", Generation: 4},
	} {
		b, err := json.Marshal(in)
		require.NoError(t, err)
		ptr := newOfSameType(in)
		require.NoError(t, json.Unmarshal(b, ptr))
		again, err := json.Marshal(ptr)
		require.NoError(t, err)
		assert.JSONEq(t, string(b), string(again))
	}

	b, err := json.Marshal(ACPLogoutResult{Generation: 12})
	require.NoError(t, err)
	assert.JSONEq(t, `{"removed":false,"generation":"12"}`, string(b), "optional fields are omitted")
}

func newOfSameType(v any) any {
	switch v.(type) {
	case ACPAPIKeyRequest:
		return &ACPAPIKeyRequest{}
	case ACPLoginStartRequest:
		return &ACPLoginStartRequest{}
	case ACPLoginInputRequest:
		return &ACPLoginInputRequest{}
	case ACPLoginIDRequest:
		return &ACPLoginIDRequest{}
	case ACPLoginCancelResult:
		return &ACPLoginCancelResult{}
	case ACPLogoutRequest:
		return &ACPLogoutRequest{}
	case ACPLogoutResult:
		return &ACPLogoutResult{}
	case ACPProvidersChangedNotification:
		return &ACPProvidersChangedNotification{}
	case ACPLoginOutcome:
		return &ACPLoginOutcome{}
	}
	panic("unknown shape")
}

func TestModelsRequestKeepsSessionKey(t *testing.T) {
	var req ACPModelsRequest
	require.NoError(t, json.Unmarshal([]byte(`{"sessionId":"s1"}`), &req))
	assert.Equal(t, "s1", req.SessionID)
	assert.False(t, req.UsableOnly)

	b, err := json.Marshal(ACPModelsRequest{SessionID: "s1", UsableOnly: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"sessionId":"s1","usableOnly":true}`, string(b))

	// The same body decodes as the old request: the session key did not move.
	var old ACPSessionRequest
	require.NoError(t, json.Unmarshal(b, &old))
	assert.Equal(t, "s1", old.SessionID)
}

func TestModelsResultOmitsEchoForOldLeader(t *testing.T) {
	b, err := json.Marshal(ACPModelsResult{Current: "m", Models: []ACPModel{}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"current":"m","models":[]}`, string(b))

	var old ACPModelsResult
	require.NoError(t, json.Unmarshal([]byte(`{"current":"m","models":[]}`), &old))
	assert.False(t, old.UsableOnly)

	b, err = json.Marshal(ACPModelsResult{Current: "m", Models: []ACPModel{}, UsableOnly: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"current":"m","models":[],"usableOnly":true}`, string(b))
}

func TestNewErrorKindsHaveFixedCodesAndTexts(t *testing.T) {
	assert.Equal(t, -32018, ACPErrorCode(ACPErrNoModel))
	assert.Equal(t, -32019, ACPErrorCode(ACPErrProviderBusy))
	assert.Equal(t, ACPErrorKind("no_model"), ACPErrNoModel)
	assert.Equal(t, ACPErrorKind("provider_busy"), ACPErrProviderBusy)
	assert.Equal(t, "the session has no model; choose one first", ACPErrorMessage(ACPErrNoModel))
	assert.Equal(t, "another client is signing in to this provider", ACPErrorMessage(ACPErrProviderBusy))
	assert.NotEqual(t, ACPErrorMessage(ACPErrNoModel), ACPErrorMessage(ACPErrProviderBusy))
}

func TestSetModelResultReportsDefaultSaved(t *testing.T) {
	b, err := json.Marshal(ACPModelRequest{SessionID: "s1", ModelID: "m", SaveDefault: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"sessionId":"s1","modelId":"m","saveDefault":true}`, string(b))
	b, err = json.Marshal(ACPModelRequest{SessionID: "s1", ModelID: "m"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"sessionId":"s1","modelId":"m"}`, string(b))

	b, err = json.Marshal(ACPSetModelResult{ModelID: "m", ThinkingLevel: ThinkingOff, DefaultSaved: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"modelId":"m","thinkingLevel":"off","defaultSaved":true}`, string(b))
	b, err = json.Marshal(ACPSetModelResult{ModelID: "m", ThinkingLevel: ThinkingOff})
	require.NoError(t, err)
	assert.JSONEq(t, `{"modelId":"m","thinkingLevel":"off"}`, string(b))

	var old ACPSetModelResult
	require.NoError(t, json.Unmarshal([]byte(`{"modelId":"m","thinkingLevel":"off"}`), &old))
	assert.False(t, old.DefaultSaved)
	var oldReq ACPModelRequest
	require.NoError(t, json.Unmarshal([]byte(`{"sessionId":"s1","modelId":"m"}`), &oldReq))
	assert.False(t, oldReq.SaveDefault)
}

func TestModelsResultCarriesSavedDefault(t *testing.T) {
	got, raw := wireRoundTrip(t, ACPModelsResult{Current: "a", Models: []ACPModel{}, Default: "b"})
	assert.Equal(t, "b", got.Default)
	assert.Contains(t, raw, `"default":"b"`)

	b, err := json.Marshal(ACPModelsResult{Current: "a", Models: []ACPModel{}})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "default")
}

func TestStateCarriesModelFallback(t *testing.T) {
	st := ACPStateResult{SessionID: "s", Epoch: "e", ModelID: "m", ThinkingLevel: ThinkingOff, Steering: []string{}, FollowUp: []string{},
		ModelFallback: &ACPModelFallback{SavedDefault: "old", Reason: ACPFallbackNoCredential}}
	got, raw := wireRoundTrip(t, st)
	require.NotNil(t, got.ModelFallback)
	assert.Equal(t, ACPModelFallback{SavedDefault: "old", Reason: "no_credential"}, *got.ModelFallback)
	assert.Contains(t, raw, `"modelFallback":{"savedDefault":"old","reason":"no_credential"}`)
	assert.Equal(t, ACPFallbackReason("not_in_catalog"), ACPFallbackNotInCatalog)

	var old ACPStateResult
	require.NoError(t, json.Unmarshal([]byte(`{"sessionId":"s","epoch":"e","running":false,"modelId":"m","thinkingLevel":"off","messageCount":0,"steering":[],"followUp":[]}`), &old))
	assert.Nil(t, old.ModelFallback)
	b, err := json.Marshal(old)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "modelFallback")
}

func TestOutcomeReasonRemoved(t *testing.T) {
	for _, r := range []ACPLoginReason{ACPLoginReasonInvalidKey, ACPLoginReasonAccessDenied, ACPLoginReasonExpired, ACPLoginReasonNetwork,
		ACPLoginReasonCancelled, ACPLoginReasonRemoved, ACPLoginReasonBusy, ACPLoginReasonInternal} {
		assert.True(t, r.Valid(), r)
	}
	assert.Equal(t, ACPLoginReason("removed"), ACPLoginReasonRemoved)
	assert.False(t, ACPLoginReason("provider said: nope").Valid())
	assert.False(t, ACPLoginReason("").Valid())

	got, raw := wireRoundTrip(t, ACPLoginOutcome{LoginID: "l", Status: ACPLoginStatusCancelled, Reason: ACPLoginReasonRemoved})
	assert.Equal(t, ACPLoginReasonRemoved, got.Reason)
	assert.Contains(t, raw, `"reason":"removed"`)
}

func TestOutcomeNotCheckedFlag(t *testing.T) {
	got, raw := wireRoundTrip(t, ACPLoginOutcome{LoginID: "l", Status: ACPLoginStatusSignedIn, NotChecked: true, Generation: 2})
	assert.True(t, got.NotChecked)
	assert.Contains(t, raw, `"notChecked":true`)

	b, err := json.Marshal(ACPLoginOutcome{LoginID: "l", Status: ACPLoginStatusSignedIn, Generation: 2})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "notChecked")

	var old ACPLoginOutcome
	require.NoError(t, json.Unmarshal([]byte(`{"loginId":"l","status":"signed_in","generation":"2"}`), &old))
	assert.False(t, old.NotChecked, "an outcome without the flag decodes as checked")
}
