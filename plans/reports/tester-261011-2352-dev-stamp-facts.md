# Dev stamp facts: how `go run` places and dates its binary

Date: 2026-10-11. Go 1.27.0 darwin/arm64. Method: a one-file program that prints `os.Executable()`, the modification time of that file and the `vcs.time` build setting, run with `go run .` three times (the third after `touch main.go`), plus SHA-256 time of a 110 MB test binary of `cmd/tui`.

## Facts

1. First `go run` of new source runs the binary from a temporary directory (`$TMPDIR/go-build<N>/b001/exe/<name>`). The Go tool removes that directory when the program ends.
2. A later `go run` of the same source (also after `touch` of the source file, which leaves the content equal) runs the binary from the build cache (`~/Library/Caches/go-build/xx/<id>-d`). The file is the same on each reuse and its modification time does not change on reuse.
3. The modification time of the temporary binary and of the cached copy of the same bytes differ by a few milliseconds. Same bytes give the same SHA-256, so the dev stamp rule "same hash keeps the leader" holds for both.
4. A binary run by `go run` has no `vcs.time` (value `none`), so it gets a dev stamp. Output is the same for the three runs except for the path and time given above.
5. SHA-256 of 110 MB (`shasum -a 256`): 0.23 s. The real `ask` binary is smaller. This is inside the 15 s probe bound of `ProbeAsk` and happens once per process, for dev builds only.

## Consequence for the plan

- No change to the decision (dev stamp, compared only with dev stamps, same hash keeps). The "removed `go run` binary" risk is real (fact 1): the leader must compute its stamp first in `serveLeader`.
- A cached binary keeps its modification time (fact 2), so "the binary written last wins" is stable between runs of unchanged source.

## Unresolved questions

None.
