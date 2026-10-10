---
phase: 8
title: "Real-provider acceptance and docs"
status: pending
priority: P1
effort: "1d"
dependencies: [7]
---

# Phase 8: Real-provider acceptance and docs

Outline. Run a scout pass and update this file before cooking.

## Goal

One run against real providers proves the key and OAuth paths with no secret in any artifact, and the docs, roadmap and the T1a design describe the implemented state.

## Real-provider run

- Runner: `e2e/tui/real_provider.py` with `real_provider_scenarios.py` (scenarios added here) and the secret scan of `e2e/tui/run.py:633-639`.
- Homes:
  - API key: a fresh home under a short path, for example `/private/tmp/ask-t1c-key` (the socket path must stay short; `internal/leader/paths.go`).
  - OAuth: a **copy** of `~/.ask-t1a-oauth` to `/private/tmp/ask-t1c-oauth` (token refresh rewrites `auth.json`, so the original is never used). Delete the copy after the run.
- Secrets, in this order: (1) the runner reads `OPENAI_API_KEY` and every other credential variable; (2) it builds the scan set from those values; (3) it removes every credential variable from the child environment, because `real_provider.py:37-41` removes only `ASK_*` names and the spawned leader inherits the rest (`internal/leader/spawn.go:272-276`), which would show `env OPENAI_API_KEY` and make scenarios 1 and 4 impossible; (4) it starts `ask`; (5) it sends the key as a bracketed paste through the PTY file descriptor, never as an argument and never in `commands.json`; (6) it runs `scan_secrets` (`run.py:639`) over its output directory and the leader log. `real_provider.py` does not call the scan today; this phase adds it. The value is never printed.
- Scenarios:
  1. First run in the empty key home: `no model`, notice, `/model` empty hint.
  2. `/providers` → OpenAI API key → paste a made-up key → refused with the provider code and status.
  3. Paste the real key → saved → `/model` lists the OpenAI group → choose `gpt-5.5` → change line → one short prompt answers.
  4. Remove OpenAI → `no model` footer → prompt refused.
  5. OAuth home: `/providers` shows the signed-in rows; `/model` lists their models; switch between two providers; the start line and change lines show the pairs.
  6. Copy-code or device sign-in only if the user is present to approve it (manual step; record date and result, or "not run").
- Record: date, providers, model calls, results and any defect in a report `plans/reports/real-provider-acceptance-<date>-t1c-providers.md`, linked from this file.

## Tests first (runner scenarios)

No scenario signs in to Alibaba Token Plan: the user's subscription has expired and its key cannot be checked (D16).

Write each scenario in `real_provider_scenarios.py` first and run it against the phase 7 build; a failure is a product defect for the owning phase, not a scenario edit.

| Scenario | Proves |
|---|---|
| `providers_first_run` | Empty key home, no credential variable in the child: `no model` footer, rail notice, `/model` empty hint (criteria 1, 31) |
| `key_refused` | A made-up key gives the refusal with status and provider code; nothing saved (criterion 11 shape) |
| `key_saved_then_model` | The real key is saved; `/model` lists the OpenAI group; change line; one short prompt answers (criteria 12, 30, 48) |
| `remove_then_no_model` | Removal of the current provider: `no model`, prompt refused (criterion 28) |
| `default_model_persists` | After `key_saved_then_model`, quit and start `ask` again in the same home: the start line shows the saved default (criterion 53); after `remove_then_no_model`, a new session shows the fallback row (criterion 54) or no model |
| `oauth_rows_and_switch` | OAuth home copy: signed-in rows, model switch across providers, start and change lines (criteria 5, 44, 48) |
| `artifact_scan` | After every scenario: no key value in the output directory or the leader log |
| `docs_check` | Every changed doc: links resolve; each claim about commands, files and behavior matches source |

## Docs and links

| Target | Change |
|---|---|
| `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` | A T1c row after T1a that links this plan; note that it delivers the local part of M2 rows H17 (`H-SLASH-10`, `H-AUTH-10` transport over the local leader) and T2 (login and logout dialogs); gateway transport stays in H17 |
| `CLAUDE.md` | `ask` command notes: `/providers`, first run with `no model`, `ASK_FAUX=1` for the demo provider in the TUI, leader and `ask acp`, `/model` saves the default in `settings.json`, the `TEA_TRACE` warning |
| `internal/tui/README.md` | Status and owners of the new components (file has uncommitted T1a edits: edit only after they are committed) |
| `docs/tui-architecture.md` | Dialog slot, secret input rule and its `TEA_TRACE` exception, rail priority, model line, default model and fallback notice |
| `internal/settings/README.md` | Already updated in phase 3; check the `settings.json` text against the code |
| `.claude/skills/ask-tui-design/SKILL.md` | Gallery: provider box, login boxes, model line goldens |
| `plans/261009-2239-t1a-local-tui/design.md` and its artifacts | Apply section 12 of the new design (sketches, text, criteria); rebuild with its `make-design.py` |
| `docs/tui-style-guide.md` | No change unless the user decides the style-guide gaps of design section 9 |
| `docs/testing-llm-cassettes.md` | No change (no cassette added) |

## Files owned

`e2e/tui/real_provider.py`, `e2e/tui/real_provider_scenarios.py`, the report, and the docs in the table above.

## Verification

```
python3 e2e/tui/real_provider.py --scenario providers   # exact flags from the scout pass
python3 e2e/tui/test-assertions.py
go test ./...
```

Then check every link in the changed docs and the plan files.

## Rollback

Docs only, plus the runner scenarios; revert the commit. Delete the OAuth home copy in every case.

## Risks

| Risk | Mitigation |
|---|---|
| The real key leaks into an artifact | Runner scan fails the run and removes the files; only the variable name is printed |
| Token refresh changes the OAuth record | Copy only; the original stays untouched |
| A real provider changes its error code text | The refusal shows status and code; the test asserts the shape, not the code value |

## Todo

- [ ] Scout runner flags
- [ ] Scenarios 1-5 run and recorded; scenario 6 run or marked "not run"
- [ ] OAuth copy deleted
- [ ] Docs, roadmap, skill gallery, T1a design amendments
- [ ] Links checked
