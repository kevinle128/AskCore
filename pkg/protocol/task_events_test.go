package protocol

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskEventsEncodeDecode(t *testing.T) {
	exit := 3
	zero := 0
	cases := []struct {
		ev   Event
		want string
	}{
		{
			&TaskStart{Envelope: env(1), TaskID: "t-1", Kind: "timer", Title: "wait", StartedAt: 1700000000123},
			`{"seq":1,"ts":1700000000001,"sessionId":"s1","runId":"r1","type":"task_start","taskId":"t-1","kind":"timer","title":"wait","startedAt":1700000000123}`,
		},
		{
			&TaskUpdate{Envelope: env(2), TaskID: "t-1", OutputBytes: 2048, OutputLines: 12},
			`{"seq":2,"ts":1700000000002,"sessionId":"s1","runId":"r1","type":"task_update","taskId":"t-1","outputBytes":2048,"outputLines":12}`,
		},
		{
			&TaskEnd{Envelope: env(3), TaskID: "t-1", Status: "failed", OutputFile: "/tmp/t-1.log", ExitCode: &exit, DurationMS: 950},
			`{"seq":3,"ts":1700000000003,"sessionId":"s1","runId":"r1","type":"task_end","taskId":"t-1","status":"failed","exitCode":3,"outputFile":"/tmp/t-1.log","durationMs":950}`,
		},
		{
			&TaskEnd{Envelope: env(4), TaskID: "t-2", Status: "completed", ExitCode: &zero, DurationMS: 1},
			`{"seq":4,"ts":1700000000004,"sessionId":"s1","runId":"r1","type":"task_end","taskId":"t-2","status":"completed","exitCode":0,"durationMs":1}`,
		},
		{
			&TaskEnd{Envelope: env(5), TaskID: "t-3", Status: "stopped"},
			`{"seq":5,"ts":1700000000005,"sessionId":"s1","runId":"r1","type":"task_end","taskId":"t-3","status":"stopped","durationMs":0}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.ev.EventType(), func(t *testing.T) {
			b, err := EncodeEvent(tc.ev)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
			assert.NotContains(t, string(b), "\n")

			back, err := DecodeEvent(b)
			require.NoError(t, err)
			assert.Equal(t, tc.ev, back)
		})
	}
}

func TestTaskEventTypes(t *testing.T) {
	assert.Equal(t, "task_start", (&TaskStart{}).EventType())
	assert.Equal(t, "task_update", (&TaskUpdate{}).EventType())
	assert.Equal(t, "task_end", (&TaskEnd{}).EventType())
}

func TestTaskEventsInsideJSONLStream(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONLWriter(&buf)
	require.NoError(t, w.Write(&TaskStart{Envelope: env(1), TaskID: "t-1", Kind: "timer"}))
	require.NoError(t, w.Write(&TaskEnd{Envelope: env(2), TaskID: "t-1", Status: "completed"}))
	r := NewJSONLReader(&buf)
	for _, want := range []string{"task_start", "task_end"} {
		line, err := r.Next()
		require.NoError(t, err)
		ev, err := DecodeEvent(line)
		require.NoError(t, err)
		assert.Equal(t, want, ev.EventType())
	}
}
