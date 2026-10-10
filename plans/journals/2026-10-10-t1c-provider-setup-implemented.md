# T1c provider setup implemented

Date: 2026-10-10. Plan: `plans/261010-1424-t1c-provider-setup/plan.md`.

## What happened

Phases 2 to 8 ran through delegates (phases 2 and 3 in parallel, the rest in order). Each phase wrote failing tests first and ended with `go vet`, `go test ./...` and lint green. Nothing is committed.

## Decisions worth keeping

- Provider methods are served only on the leader link. `ask acp` and the in-process link answer `unsupported`, even with a forged route.
- The faux provider is opt-in with `ASK_FAUX=1`. The env helpers and the runner changed in the same step.
- A follow result now carries `head`, so a reconnect replay draws no change lines for missed model changes. An older leader sends none and behaves as before.

## Defect found at the end

`TestResyncSnapshotReplacesTranscript` failed in about 3 of 30 runs. The cause was in the inline rebuild: stable rows that no commit had taken were drawn in the live tail and then committed above it, so they showed twice for one frame. The window also exists at HEAD. `pump` now takes stable rows before the rebuild frame. A deterministic test covers it.

## Real-provider run

Scenarios 1 to 5 passed (41 steps, 0 failed, secret scan clean). Scenario 6 (copy-code or device sign-in) was not run: it needs the user present.

## Open for the user

- Visible texts that the approved design does not state (start failure reason, wrong pasted code, removal edge cases, old leader). The closest approved wording was used. Per the design skill, a change of visible text sends the design back to draft.
- Criterion 25 says `Remove openai?`; the sketch says `Remove ChatGPT Plus/Pro subscription?`. The sketch was followed.
- "Your draft is kept" can only hold for an empty editor, because `/providers` is the whole editor line.
- ChatGPT account naming cannot be driven from the TUI (the browser flow needs a browser); criterion 20 uses the xAI device flow.
- `plans/261009-2239-t1a-local-tui/artifacts/__pycache__/sketch-gen.cpython-314.pyc` is tracked and shows as modified.
