# Phase 7: Verification and JSONL diff

## Steps
1. `go test ./internal/agent/... ./internal/pipeline/... -race -count=1` and compare `--- PASS` lines with `tests-before.txt`: every old test still passes, new ones added.
2. `go test ./... -count=1` green.
3. `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` 0 issues (depguard included).
4. Re-run the phase 1 capture loop into `after-*.jsonl`, normalize with the same `jq` filter, then `diff before-X.jsonl.norm after-X.jsonl.norm` for all three prompts and `diff before-plain.txt after-plain.txt`. All must be empty. Compare `exit-before.txt` with exit codes after.
5. `git diff --name-status -- 'internal/agent/*_test.go' 'internal/pipeline/*_test.go'` shows only `A` entries (`loop_stage_test.go`, `hooks_compose_test.go`).
6. `grep -nE 'phase|Phase|H[0-9]+|F[0-9]+' internal/agent/*.go internal/pipeline/*.go` shows no plan reference in new comments.
7. Read the final `loop.run`: confirm the driver is short and the outer follow-up loop is explicit.
8. Single seam: `grep -n 'cfg\.Hooks\.' internal/agent/*.go | grep -v _test.go` gives exactly 9 lines, one inside each seam method (`pollSteering`, `pollFollowUps`, `prepareRequest`, `finishTurn`, `transformContext`, `convertToLLM`, `apiKey`, `beforeToolCall`, `afterToolCall`). `agent.go` assigns `cfg.Hooks = pipeline.Compose(...)` and has no field-level wrap.
9. Compose: `go test ./internal/pipeline/... -race -count=1 -run 'Compose|Apply' -v` lists one passing test per merge rule. Flip one rule locally (for example make `FinishTurn` short-circuit on `End`) and confirm a test fails; restore, do not commit.
10. `git diff --name-status -- internal/pipeline` shows `A hooks_compose.go`, `A hooks_compose_test.go`, `M hooks.go`, `M README.md`, nothing else.

## Done when
All ten checks pass; record outputs in the plan's review notes. Any JSONL diff blocks merge: fix inside the refactor, do not edit tests.

## Rollback
See plan.md.
