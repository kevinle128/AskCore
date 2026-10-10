---
phase: 8
title: "Real-provider acceptance and docs"
status: in_progress
priority: P1
effort: "1d"
dependencies: [7]
---

# Phase 8: Real-provider acceptance and docs

Scout pass (2026-10-10): the runner needed three changes before any scenario could run.

- `real_provider.py` removed only `ASK_*` names, so a leader would inherit `OPENAI_API_KEY`. It now builds the child environment with `child_env` (no `ASK_*`, no name for which `run.is_credential_name` is true), and `leader_stop` uses it too.
- The runner imports `secret_values`, `scan_secrets` and `is_credential_name` from [run.py](../../e2e/tui/run.py).
- There was no binary: `real_provider.py` now has `ensure_ask`, which compiles `./cmd/tui` into `/private/tmp/ask-rp-bin/ask` (or `ASK_BIN`) the way `run.py` does.
- Flags: `python3 e2e/tui/real_provider_scenarios.py <scenario> [cols rows]` with `providers`, `oauth` or `acceptance` (both, then the scan). `RP_OUT` chooses the output directory. `PYTHONPATH` needs a directory that has `pyte`.

## Goal

One run against real providers must prove the key path and a new OAuth sign-in with no secret in any artifact.
The docs, roadmap and the T1a design must describe the implemented state.
The recorded run covers the key path and existing OAuth credentials; the new OAuth sign-in remains unverified.

## Real-provider run

- Runner: [real_provider.py](../../e2e/tui/real_provider.py) with [real_provider_scenarios.py](../../e2e/tui/real_provider_scenarios.py) and `scan_secrets` in [run.py](../../e2e/tui/run.py).
- Each run owns a unique `RUN_HOME` under `/private/tmp`, with separate key and OAuth homes.
  `fresh_home` resets only those owned homes.
  It copies `auth.json` and `settings.json` from the OAuth source, because token refresh can rewrite the credential file.
  It never copies a live socket, leader lock or process id.
- Secret handling is owned by `child_env` in `real_provider.py` and by `paste`, `oauth_home` and `artifact_scan` in `real_provider_scenarios.py`.
  The runner reads credentials before it removes credential variables from the child environment.
  It sends the key through the PTY descriptor, not a process argument.
  Every scenario entry point calls `cleanup` in its `finally` block.
  Cleanup stops owned sessions and leaders, reads the latest credential fields for the scan, then removes the owned homes.
  A failed process stop keeps the homes for cleanup.
- Scenarios:
  1. First run in the empty key home: `no model`, notice, `/model` empty hint.
  2. `/providers` → OpenAI API key → paste a made-up key → refused with the provider code and status.
  3. Paste the real key → saved → `/model` lists the OpenAI group → choose `gpt-5.5` → change line → one short prompt answers.
  4. Remove OpenAI → `no model` footer → prompt refused.
  5. OAuth home: `/providers` shows the signed-in rows; `/model` lists their models; switch between two providers; the start line and change lines show the pairs.
  6. A new OAuth sign-in requires the user to complete browser, copy-code or device approval.
     Record the date and result, or "not run".
     Result 2026-10-10: not run; phase acceptance remains incomplete.
- Record: [real-provider-acceptance-2026-10-10-t1c-providers.md](../reports/real-provider-acceptance-2026-10-10-t1c-providers.md).
  The record reports that scenarios 1 to 5 passed and the scan was clean.
  It uses existing OAuth credentials and does not prove a new OAuth sign-in.
  This review did not run a real-provider scenario.

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
| `artifact_scan` | Every scenario entry point scans its output directory and copied leader logs for registered and current credential fields |
| `docs_check` | Every changed doc: links resolve; each claim about commands, files and behavior matches source |

## Docs and links

| Target | Change |
|---|---|
| `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` | A T1c row after T1a that links this plan; note that it delivers the local part of M2 rows H17 (`H-SLASH-10`, `H-AUTH-10` transport over the local leader) and T2 (login and logout dialogs); gateway transport stays in H17 |
| `CLAUDE.md` | `ask` command notes: `/providers`, first run with `no model`, `ASK_FAUX=1` for the demo provider in the TUI, leader and `ask acp`, `/model` saves the default in `settings.json`, the `TEA_TRACE` warning |
| `internal/tui/README.md` | Status and owners of the new components (the file's earlier uncommitted edits are kept) |
| `docs/tui-architecture.md` | Dialog slot, secret input rule and its `TEA_TRACE` exception, rail priority, model line, default model and fallback notice |
| `internal/settings/README.md` | Already updated in phase 3; check the `settings.json` text against the code |
| `.claude/skills/ask-tui-design/SKILL.md` | Gallery: provider box, login boxes, model line goldens |
| `plans/261009-2239-t1a-local-tui/design.md` and its artifacts | Apply section 12 of the new design (sketches, text, criteria); rebuild with `artifacts/build-design.py` |
| `docs/tui-style-guide.md` | No change unless the user decides the style-guide gaps of design section 9 |
| `docs/testing-llm-cassettes.md` | No change (no cassette added) |

## Files owned

`e2e/tui/real_provider.py`, `e2e/tui/real_provider_scenarios.py`, the report, and the docs in the table above. `e2e/tui/README.md` was also edited, because its real-provider paragraph said keys reach `ask` through the environment.

## Verification

```
python3 e2e/tui/real_provider_scenarios.py acceptance   # PYTHONPATH has pyte; RP_OUT is optional
python3 e2e/tui/test-assertions.py
python3 e2e/tui/test-real-provider-safety.py
go test ./...
```

Then check every link in the changed docs and the plan files.
The safety controls use fake sessions and processes; they make no real-provider request.

## Rollback

Docs only, plus the runner scenarios; revert the commit. Delete the OAuth home copy in every case.

## Risks

| Risk | Mitigation |
|---|---|
| The real key leaks into an artifact | Runner scan fails the run and removes the files; only the variable name is printed |
| Token refresh changes the OAuth record | Copy only; the original stays untouched |
| A real provider changes its error code text | The refusal shows status and code; the test asserts the shape, not the code value |

## Todo

- [x] Scout runner flags
- [x] Scenarios 1-5 run and recorded; scenario 6 marked "not run"
- [ ] New OAuth sign-in run and recorded
- [x] OAuth copy deleted
- [x] Docs, roadmap, skill gallery, T1a design amendments
- [x] Links checked
