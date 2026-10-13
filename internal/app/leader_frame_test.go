package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/leader"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestLeaderLargeFramesThroughRealHost(t *testing.T) {
	if testing.Short() {
		t.Skip("moves large socket frames")
	}
	const size = 11 << 20
	input, output := strings.Repeat("i", size), strings.Repeat("o", size)
	seen := make(chan string, 1)
	fp, err := faux.New(faux.WithChunk(size, size))
	require.NoError(t, err)
	fp.Set(faux.Func(func(_ctx context.Context, call faux.Call) (faux.Step, error) {
		for _, msg := range call.Request.Messages {
			if user, ok := msg.(protocol.UserMessage); ok {
				for _, block := range user.Content {
					if text, ok := block.(protocol.Text); ok {
						seen <- text.Text
					}
				}
			}
		}
		return faux.Say(output), nil
	}))
	l := startLeader(t, fauxLeaderParamsWith(t, saying("unused"), func(c *agent.Config) { c.Stream = fp.Stream }))
	a, b := l.client(nil), l.client(nil)
	sid := a.newSession("/w")
	b.must(protocol.ACPAttach, map[string]any{"sessionId": sid})
	id := a.request("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": input}}})
	_, replyErr := a.awaitWithin(id, 30*time.Second)
	require.Nil(t, replyErr)
	require.Equal(t, input, <-seen, "the SDK delivered the complete large prompt to the real Agent")
	for _, c := range []*routingClient{a, b} {
		routingEventuallyWithin(t, "large update", 30*time.Second, func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			for _, m := range c.msgs {
				var params struct {
					Update struct {
						SessionUpdate string `json:"sessionUpdate"`
						Content       struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"update"`
				}
				if json.Unmarshal(m["params"], &params) == nil && params.Update.Content.Text == output {
					return true
				}
			}
			return false
		})
	}
	require.NoError(t, l.rt.Adapter.Failure())
}

func TestLeaderMaximumFrameAndOversizeClientIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("moves a 64 MiB socket frame")
	}
	l := startLeader(t, fauxLeaderParams(t, saying("ok")))
	// Maximum legal capabilities leave enough of the 1 MiB link reserve for route data.
	caps := map[string]any{"extra": strings.Repeat("c", (64<<10)-12)}
	a, b := l.client(caps), l.client(nil)
	sid := a.newSession("/w")
	b.must(protocol.ACPAttach, map[string]any{"sessionId": sid})
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 999, "method": protocol.ACPState, "params": map[string]any{"sessionId": sid, "_meta": map[string]any{"padding": ""}}})
	var frame bytes.Buffer
	require.NoError(t, leader.NewFrameWriter(&frame, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: raw}))
	raw = bytes.Replace(raw, []byte(`"padding":""`), []byte(`"padding":"`+strings.Repeat("p", protocol.LeaderMaxFrame-(frame.Len()-4))+`"`), 1)
	frame.Reset()
	require.NoError(t, leader.NewFrameWriter(&frame, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: raw}))
	require.Equal(t, uint32(protocol.LeaderMaxFrame), binary.BigEndian.Uint32(frame.Bytes()[:4]))
	_, err := a.conn.Write(frame.Bytes())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, m := range a.msgs {
			if string(m["id"]) == "999" {
				require.Nil(t, m["error"])
				return true
			}
		}
		return false
	}, 30*time.Second, 10*time.Millisecond, "maximum socket frame did not cross the real SDK link")
	// Sending just an oversized length must close A without allocating its claimed body.
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], protocol.LeaderMaxFrame+1)
	_, err = a.conn.Write(hdr[:])
	require.NoError(t, err)
	select {
	case <-a.done:
	case <-time.After(routingWait):
		t.Fatal("oversized client stayed connected")
	}
	require.NoError(t, l.rt.Adapter.Failure())
	b.must(protocol.ACPTake, map[string]any{"sessionId": sid})
	require.Contains(t, string(b.must("session/prompt", promptArgs(sid))), "end_turn", "other clients and the shared SDK link remain usable")
}
