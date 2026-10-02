package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

var longPrompt = strings.TrimSpace(strings.Repeat("word ", 200))

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
		{"json mode is not built yet", "", []string{"--mode", "json", "hi"}, result{"", "Error: --mode json is not implemented yet\n", 1}},
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

func TestSignalInProcess(t *testing.T) {
	for sig, want := range map[os.Signal]int{os.Interrupt: 130, syscall.SIGTERM: 143, syscall.SIGHUP: 129} {
		t.Run(sig.String(), func(t *testing.T) {
			getenv := func(k string) string {
				if k == fauxPaceEnv {
					return "20"
				}
				return ""
			}
			ag, err := newHeadlessAgent(options{provider: "faux", model: "faux-1"}, getenv)
			require.NoError(t, err)
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

			go func() { codes <- runPrint(ag, []string{longPrompt, "never runs"}, &out, &errb, sigs) }()
			<-streaming
			start := time.Now()
			sigs <- sig

			select {
			case code := <-codes:
				assert.Equal(t, want, code)
			case <-time.After(5 * time.Second):
				t.Fatal("runPrint did not return after the signal")
			}
			assert.Less(t, time.Since(start), time.Second)
			assert.Equal(t, "", out.String())
			assert.Equal(t, "", errb.String())
			st := ag.State()
			assert.Equal(t, agent.Idle, st.Status)
			last := st.Messages[len(st.Messages)-1].(protocol.AssistantMessage)
			assert.Equal(t, protocol.StopAborted, last.StopReason)
			assert.Equal(t, 2, len(st.Messages), "only the first prompt and its aborted reply")
		})
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ask")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, string(out))
	return bin
}

func TestSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := buildBinary(t)
	// The first exec of a new binary can be slow (macOS assesses it), and a
	// signal that lands before signal.Notify kills the process instead.
	require.NoError(t, exec.Command(bin, "--help").Run())
	for _, tc := range []struct {
		sig  syscall.Signal
		want int
	}{
		{syscall.SIGINT, 130},
		{syscall.SIGTERM, 143},
		{syscall.SIGHUP, 129},
	} {
		t.Run(tc.sig.String(), func(t *testing.T) {
			cmd := exec.Command(bin, "-p", longPrompt)
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
			assert.Equal(t, "", out.String())
			assert.Equal(t, "", errb.String())
		})
	}
}
