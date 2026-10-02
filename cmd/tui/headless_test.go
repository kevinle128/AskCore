package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/internal/agent"
	"AskCore/pkg/protocol"
)

// os/signal.loop starts at the first signal.Notify and lives for the rest
// of the process.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreAnyFunction("os/signal.loop"))
}

var (
	longPrompt = strings.TrimSpace(strings.Repeat("word ", 200))
	// bigPrompt makes one record larger than any pipe buffer, so a write to
	// a closed pipe is certain to happen.
	bigPrompt = strings.Repeat("a", 300_000)
)

type result struct {
	stdout, stderr string
	code           int
}

func runCLI(stdin string, argv ...string) result {
	var out, errb bytes.Buffer
	code := run(argv, strings.NewReader(stdin), &out, &errb)
	return result{out.String(), errb.String(), code}
}

func TestPrint(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		argv  []string
		want  result
	}{
		{"hello", "", []string{"-p", "hello"}, result{"hello\n", "", 0}},
		{"echo tool", "", []string{"-p", "echo hi"}, result{"hi\n", "", 0}},
		{"assistant error", "", []string{"-p", "fail boom"}, result{"", "boom\n", 1}},
		{"first prompt fails, second runs", "", []string{"-p", "fail boom", "second"}, result{"second\n", "", 0}},
		{"last prompt fails", "", []string{"-p", "first", "fail late"}, result{"", "late\n", 1}},
		{"unknown provider", "", []string{"--provider", "openai", "-p", "hi"},
			result{"", "Error: provider \"openai\" is not available until H3\n", 1}},
		{"unknown model", "", []string{"--model", "gpt", "-p", "hi"},
			result{"", "Error: model \"gpt\" not found for provider \"faux\"\n", 1}},
		{"stdin joins the message", "a", []string{"-p", "b"}, result{"ab\n", "", 0}},
		{"piped stdin alone selects print mode", "piped\n", nil, result{"piped\n", "", 0}},
		{"slash stays literal", "", []string{"-p", "/help"}, result{"/help\n", "", 0}},
		{"no prompt prints nothing", "", []string{"-p"}, result{"", "", 0}},
		{"bad thinking warns and runs", "", []string{"--thinking", "huge", "-p", "hi"},
			result{"hi\n", "Warning: Invalid thinking level \"huge\". Valid values: off, minimal, low, medium, high, xhigh, max\n", 0}},
		{"bad mode", "", []string{"--mode", "x", "-p", "hi"}, result{"", "Error: Invalid mode \"x\". Valid values: text, json\n", 1}},
		{"unknown flag", "", []string{"--nope", "-p", "hi"}, result{"", "Error: Unknown option: --nope\n", 1}},
		{"help", "", []string{"--help"}, result{usage, "", 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, runCLI(tt.stdin, tt.argv...))
		})
	}
}

func TestPrintMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")

	got := runCLI("", "-p", "@"+missing)

	assert.Equal(t, result{"", "Error: File not found: " + missing + "\n", 1}, got)
}

func TestPrintBadPaceEnv(t *testing.T) {
	t.Setenv(fauxPaceEnv, "fast")

	got := runCLI("", "-p", "hi")

	assert.Equal(t, result{"", "Error: ASK_FAUX_TPS must be a non-negative number, got \"fast\"\n", 1}, got)
}

var volatileFields = regexp.MustCompile(`"(ts|timestamp)":\d+|"runId":"[0-9a-f]{32}"`)

// normalize replaces the clock and run id values, which change per run.
func normalize(jsonl string) string {
	return volatileFields.ReplaceAllStringFunc(jsonl, func(s string) string {
		if strings.HasPrefix(s, `"runId"`) {
			return `"runId":"R"`
		}
		return s[:strings.IndexByte(s, ':')+1] + "0"
	})
}

func TestJSONHelloLines(t *testing.T) {
	got := runCLI("", "--mode", "json", "hello")

	want := `{"seq":1,"ts":0,"sessionId":"","runId":"R","type":"agent_start"}
{"seq":2,"ts":0,"sessionId":"","runId":"R","type":"turn_start"}
{"seq":3,"ts":0,"sessionId":"","runId":"R","type":"message_start","message":{"role":"user","content":[{"type":"text","text":"hello"}],"timestamp":0}}
{"seq":4,"ts":0,"sessionId":"","runId":"R","type":"message_end","message":{"role":"user","content":[{"type":"text","text":"hello"}],"timestamp":0}}
{"seq":5,"ts":0,"sessionId":"","runId":"R","type":"message_start","message":{"role":"assistant","content":[],"api":"faux","provider":"faux","model":"faux-1","usage":{"input":54,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":54,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"pending","timestamp":0}}
{"seq":6,"ts":0,"sessionId":"","runId":"R","type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":0,"content":{"type":"text","text":""}},"usage":{"input":54,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":56,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}
{"seq":7,"ts":0,"sessionId":"","runId":"R","type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hello"},"usage":{"input":54,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":56,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}
{"seq":8,"ts":0,"sessionId":"","runId":"R","type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":0,"content":"hello"},"usage":{"input":54,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":56,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}
{"seq":9,"ts":0,"sessionId":"","runId":"R","type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"api":"faux","provider":"faux","model":"faux-1","thinkingLevel":"off","usage":{"input":54,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":56,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":0}}
{"seq":10,"ts":0,"sessionId":"","runId":"R","type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"api":"faux","provider":"faux","model":"faux-1","thinkingLevel":"off","usage":{"input":54,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":56,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":0},"toolResults":[]}
{"seq":11,"ts":0,"sessionId":"","runId":"R","type":"agent_end","messages":[{"role":"user","content":[{"type":"text","text":"hello"}],"timestamp":0},{"role":"assistant","content":[{"type":"text","text":"hello"}],"api":"faux","provider":"faux","model":"faux-1","thinkingLevel":"off","usage":{"input":54,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":56,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":0}]}
{"seq":12,"ts":0,"sessionId":"","runId":"R","type":"agent_settled"}
`
	assert.Equal(t, result{want, "", 0}, result{normalize(got.stdout), got.stderr, got.code})
}

func decodeJSONL(t *testing.T, stdout string) []protocol.Event {
	t.Helper()
	require.True(t, strings.HasSuffix(stdout, "\n"), "the stream ends with LF")
	var evs []protocol.Event
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		ev, err := protocol.DecodeEvent([]byte(line))
		require.NoError(t, err, "line %q", line)
		evs = append(evs, ev)
	}
	return evs
}

// label names an event the way Appendix E of the H2 plan does.
func label(ev protocol.Event) string {
	switch e := ev.(type) {
	case *protocol.MessageStart:
		return e.EventType() + "(" + e.Message.Role() + ")"
	case *protocol.MessageEnd:
		return e.EventType() + "(" + e.Message.Role() + ")"
	case *protocol.MessageUpdate:
		return e.EventType() + "(" + e.AssistantMessageEvent.EventType() + ")"
	case *protocol.ToolExecutionStart:
		return e.EventType() + "(" + e.ToolName + ")"
	case *protocol.ToolExecutionEnd:
		return e.EventType() + "(" + e.ToolName + ")"
	}
	return ev.EventType()
}

func labels(evs []protocol.Event) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = label(ev)
	}
	return out
}

func TestJSONEchoEventOrder(t *testing.T) {
	got := runCLI("", "--mode", "json", "echo hi")

	require.Equal(t, 0, got.code)
	assert.Equal(t, "", got.stderr)
	evs := decodeJSONL(t, got.stdout)
	assert.Equal(t, []string{
		"agent_start",
		"turn_start",
		"message_start(user)", "message_end(user)",
		"message_start(assistant)", "message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)", "message_end(assistant)",
		"tool_execution_start(echo)", "tool_execution_end(echo)",
		"message_start(toolResult)", "message_end(toolResult)",
		"turn_end",
		"turn_start",
		"message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)",
		"turn_end",
		"agent_end",
		"agent_settled",
	}, labels(evs))
	runID := evs[0].Env().RunID
	require.Len(t, runID, 32)
	for i, ev := range evs {
		assert.Equal(t, uint64(i+1), ev.Env().Seq, "seq of %s", label(ev))
		assert.Equal(t, runID, ev.Env().RunID, "runId of %s", label(ev))
	}
}

func lastAssistantText(t *testing.T, evs []protocol.Event) string {
	t.Helper()
	for i := len(evs) - 1; i >= 0; i-- {
		if end, ok := evs[i].(*protocol.MessageEnd); ok {
			if m, ok := end.Message.(protocol.AssistantMessage); ok {
				return m.Content[0].(protocol.Text).Text
			}
		}
	}
	t.Fatal("no assistant message_end")
	return ""
}

func TestJSONFraming(t *testing.T) {
	t.Run("100 KiB reply", func(t *testing.T) {
		text := strings.Repeat("x", 100<<10)

		got := runCLI("", "--mode", "json", text)

		require.Equal(t, 0, got.code)
		evs := decodeJSONL(t, got.stdout)
		assert.Equal(t, text, lastAssistantText(t, evs))
		longest := 0
		for _, line := range strings.Split(got.stdout, "\n") {
			longest = max(longest, len(line))
		}
		assert.Greater(t, longest, 100<<10)
		assert.Equal(t, "agent_settled", label(evs[len(evs)-1]))
	})
	t.Run("U+2028 stays inside its record", func(t *testing.T) {
		got := runCLI("", "--mode", "json", "a\u2028b\u2029c")

		require.Equal(t, 0, got.code)
		assert.Equal(t, "a\u2028b\u2029c", lastAssistantText(t, decodeJSONL(t, got.stdout)))
	})
}

func TestJSONAssistantErrorExitsZero(t *testing.T) {
	got := runCLI("", "--mode", "json", "fail boom")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, "", got.stderr)
	evs := decodeJSONL(t, got.stdout)
	assert.Equal(t, []string{"message_end(assistant)", "turn_end", "agent_end", "agent_settled"}, labels(evs[len(evs)-4:]))
	end := evs[len(evs)-4].(*protocol.MessageEnd).Message.(protocol.AssistantMessage)
	assert.Equal(t, protocol.StopError, end.StopReason)
	require.NotNil(t, end.ErrorMessage)
	assert.Equal(t, "boom", *end.ErrorMessage)
}

func TestJSONTwoPrompts(t *testing.T) {
	got := runCLI("", "--mode", "json", "a", "b")

	require.Equal(t, 0, got.code)
	evs := decodeJSONL(t, got.stdout)
	var settled []int
	for i, ev := range evs {
		assert.Equal(t, uint64(i+1), ev.Env().Seq)
		if ev.EventType() == protocol.TypeAgentSettled {
			settled = append(settled, i)
		}
	}
	require.Equal(t, []int{11, 23}, settled)
	first, second := evs[0].Env().RunID, evs[12].Env().RunID
	assert.NotEqual(t, first, second)
	for i, ev := range evs {
		want := first
		if i > 11 {
			want = second
		}
		assert.Equal(t, want, ev.Env().RunID, "runId of event %d", i)
	}
}

// gatedWriter holds every write until gate is closed.
type gatedWriter struct {
	gate chan struct{}
	buf  bytes.Buffer
}

func (g *gatedWriter) Write(p []byte) (int, error) {
	<-g.gate
	return g.buf.Write(p)
}

func newAgent(t *testing.T, tps string) *agent.Agent {
	t.Helper()
	ag, err := newHeadlessAgent(options{provider: "faux", model: "faux-1"}, func(k string) string {
		if k == fauxPaceEnv {
			return tps
		}
		return ""
	})
	require.NoError(t, err)
	return ag
}

func TestJSONSlowReaderStallsRun(t *testing.T) {
	ag := newAgent(t, "")
	w := &gatedWriter{gate: make(chan struct{})}
	var errb bytes.Buffer
	codes := make(chan int, 1)

	go func() { codes <- runHeadless(ag, []string{"hello"}, modeJSON, w, &errb, nil) }()
	time.Sleep(200 * time.Millisecond)

	select {
	case <-codes:
		t.Fatal("the run ended while stdout was blocked")
	default:
	}
	assert.Equal(t, agent.Running, ag.State().Status)
	close(w.gate)
	assert.Equal(t, 0, <-codes)
	evs := decodeJSONL(t, w.buf.String())
	assert.Len(t, evs, 12)
	assert.Equal(t, "", errb.String())
}

func TestEPIPEInProcess(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		read func(r io.Reader)
	}{
		{"json", []string{"--mode", "json", bigPrompt}, func(r io.Reader) { _, _ = bufio.NewReader(r).ReadString('\n') }},
		{"text", []string{"-p", bigPrompt}, func(r io.Reader) { _, _ = r.Read(make([]byte, 1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			require.NoError(t, err)
			defer func() { _ = w.Close() }()
			readerDone := make(chan struct{})
			go func() {
				defer close(readerDone)
				tc.read(r)
				_ = r.Close()
			}()
			var errb bytes.Buffer

			code := run(tc.argv, strings.NewReader(""), w, &errb)

			<-readerDone
			assert.Equal(t, 1, code)
			assert.Equal(t, "", errb.String())
		})
	}
}

func TestSignalInProcess(t *testing.T) {
	for _, mode := range []runMode{modePrint, modeJSON} {
		for sig, want := range map[os.Signal]int{os.Interrupt: 130, syscall.SIGTERM: 143, syscall.SIGHUP: 129} {
			t.Run(map[runMode]string{modePrint: "text", modeJSON: "json"}[mode]+"/"+sig.String(), func(t *testing.T) {
				ag := newAgent(t, "20")
				streaming := make(chan struct{})
				var once sync.Once
				ag.Subscribe(func(ev protocol.Event) error {
					if _, ok := ev.(*protocol.MessageUpdate); ok {
						once.Do(func() { close(streaming) })
					}
					return nil
				})
				sigs := make(chan os.Signal, 1)
				var out, errb bytes.Buffer
				codes := make(chan int, 1)

				go func() { codes <- runHeadless(ag, []string{longPrompt, "never runs"}, mode, &out, &errb, sigs) }()
				<-streaming
				start := time.Now()
				sigs <- sig

				select {
				case code := <-codes:
					assert.Equal(t, want, code)
				case <-time.After(5 * time.Second):
					t.Fatal("runHeadless did not return after the signal")
				}
				assert.Less(t, time.Since(start), time.Second)
				assert.Equal(t, "", errb.String())
				if mode == modePrint {
					assert.Equal(t, "", out.String())
				} else {
					evs := decodeJSONL(t, out.String())
					assert.Equal(t, "agent_settled", label(evs[len(evs)-1]))
				}
				st := ag.State()
				assert.Equal(t, agent.Idle, st.Status)
				last := st.Messages[len(st.Messages)-1].(protocol.AssistantMessage)
				assert.Equal(t, protocol.StopAborted, last.StopReason)
				assert.Equal(t, 2, len(st.Messages), "only the first prompt and its aborted reply")
			})
		}
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ask")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, string(out))
	// The first exec of a new binary can be slow (macOS assesses it), and a
	// signal that lands before signal.Notify kills the process instead.
	require.NoError(t, exec.Command(bin, "--help").Run())
	return bin
}

func TestSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := buildBinary(t)
	for _, mode := range []string{"text", "json"} {
		for _, tc := range []struct {
			sig  syscall.Signal
			want int
		}{
			{syscall.SIGINT, 130},
			{syscall.SIGTERM, 143},
			{syscall.SIGHUP, 129},
		} {
			t.Run(mode+"/"+tc.sig.String(), func(t *testing.T) {
				cmd := exec.Command(bin, "--mode", mode, "-p", longPrompt)
				cmd.Env = append(os.Environ(), fauxPaceEnv+"=2")
				var out, errb bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &errb
				require.NoError(t, cmd.Start())
				time.Sleep(500 * time.Millisecond)
				start := time.Now()
				require.NoError(t, cmd.Process.Signal(tc.sig))

				err := cmd.Wait()

				var exit *exec.ExitError
				require.True(t, errors.As(err, &exit), "want an exit error, got %v", err)
				assert.Equal(t, tc.want, exit.ExitCode())
				assert.Less(t, time.Since(start), time.Second)
				assert.Equal(t, "", errb.String())
				if mode == "text" {
					assert.Equal(t, "", out.String())
				} else {
					evs := decodeJSONL(t, out.String())
					assert.Equal(t, "agent_settled", label(evs[len(evs)-1]))
				}
			})
		}
	}
}

func TestEPIPE(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := buildBinary(t)
	readLine := func(r io.Reader) { _, _ = bufio.NewReader(r).ReadString('\n') }
	readByte := func(r io.Reader) { _, _ = r.Read(make([]byte, 1)) }
	for _, tc := range []struct {
		name, tps, stdin string
		argv             []string
		read             func(r io.Reader)
	}{
		{"json paced", "200", "", []string{"--mode", "json", longPrompt}, readLine},
		{"json large", "", bigPrompt, []string{"--mode", "json"}, readLine},
		{"text large", "", bigPrompt, []string{"-p"}, readByte},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			require.NoError(t, err)
			cmd := exec.Command(bin, tc.argv...)
			cmd.Env = append(os.Environ(), fauxPaceEnv+"="+tc.tps)
			cmd.Stdin = strings.NewReader(tc.stdin)
			var errb bytes.Buffer
			cmd.Stdout, cmd.Stderr = w, &errb
			require.NoError(t, cmd.Start())
			require.NoError(t, w.Close())

			tc.read(r)
			require.NoError(t, r.Close())
			closed := time.Now()
			err = cmd.Wait()

			var exit *exec.ExitError
			require.True(t, errors.As(err, &exit), "want an exit error, got %v", err)
			assert.Equal(t, 1, exit.ExitCode(), "exit 1, not 141 from SIGPIPE")
			assert.Equal(t, "", errb.String())
			assert.Less(t, time.Since(closed), time.Second)
		})
	}
}
