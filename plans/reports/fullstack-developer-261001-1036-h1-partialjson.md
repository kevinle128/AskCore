# H1 step 4.4: partialjson

Files (all new, in `internal/providers/partialjson/`): `partialjson.go`, `repair.go`, `partialjson_test.go`, `fuzz_test.go`. Standard library only in production code.

API: `Parse(string) map[string]any`, `Repair(string) string`, `StrictWithRepair([]byte, any) error`.

Verification (run from the repo root):
- `go test -race ./internal/providers/partialjson/...` : ok
- `go test -run='^$' -fuzz=Fuzz -fuzztime=20s ./internal/providers/partialjson/` : PASS, about 2.8M execs, no crash
- `go vet ./internal/providers/partialjson/` : clean
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/providers/partialjson/...` : 0 issues

Tests: vectors V1-V33 (plus extras: overflow in strict and tolerant paths, duplicate key in open string, early-stop on bad number), prefix sweep over 4 documents (nested, escapes, emoji, split UTF-8), Repair table, StrictWithRepair (valid, repairable, incomplete, unrepairable, empty), deep-nesting no-crash, fuzz target.

Decisions:
- The "text before last e" fallback runs only when the token is not a well-formed number. A well-formed but non-finite token (1e999) drops the key and stops the object.
- Tolerant parser has a depth limit of 10000 (same as encoding/json) to protect the stack.
- `StrictWithRepair` returns the first decode error if both decodes fail; `v` may be partly filled on error.

Status: DONE
Summary: Parser, repair and strict helper implemented with all required tests; race, fuzz, vet and lint pass.
Concerns: None. Not committed, as requested.
