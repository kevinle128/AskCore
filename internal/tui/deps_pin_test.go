package tui_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The terminal stack is held at exact versions: the patched Bubble Tea copy
// and the Ultraviolet commit it was tested with. A newer Ultraviolet or
// lipgloss changes cell and escape sequence behavior that the renderer patch
// and the golden tests rely on, so a moved pin must fail here and not in a
// terminal. The copy is its own module, so its guard tests run from here.
const (
	pinBubbleTea   = "v2.0.10"
	pinUltraviolet = "v0.0.0-20260811164956-006e29f97886"
	pinLipgloss    = "v2.0.6"
	// Markdown and syntax highlighting. Glamour v2.0.1 needs lipgloss v2.0.4 and
	// an older Ultraviolet, so it must not move the two pins above.
	pinGlamour = "v2.0.1"
	pinChroma  = "v2.27.0"
)

// goOutput runs the go tool in the repository root and returns its stdout.
func goOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = "../.."
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "go %s\n%s", strings.Join(args, " "), stderr.String())
	return stdout.String()
}

func TestDependencyPins(t *testing.T) {
	t.Run("module versions", func(t *testing.T) {
		out := goOutput(t, "list", "-m", "-f", "{{.Path}} {{.Version}} {{with .Replace}}{{.Path}}{{end}}",
			"charm.land/bubbletea/v2", "github.com/charmbracelet/ultraviolet", "charm.land/lipgloss/v2",
			"charm.land/glamour/v2", "github.com/alecthomas/chroma/v2")
		got := map[string][]string{}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			f := strings.Fields(line)
			require.GreaterOrEqual(t, len(f), 2, "line %q", line)
			got[f[0]] = f[1:]
		}
		require.Equal(t, []string{pinBubbleTea, "./third_party/bubbletea"}, got["charm.land/bubbletea/v2"],
			"Bubble Tea must come from the patched copy")
		require.Equal(t, []string{pinUltraviolet}, got["github.com/charmbracelet/ultraviolet"])
		require.Equal(t, []string{pinLipgloss}, got["charm.land/lipgloss/v2"])
		require.Equal(t, []string{pinGlamour}, got["charm.land/glamour/v2"])
		require.Equal(t, []string{pinChroma}, got["github.com/alecthomas/chroma/v2"])
	})

	t.Run("patched Bubble Tea copy passes its own tests", func(t *testing.T) {
		goOutput(t, "-C", "third_party/bubbletea", "test", "-count=1", "./...")
	})

	t.Run("no v1 terminal libraries in Ask packages", func(t *testing.T) {
		out := goOutput(t, "list", "-deps", "./cmd/...", "./internal/...", "./pkg/...")
		for _, pkg := range strings.Fields(out) {
			for _, v1 := range []string{"github.com/charmbracelet/bubbletea", "github.com/charmbracelet/lipgloss"} {
				if pkg == v1 || strings.HasPrefix(pkg, v1+"/") {
					t.Errorf("package %s is a v1 terminal library", pkg)
				}
			}
		}
	})
}
