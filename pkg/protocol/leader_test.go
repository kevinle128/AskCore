package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLeaderFrameJSONNames(t *testing.T) {
	for typ, want := range map[string]string{
		LeaderFrameRegister:     "register",
		LeaderFrameRegistered:   "registered",
		LeaderFrameReady:        "leader_ready",
		LeaderFrameACP:          "acp",
		LeaderFrameControl:      "control",
		LeaderFrameControlReply: "control_result",
		LeaderFramePing:         "ping",
		LeaderFramePong:         "pong",
		LeaderFrameDisconnect:   "disconnect",
		LeaderFrameError:        "error",
	} {
		require.Equal(t, want, typ)
	}
	require.Equal(t, 1, LeaderProtocolVersion)
	require.Equal(t, 64<<20, LeaderMaxFrame)

	raw, err := json.Marshal(LeaderFrame{Type: LeaderFrameACP, Payload: json.RawMessage(`{"id":1}`)})
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"acp","payload":{"id":1}}`, string(raw))

	raw, err = json.Marshal(LeaderRegister{ClientKind: "connect", ProtocolVersion: 1, Build: "dev"})
	require.NoError(t, err)
	require.JSONEq(t, `{"clientKind":"connect","protocolVersion":1,"build":"dev"}`, string(raw))

	raw, err = json.Marshal(LeaderRegistered{ClientID: "c1", InstanceID: "i1", Build: "dev", ProtocolVersion: 1, Ready: true, Controls: []string{"status"}})
	require.NoError(t, err)
	require.JSONEq(t, `{"clientId":"c1","instanceId":"i1","build":"dev","protocolVersion":1,"ready":true,"controls":["status"]}`, string(raw))

	raw, err = json.Marshal(LeaderError{Kind: LeaderErrVersionMismatch, Message: "m", Upgrade: LeaderUpgradeClient})
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"leader_version_mismatch","message":"m","upgrade":"client"}`, string(raw))

	raw, err = json.Marshal(LeaderStatus{InstanceID: "i", Build: "b", ProtocolVersion: 1, PID: 2, Clients: 3, Sessions: 4, ActiveRuns: 5, SpawnedByClient: true})
	require.NoError(t, err)
	require.JSONEq(t, `{"instanceId":"i","build":"b","protocolVersion":1,"pid":2,"clients":3,"sessions":4,"activeRuns":5,"spawnedByClient":true}`, string(raw))
}

func TestACPRouteMetaDriverGenIsDecimalText(t *testing.T) {
	in := ACPRouteMeta{ClientID: "c1", DriverGen: 9007199254740993, LiveDriver: true}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, `{"clientId":"c1","driverGen":"9007199254740993","liveDriver":true}`, string(raw))
	var out ACPRouteMeta
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, in, out)
	require.Equal(t, "ask.dev/route", ACPRouteMetaKey)
	require.Equal(t, -32015, ACPCodeNotDriver)
	require.Equal(t, -32016, ACPCodeNotInitializedClient)
	require.Equal(t, "_ask/session/take", ACPTake)
}

func TestLeaderRoutingTypes(t *testing.T) {
	require.Equal(t, -32015, ACPErrorCode(ACPErrNotDriver))
	require.Equal(t, -32016, ACPErrorCode(ACPErrClientNotInitialized))
	require.NotEqual(t, ACPErrorMessage(ACPErrNotDriver), ACPErrorMessage(ACPErrInternal))
	require.NotEqual(t, ACPErrorMessage(ACPErrClientNotInitialized), ACPErrorMessage(ACPErrInternal))
	require.NotEqual(t, ACPErrorMessage(ACPErrNotDriver), ACPErrorMessage(ACPErrClientNotInitialized))

	raw, err := json.Marshal(ACPTakeResult{SessionID: "s1", DriverGen: 9007199254740993})
	require.NoError(t, err)
	require.JSONEq(t, `{"sessionId":"s1","driverGen":"9007199254740993"}`, string(raw))

	raw, err = json.Marshal(ACPAttachResult{SessionID: "s1", Role: ACPRoleObserver})
	require.NoError(t, err)
	require.JSONEq(t, `{"sessionId":"s1","role":"observer"}`, string(raw))

	raw, err = json.Marshal(ACPListLiveResult{Sessions: []ACPLiveSession{{SessionID: "s1", Cwd: "/w", Role: ACPRoleDriver, HasDriver: true, Subscribers: 2}}})
	require.NoError(t, err)
	require.JSONEq(t, `{"sessions":[{"sessionId":"s1","cwd":"/w","role":"driver","hasDriver":true,"subscribers":2}]}`, string(raw))
}
