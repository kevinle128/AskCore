package leader

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func fields(t *testing.T, msg []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(msg, &m))
	return m
}

func request(id, method string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":%q,"params":{"k":"v"}}`, id, method))
}

func response(id json.RawMessage) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"ok":true}}`, id))
}

func mustForward(t *testing.T, tb *IDTable, client string, msg json.RawMessage) Outcome {
	t.Helper()
	out, err := tb.Forward(client, msg)
	require.NoError(t, err)
	return out
}

func TestIDRoundTripExact(t *testing.T) {
	ids := []string{`7`, `"7"`, `9007199254740993`, `"a:b|c"`, `"é"`, `"é"`, `-0`, `1e3`, `"<>&"`, `0.5`, `""`}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			tb := NewIDTable()
			out := mustForward(t, tb, "c1", request(id, "session/new"))
			require.Equal(t, KindRequest, out.Kind)
			require.Equal(t, "session/new", out.Method)
			internal := fields(t, out.Msg)["id"]
			require.NotEqual(t, id, string(internal))
			require.Equal(t, byte('"'), internal[0], "the internal id is always a JSON string")
			require.Equal(t, `{"k":"v"}`, string(fields(t, out.Msg)["params"]))

			got, ok := tb.Restore(response(internal))
			require.True(t, ok)
			require.Equal(t, "c1", got.ClientID)
			require.False(t, got.Dropped)
			require.Equal(t, "session/new", got.Method)
			require.Equal(t, id, string(fields(t, got.Msg)["id"]), "the original id bytes come back exact")
			require.Zero(t, tb.Pending("c1"))

			_, ok = tb.Restore(response(internal))
			require.False(t, ok, "a route answers once")
		})
	}
}

func TestIDRejectsInvalidIDTypes(t *testing.T) {
	for _, id := range []string{`null`, `true`, `false`, `[]`, `{}`, `[1]`} {
		_, err := NewIDTable().Forward("c1", request(id, "m"))
		require.ErrorIs(t, err, ErrInvalidID, id)
	}
	_, err := NewIDTable().Forward("c1", json.RawMessage(`[{"id":1,"method":"m"}]`))
	require.ErrorIs(t, err, ErrInvalidMessage, "a batch is refused")
	_, err = NewIDTable().Forward("c1", json.RawMessage(`{"jsonrpc":"2.0"`))
	require.ErrorIs(t, err, ErrInvalidMessage)
}

func TestIDEqualAcrossClients(t *testing.T) {
	tb := NewIDTable()
	a := mustForward(t, tb, "A", request(`1`, "m"))
	b := mustForward(t, tb, "B", request(`1`, "m"))
	ai, bi := fields(t, a.Msg)["id"], fields(t, b.Msg)["id"]
	require.NotEqual(t, string(ai), string(bi))

	got, ok := tb.Restore(response(bi))
	require.True(t, ok)
	require.Equal(t, "B", got.ClientID)
	got, ok = tb.Restore(response(ai))
	require.True(t, ok)
	require.Equal(t, "A", got.ClientID)
}

func TestIDDuplicateActiveRejected(t *testing.T) {
	pairs := [][2]string{{`1`, `1`}, {`1`, `1e0`}, {`1`, `1.0`}, {`0`, `-0`}, {`"a"`, `"a"`}, {`"a"`, `"a"`}, {`100`, `1e2`}}
	for _, p := range pairs {
		tb := NewIDTable()
		first := mustForward(t, tb, "A", request(p[0], "m"))
		_, err := tb.Forward("A", request(p[1], "m"))
		require.ErrorIs(t, err, ErrDuplicateID, "%s then %s", p[0], p[1])

		_, err = tb.Forward("B", request(p[1], "m"))
		require.NoError(t, err, "another client may use the same id")

		_, ok := tb.Restore(response(fields(t, first.Msg)["id"]))
		require.True(t, ok)
		_, err = tb.Forward("A", request(p[1], "m"))
		require.NoError(t, err, "an answered id can be used again")
	}
	tb := NewIDTable()
	mustForward(t, tb, "A", request(`1`, "m"))
	_, err := tb.Forward("A", request(`"1"`, "m"))
	require.NoError(t, err, "number 1 and string \"1\" are different ids")
}

func cancel(requestID string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":%s}}`, requestID))
}

func TestCancelRequestScoped(t *testing.T) {
	tb := NewIDTable()
	a := mustForward(t, tb, "A", request(`1`, "session/prompt"))
	b := mustForward(t, tb, "B", request(`2`, "session/prompt"))
	aInternal, bInternal := fields(t, a.Msg)["id"], fields(t, b.Msg)["id"]

	out := mustForward(t, tb, "A", cancel(`1.0`))
	require.Equal(t, KindNotification, out.Kind)
	require.False(t, out.Drop)
	var params struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	require.NoError(t, json.Unmarshal(fields(t, out.Msg)["params"], &params))
	require.Equal(t, string(aInternal), string(params.RequestID))

	require.True(t, mustForward(t, tb, "A", cancel(`2`)).Drop, "A cannot name B's original id")
	require.True(t, mustForward(t, tb, "A", cancel(string(bInternal))).Drop, "A cannot name B's internal id")
	require.True(t, mustForward(t, tb, "A", cancel(string(aInternal))).Drop, "a client never sees internal ids")
	require.True(t, mustForward(t, tb, "A", cancel(`99`)).Drop)
	require.True(t, mustForward(t, tb, "A", json.RawMessage(`{"jsonrpc":"2.0","method":"$/cancel_request","params":[]}`)).Drop)
	require.True(t, mustForward(t, tb, "A", json.RawMessage(`{"jsonrpc":"2.0","method":"$/cancel_request"}`)).Drop)
}

func TestIDNotificationAndResponsePassThrough(t *testing.T) {
	tb := NewIDTable()
	note := json.RawMessage(`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"s1"}}`)
	out := mustForward(t, tb, "A", note)
	require.Equal(t, KindNotification, out.Kind)
	require.False(t, out.Drop)
	require.JSONEq(t, string(note), string(out.Msg))

	resp := response(json.RawMessage(`"agent-7"`))
	out = mustForward(t, tb, "A", resp)
	require.Equal(t, KindResponse, out.Kind)
	require.JSONEq(t, string(resp), string(out.Msg), "a reverse response is not rewritten here")
	require.Zero(t, tb.Pending("A"))
}

func TestIDDropClient(t *testing.T) {
	tb := NewIDTable()
	a := mustForward(t, tb, "A", request(`1`, "session/new"))
	mustForward(t, tb, "B", request(`1`, "m"))
	tb.DropClient("A")
	require.Zero(t, tb.Pending("A"))
	require.Equal(t, 1, tb.Pending("B"))

	got, ok := tb.Restore(response(fields(t, a.Msg)["id"]))
	require.True(t, ok)
	require.True(t, got.Dropped, "the late result is marked so the router can release what it created")
	require.Equal(t, "session/new", got.Method)
	require.Equal(t, "A", got.ClientID)

	_, err := tb.Forward("A", request(`1`, "m"))
	require.NoError(t, err, "a dropped client's ids are free")
}

func TestIDRestoreIgnoresUnknownAndRequests(t *testing.T) {
	tb := NewIDTable()
	_, ok := tb.Restore(response(json.RawMessage(`"nobody:1"`)))
	require.False(t, ok)
	_, ok = tb.Restore(response(json.RawMessage(`5`)))
	require.False(t, ok)
	out := mustForward(t, tb, "A", request(`1`, "m"))
	_, ok = tb.Restore(request(string(fields(t, out.Msg)["id"]), "fs/read_text_file"))
	require.False(t, ok, "a message with a method is not a response")
}

func TestIDHugeExponentDoesNotCostMemory(t *testing.T) {
	tb := NewIDTable()
	mustForward(t, tb, "A", request(`1e999999999999`, "m"))
	_, err := tb.Forward("A", request(`1e999999999999`, "m"))
	require.ErrorIs(t, err, ErrDuplicateID)
}

func TestIDRejectsUnsafeClientID(t *testing.T) {
	for _, bad := range []string{"", "a<b", "a&b", `a"b`, "a:b", "a b"} {
		_, err := NewIDTable().Forward(bad, request(`1`, "m"))
		require.ErrorIs(t, err, ErrInvalidClientID, "%q", bad)
	}
	_, err := NewIDTable().Forward("c-1_A", request(`1`, "m"))
	require.NoError(t, err)
}

func TestIDHugeExponentKeepsTheSign(t *testing.T) {
	tb := NewIDTable()
	mustForward(t, tb, "A", request(`-1e99999999999`, "m"))
	_, err := tb.Forward("A", request(`1e99999999999`, "m"))
	require.NoError(t, err, "a negative and a positive number are not the same id")
	_, err = tb.Forward("A", request(`-1e99999999999`, "m"))
	require.ErrorIs(t, err, ErrDuplicateID)
}

// The SDK reads requestId without regard to case. A cancel that holds a second
// spelling of the key could name the request of another client.
func TestCancelRequestRefusesKeysThatDifferInCase(t *testing.T) {
	tb := NewIDTable()
	a := mustForward(t, tb, "A", request(`1`, "m"))
	b := mustForward(t, tb, "B", request(`1`, "m"))
	aInternal, bInternal := fields(t, a.Msg)["id"], fields(t, b.Msg)["id"]
	forged := json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":1,"requestid":%s}}`, bInternal))
	require.True(t, mustForward(t, tb, "A", forged).Drop, "A cannot name the request of B with a second key")
	same := json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":1,"REQUESTID":%s}}`, aInternal))
	require.True(t, mustForward(t, tb, "A", same).Drop, "even a key that names its own request is refused")
	require.False(t, mustForward(t, tb, "A", cancel(`1`)).Drop, "the honest cancel works")
}
