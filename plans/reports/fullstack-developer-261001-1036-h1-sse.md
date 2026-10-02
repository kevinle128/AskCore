# H1 step 4.6: sse reader

Files: internal/providers/sse/{reader.go,reader_test.go,bench_test.go,fuzz_test.go}. No other file edited.

Design notes
- bufio.Reader, Peek/Discard scan of buffered bytes: each byte scanned once (linear). Pending-CR is resolved lazily on the first byte of the next line, so a bare \r dispatches without waiting for more input.
- BOM matched byte by byte at stream start (split-safe); a partial prefix that mismatches or hits EOF becomes text.
- Line limit checked per chunk before append; event limit counts stored field lines (event/data) only. Comments are never stored or counted, so comment streams use constant memory.
- Raw = field lines only, max 32 lines, each cut to 512 bytes at a rune boundary.
- Blank line sets a `ready` flag; ctx is checked at the top of the loop, so a cancelled ctx never yields an event, and the frame is kept for the next call. Cancel is not sticky; limit/read errors and EOF are sticky. A read error with a done ctx becomes ctx.Err().
- Event dispatches when name != "" or any data line exists (a bare `data:` dispatches an empty event, as in Pi).

Verification (from repo root)
- go vet ./internal/providers/sse/ : clean
- go test -race -count=1 ./internal/providers/sse/... : ok (also -count=3 ok); goleak via TestMain clean
- go test -run='^$' -fuzz=Fuzz -fuzztime=20s ./internal/providers/sse/ : PASS, 3.2M execs, no failures
- go test -run='^$' -bench=. ./internal/providers/sse/ : LongLine 1MiB 1767 MB/s, 16MiB 2909 MB/s; ManyEvents 1000 -> 208 MB/s, 100000 -> 215 MB/s (flat per byte: linear)
- golangci-lint run ./internal/providers/sse/... : clean (after fixing two errcheck in tests)

Status: DONE
Summary: SSE reader and the full test list (including split-offset, pipe cancel, httptest terminal-event close, bench, fuzz) pass.
Concerns: goleak is still marked `// indirect` in go.mod (not mine to edit; `go mod tidy` will fix). Event limit ignores comment lines and id/retry by design. Raw is truncated (32 x 512 bytes) rather than complete.
