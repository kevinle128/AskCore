package protocol

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestACPWireCursorPrecision(t *testing.T) {
	want := ACPCursor{Epoch: "epoch", Seq: math.MaxUint64}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"epoch":"epoch","seq":"18446744073709551615"}` {
		t.Fatalf("cursor wire: %s", data)
	}
	var got ACPCursor
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v", got)
	}
	for _, bad := range []string{`{"epoch":"e","seq":9007199254740993}`, `{"epoch":"e","seq":"-1"}`, `{"epoch":"e","seq":"18446744073709551616"}`} {
		if err := json.Unmarshal([]byte(bad), &got); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestACPWireOpenArguments(t *testing.T) {
	want := ACPStreamBaseline{AttemptID: "attempt", Seq: 9007199254740993, Message: AssistantMessage{StopReason: StopPending, Content: []AssistantBlock{}}, Open: map[int][]byte{0: []byte(`{"unfinished":`)}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got ACPStreamBaseline
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Seq != want.Seq || string(got.Open[0]) != string(want.Open[0]) {
		t.Fatalf("lossy baseline: %s", data)
	}
}

func TestACPWireInputBlocks(t *testing.T) {
	var req ACPInputRequest
	raw := `{"sessionId":"s","content":[{"type":"text","text":"hi"},{"type":"image","data":"QUJD","mimeType":"image/png"}]}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	blocks, err := req.UserBlocks()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || blocks[0].(Text).Text != "hi" || blocks[1].(Image).MimeType != "image/png" {
		t.Fatalf("blocks: %#v", blocks)
	}
	for _, bad := range []string{
		`{"sessionId":"s","content":[]}`,
		`{"sessionId":"s","content":[{"type":"audio","data":"x"}]}`,
		`{"sessionId":"s","content":[{"type":"text"}]}`,
		`{"sessionId":"s","content":[{"type":"image","data":"x"}]}`,
	} {
		var r ACPInputRequest
		if err := json.Unmarshal([]byte(bad), &r); err != nil {
			t.Fatal(err)
		}
		if _, err := r.UserBlocks(); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestACPWireEventIdentity(t *testing.T) {
	want := ACPEventNotification{SubscriptionID: "sub", SessionID: "s", Epoch: "e", Seq: math.MaxUint64, RunID: "r", CycleID: "c", AttemptID: "a", Event: json.RawMessage(`{"type":"attempt_start"}`)}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got ACPEventNotification
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Seq != want.Seq || got.CycleID != "c" || got.AttemptID != "a" || got.RunID != "r" {
		t.Fatalf("identity lost: %s", data)
	}
	data, _ = json.Marshal(ACPEventNotification{Seq: 1, Event: json.RawMessage(`{}`)})
	if strings.Contains(string(data), "cycleId") || strings.Contains(string(data), "attemptId") {
		t.Fatalf("empty identity written: %s", data)
	}
}

func TestACPWireErrorCodes(t *testing.T) {
	kinds := []ACPErrorKind{ACPErrUnknownSession, ACPErrUnknownSubscription, ACPErrNotInitialized, ACPErrBusy, ACPErrDisposed, ACPErrNoAPIKey, ACPErrQueueFull, ACPErrInvalidModel, ACPErrInvalidContent, ACPErrCancelled, ACPErrUnsupported, ACPErrOutputFailure}
	seen := map[int]ACPErrorKind{}
	for _, k := range kinds {
		c := ACPErrorCode(k)
		if c >= 0 {
			t.Fatalf("%s: code %d is not an error code", k, c)
		}
		if prev, dup := seen[c]; dup && c != -32602 {
			t.Fatalf("%s and %s share code %d", prev, k, c)
		}
		seen[c] = k
	}
	if ACPErrorCode(ACPErrUnsupported) != -32601 || ACPErrorCode(ACPErrCancelled) != -32800 || ACPErrorCode("other") != -32603 {
		t.Fatal("standard code mapping changed")
	}
	// Ask-only codes must not collide with the codes that the pinned ACP schema defines.
	for _, c := range []int{ACPCodeBusy, ACPCodeDisposed, ACPCodeQueueFull, ACPCodeOutputFailure} {
		if c == -32000 || c == -32002 || c < -32099 {
			t.Fatalf("code %d outside the free reserved range", c)
		}
	}
	data, _ := json.Marshal(ACPErrorData{Kind: ACPErrBusy})
	if string(data) != `{"kind":"busy"}` {
		t.Fatalf("error data: %s", data)
	}
}

func TestACPWireResultsRoundTrip(t *testing.T) {
	one := int64(0)
	for _, v := range []any{
		ACPStateResult{SessionID: "s", Epoch: "e", Running: true, ModelID: "p/m", ThinkingLevel: ThinkingMinimal, MessageCount: 3, Steering: []string{"i1"}, FollowUp: []string{}},
		ACPModelsResult{Current: "p/m", Models: []ACPModel{{ModelID: "p/m", Provider: "p", ID: "m", API: "openai-completions", Input: []string{"text"}, ContextWindow: 10, MaxTokens: 5}}},
		ACPUsageResult{Attempts: []ACPUsageRow{{AttemptID: "a", Outcome: "completed", Usage: &Usage{Input: 1, Reasoning: &one}}, {AttemptID: "b", Outcome: "failed"}}, Complete: false},
		ACPResetResult{SessionID: "s", Epoch: "e2"},
		ACPContinueResult{StopReason: "end_turn"},
	} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		back := reflect.New(reflect.TypeOf(v))
		if err = json.Unmarshal(data, back.Interface()); err != nil {
			t.Fatal(err)
		}
		again, _ := json.Marshal(back.Elem().Interface())
		if string(again) != string(data) {
			t.Fatalf("round trip changed %T: %s -> %s", v, data, again)
		}
	}
}

func TestACPWireDeferredMethods(t *testing.T) {
	for _, m := range []string{ACPCompact, ACPFork, ACPTree} {
		if !strings.HasPrefix(m, "_ask/session/") {
			t.Fatalf("method %q is outside the Ask namespace", m)
		}
	}
}

func TestACPWireErrorMessages(t *testing.T) {
	kinds := []ACPErrorKind{ACPErrUnknownSession, ACPErrUnknownSubscription, ACPErrNotInitialized, ACPErrBusy, ACPErrDisposed, ACPErrNoAPIKey, ACPErrQueueFull, ACPErrInvalidModel, ACPErrInvalidContent, ACPErrInvalidParams, ACPErrInvalidState, ACPErrCancelled, ACPErrUnsupported, ACPErrOutputFailure, ACPErrInternal}
	seen := map[string]ACPErrorKind{}
	for _, k := range kinds {
		m := ACPErrorMessage(k)
		if m == "" {
			t.Fatalf("%s: no fixed message", k)
		}
		if prev, dup := seen[m]; dup {
			t.Fatalf("%s and %s share the message %q", prev, k, m)
		}
		seen[m] = k
	}
	if m := ACPErrorMessage(ACPErrNoAPIKey); !strings.Contains(m, "ask auth") {
		t.Fatalf("auth guidance missing: %q", m)
	}
	if ACPErrorMessage("other") != ACPErrorMessage(ACPErrInternal) {
		t.Fatal("an unknown kind must give the internal text")
	}
	if ACPErrorCode(ACPErrInvalidState) != ACPCodeInvalidState || ACPErrorCode(ACPErrInvalidParams) != -32602 {
		t.Fatal("new kind codes")
	}
}
