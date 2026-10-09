package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// routeRecorder records the route meta that the SDK hands to each Agent method,
// then calls the real adapter. It proves delivery only: the adapter itself does
// not read the meta yet.
type routeRecorder struct {
	*Adapter
	mu   sync.Mutex
	seen map[string][]protocol.ACPRouteMeta
}

func (r *routeRecorder) record(method string, meta map[string]any) {
	var route protocol.ACPRouteMeta
	if raw, ok := meta[protocol.ACPRouteMetaKey]; ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &route)
	}
	r.mu.Lock()
	r.seen[method] = append(r.seen[method], route)
	r.mu.Unlock()
}

func (r *routeRecorder) got(method string) []protocol.ACPRouteMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]protocol.ACPRouteMeta(nil), r.seen[method]...)
}

func (r *routeRecorder) NewSession(ctx context.Context, req sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	r.record("session/new", req.Meta)
	return r.Adapter.NewSession(ctx, req)
}

func (r *routeRecorder) Prompt(ctx context.Context, req sdk.PromptRequest) (sdk.PromptResponse, error) {
	r.record("session/prompt", req.Meta)
	return r.Adapter.Prompt(ctx, req)
}

func (r *routeRecorder) Cancel(ctx context.Context, req sdk.CancelNotification) error {
	r.record("session/cancel", req.Meta)
	return r.Adapter.Cancel(ctx, req)
}

func (r *routeRecorder) HandleExtensionMethod(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	var params struct {
		Meta map[string]any `json:"_meta"`
	}
	_ = json.Unmarshal(raw, &params)
	r.record(method, params.Meta)
	return r.Adapter.HandleExtensionMethod(ctx, method, raw)
}

func newRecordedPeer(t *testing.T) (*adapterPeer, *routeRecorder) {
	t.Helper()
	baseline := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, baseline) })
	a, err := NewAdapter(context.Background(), testConfig(says("one", "two"), nil))
	require.NoError(t, err)
	rec := &routeRecorder{Adapter: a, seen: map[string][]protocol.ACPRouteMeta{}}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	cw := NewCheckedWriter(outW, func(err error) { a.Fail(err); _ = inR.CloseWithError(err) })
	conn := sdk.NewAgentSideConnection(rec, cw, NewLineLimitReader(inR, 1<<20))
	a.Bind(conn, cw, outW)
	p := &adapterPeer{t: t, a: a, conn: conn, in: inW}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
		for sc.Scan() {
			var f rpcFrame
			_ = json.Unmarshal(sc.Bytes(), &f)
			p.mu.Lock()
			p.lines = append(p.lines, sc.Text())
			p.frames = append(p.frames, f)
			p.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		select {
		case <-conn.Done():
		case <-time.After(peerWait):
			t.Error("connection did not stop")
		}
		_ = a.Close()
		_ = outW.Close()
		_ = outR.Close()
		<-readerDone
		_ = inR.Close()
	})
	return p, rec
}

func routeMeta(clientID, gen string, live bool) map[string]any {
	return map[string]any{protocol.ACPRouteMetaKey: map[string]any{"clientId": clientID, "driverGen": gen, "liveDriver": live}}
}

// TestRouteMetaReachesAdapter checks that the SDK hands a per-request route
// context to the Agent methods over one link after one link-level initialize.
// Requests from two clients share the link and carry different contexts.
func TestRouteMetaReachesAdapter(t *testing.T) {
	p, rec := newRecordedPeer(t)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)

	const bigGen = "9007199254740993" // above 2^53: it must stay exact
	sessions := map[string]string{}
	for client, gen := range map[string]string{"A": "1", "B": bigGen} {
		var res struct {
			SessionID string `json:"sessionId"`
		}
		p.ok("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}, "_meta": routeMeta(client, gen, client == "B")}, &res)
		require.NotEmpty(t, res.SessionID)
		sessions[client] = res.SessionID
	}
	require.NotEqual(t, sessions["A"], sessions["B"], "the adapter result is unchanged: two independent sessions")

	prompt := promptParams(sessions["A"], "hello")
	prompt["_meta"] = routeMeta("A", "1", false)
	var done struct {
		StopReason string `json:"stopReason"`
	}
	p.ok("session/prompt", prompt, &done)
	require.Equal(t, "end_turn", done.StopReason)

	p.notify("session/cancel", map[string]any{"sessionId": sessions["B"], "_meta": routeMeta("B", bigGen, true)})
	p.ok("_ask/session/state", map[string]any{"sessionId": sessions["B"], "_meta": routeMeta("B", bigGen, true)}, nil)
	p.waitFor("cancel recorded", func() bool { return len(rec.got("session/cancel")) == 1 })

	require.Equal(t, []protocol.ACPRouteMeta{
		{ClientID: "A", DriverGen: 1},
		{ClientID: "B", DriverGen: 9007199254740993, LiveDriver: true},
	}, sortedByClient(rec.got("session/new")))
	require.Equal(t, []protocol.ACPRouteMeta{{ClientID: "A", DriverGen: 1}}, rec.got("session/prompt"))
	require.Equal(t, []protocol.ACPRouteMeta{{ClientID: "B", DriverGen: 9007199254740993, LiveDriver: true}}, rec.got("session/cancel"))
	require.Equal(t, []protocol.ACPRouteMeta{{ClientID: "B", DriverGen: 9007199254740993, LiveDriver: true}}, rec.got(protocol.ACPState))
}

func sortedByClient(in []protocol.ACPRouteMeta) []protocol.ACPRouteMeta {
	out := append([]protocol.ACPRouteMeta(nil), in...)
	if len(out) == 2 && out[0].ClientID > out[1].ClientID {
		out[0], out[1] = out[1], out[0]
	}
	return out
}
