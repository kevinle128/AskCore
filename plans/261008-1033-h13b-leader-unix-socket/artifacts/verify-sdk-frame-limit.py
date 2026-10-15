"""Check the installed SDK cap and a buffer-only patch in a temporary copy.

Run from the repository root: python3 plans/261008-1033-h13b-leader-unix-socket/artifacts/verify-sdk-frame-limit.py
This does not change the installed SDK, go.mod, or generated files.
"""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile


PROBE = r'''
package main
import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "strconv"
    "strings"
    "time"
    sdk "github.com/coder/acp-go-sdk"
)
type output struct { done chan struct{} }
func (w output) Write(p []byte) (int, error) { select { case w.done <- struct{}{}: default: }; return len(p), nil }
func main() {
    size, _ := strconv.Atoi(os.Args[1])
    expect := os.Args[2] == "accept"
    prefix := `{"jsonrpc":"2.0","id":1,"method":"probe","params":{"text":"`
    suffix := `"}}`
    line := prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix + "\n"
    r, w := io.Pipe()
    defer r.Close()
    defer w.Close()
    out := output{make(chan struct{}, 1)}
    conn := sdk.NewConnection(func(context.Context, string, json.RawMessage) (any, *sdk.RequestError) { return struct{}{}, nil }, out, r)
    go func() { _, _ = io.WriteString(w, line) }()
    accepted := false
    select { case <-out.done: accepted = true; case <-conn.Done(): case <-time.After(15*time.Second): panic("probe timeout") }
    if accepted != expect { panic(fmt.Sprintf("size=%d accepted=%v expected=%v", size, accepted, expect)) }
    fmt.Printf("PASS size=%d accepted=%v\n", size, accepted)
}
'''


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def main():
    sdk = Path(run("go", "list", "-m", "-f", "{{.Dir}}", "github.com/coder/acp-go-sdk"))
    with tempfile.TemporaryDirectory(prefix="ask-sdk-cap-") as directory:
        root = Path(directory)
        env = dict(os.environ, GOCACHE="/private/tmp/ask-h13b-review-cache", GOWORK="off", GOPROXY="off")
        probe = root / "main.go"
        probe.write_text(PROBE)
        patched = root / "sdk"
        shutil.copytree(sdk, patched)
        source = patched / "connection.go"
        source.chmod(0o600)
        before = source.read_text()
        old = "maxBufSize     = 10 * 1024 * 1024"
        assert before.count(old) == 1, "SDK changed; review the probe before use"
        source.write_text(before.replace(old, "maxBufSize     = (65 << 20) + 1"))
        for name, path, cases in (
            ("installed", sdk, ((9 << 20, "accept"), (11 << 20, "reject"))),
            ("buffer-only patch", patched, ((11 << 20, "accept"), (64 << 20, "accept"), (65 << 20, "accept"), ((65 << 20) + 1, "reject"))),
        ):
            print(name, flush=True)
            (root / "go.mod").write_text(
                "module frameprobe\n\ngo 1.25\n\nrequire github.com/coder/acp-go-sdk v0.13.5\n"
                f"replace github.com/coder/acp-go-sdk => {path}\n"
            )
            for size, result in cases:
                print(run("go", "run", ".", str(size), result, cwd=root, env=env), flush=True)


if __name__ == "__main__":
    main()
