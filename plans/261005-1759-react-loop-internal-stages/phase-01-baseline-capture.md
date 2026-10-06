# Phase 1: Baseline capture

## Context
The refactor must not change behavior. Capture evidence from the current tree before editing any Go file. The working tree has uncommitted `cmd/tui` and `internal/providers` changes; the baseline is taken from that tree as-is, and those files are not touched by this plan.

## Steps
1. Record the test list: `go test ./internal/agent/... -race -count=1 -v 2>&1 | grep -E '^(=== RUN|--- )' > $SCRATCH/tests-before.txt`. All must pass. If any fail, stop and report (do not start phase 2 on a red tree).
2. Record lint: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` must be clean.
3. Capture JSONL and exit codes for three prompts:
   ```bash
   for p in "echo hi" "hello" "fail boom"; do
     f=$SCRATCH/before-$(echo "$p" | tr ' ' _).jsonl
     go run ./cmd/tui -p "$p" --mode json > "$f"; echo "$p exit=$?" >> $SCRATCH/exit-before.txt
     jq -cS 'del(.. | .ts?, .runId?, .timestamp?)' "$f" > "$f.norm"
   done
   ```
   Also capture plain print mode (`go run ./cmd/tui -p "echo hi"`) stdout to `before-plain.txt`.
4. Inspect one `.norm` file and confirm no other volatile field remains (for example duration or id fields). Add any to the `del(...)` list and note it in phase 7.

`$SCRATCH` is the session scratchpad directory; do not commit these files.

## Done when
Six normalized files, `exit-before.txt`, `tests-before.txt` exist and tests and lint are green.

## Rollback
Nothing to revert.
