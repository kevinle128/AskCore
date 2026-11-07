---
phase: 2
title: "Layered settings read path"
status: completed
effort: 5h
depends_on: [1]
---

# Phase 2: Layered settings read path

## Context links

- [plan.md](./plan.md): S3, S5, S6, S7, S16, [Project key policy](./plan.md#project-key-policy), [Key disposition](./plan.md#key-disposition-h-conf-03-to-06)
- `internal/settings/settings.go:15-21` (keys, 1 MiB cap), `:73-117` (`ThinkingFold`), `:173-201` (`readSettings`, which needs mode 0600 and the owner, line 186)
- Pi: `settings-manager.ts:191-212` (merge), `:1100-1103` (global-only getter)

## Overview

Priority P1. Add the read side of the two layers: the project file reader, the merge, the key policy, the typed `Effective` value with sources and diagnostics. No writer changes and no consumer changes in this phase.

## Key insights

- The user file rules stay: 0600, owner, no symlink, 1 MiB (`settings.go:186-195`). A committed project file is usually 0644 and can belong to the repository checkout, so the project reader cannot use `readSettings`. It needs its own rules: the path, after the symlink is resolved, must be a regular file (checked with `Stat` before the open and with `f.Stat` after it), open with `O_NONBLOCK` so a FIFO cannot block, 1 MiB cap, JSON object.
- The project reader is called only with `Project.Trusted == true`. An untrusted project file is never opened. This is the fix for Pi `main.ts:669`.
- A read never creates a directory or a file in the project (Pi `CHANGELOG.md:3284`).
- Go's `json.Unmarshal` into a struct matches field names without regard to case. The typed read takes exact keys from the merged map, and the policy check folds case (S16).
- The user layer is read before the trust decision only for `defaultProjectTrust` (phase 4). That read is of the user file and is allowed.

## Requirements

Functional:
1. `settings.Project{Root string; Trusted bool}`. Empty `Root` means no project layer.
2. `settings.NewLayered(home, project)` builds the per-session value (decision S2: the trust decision arrives through the constructor).
3. `Layered.Load(ctx)` reads the user file (existing rules; missing file is empty), then the project file only when `Trusted` and `Root != ""`. Each load error becomes a diagnostic with layer and path, and that layer counts as empty. Load returns an error only for a context error.
4. `merge.go`: recursive merge for JSON objects; project arrays and scalars replace; project `null` is ignored. The merge works on raw JSON so unknown keys survive and keep their bytes.
5. Key policy (in `merge.go`, one table): user-only keys and deny-by-prefix keys are removed from the project layer before the merge. Each removed key gives one diagnostic. The match folds case.
6. `Effective` fields: `DefaultModel string`, `ThinkingFold ThinkingFold`, `DefaultProjectTrust ProjectTrustDefault` (`ask`, `always`, `never`; default `ask`). `Sources` maps each shipped key to `default`, `user` or `project`. A known key with a bad type or value gives the default and a diagnostic (`defaultModel` uses `validModelID`, `settings.go:119-129`).
7. A project key that differs from a shipped key only by case gives a diagnostic "unknown key; did you mean ...". Other unknown keys give no diagnostic (a newer build can own them).
8. `UserLayer(ctx)` returns the typed user-only view for the trust decision (no project read).

Non-functional: standard library only; no write; at most two file reads per `Load`.

## Architecture

```text
user file  --readSettings (0600 rules)-------------\
                                                     merge(user, filtered project) -> typed decode (exact keys) -> Effective{values, Sources, Diagnostics}
project file --readProjectSettings (only if Trusted)--policy filter (case-folded)--/
```

## Related code files

Create:
- `internal/settings/merge.go`: merge, key policy table, case-folded match.
- `internal/settings/project.go`: `Project`, project file reader.
- `internal/settings/layered_test.go`, `internal/settings/merge_test.go`.

Modify:
- `internal/settings/settings.go`: `Layered`, `NewLayered`, `Load`, `UserLayer`, `Effective`, `ProjectTrustDefault`, `Layer`, `Diagnostic`. Existing `SettingsStore` methods stay unchanged in this phase.

## Implementation steps

1. Write "Tests Before".
2. Add `merge.go` with the merge and the policy table from plan.md.
3. Add `project.go` with the project reader.
4. Add the `Layered` types and `Load` in `settings.go`. Reuse `parseThinkingFold` (`settings.go:89-99`) and `validModelID`.
5. Regression gate.

## Tests Before (written first, fail first)

| Test | Checks |
|---|---|
| `TestLayeredProjectOverridesUser` | user `defaultModel` a, project b gives b, source `project` |
| `TestMergeNestedObjectsAndReplacesArrays` | nested object keys merge; arrays replace; project `null` ignored |
| `TestUntrustedProjectSettingsNeverOpened` | untrusted project with an invalid file and mode 000: no diagnostic, no error; the same file with `Trusted` gives a diagnostic with its path |
| `TestLoadNeverCreatesProjectDir` | load with a missing `.ask/` leaves the project tree unchanged (walk before and after) |
| `TestProjectCannotSetUserOnlyKeys` | project `defaultProjectTrust: always` is dropped with one diagnostic |
| `TestTrustedProjectOverridesThinkingFold` | trusted project `thinkingFold: collapsed` wins over the user value; `Sources` says `project`; an untrusted project gives the user value |
| `TestProjectDeniedKeysDroppedWithDiagnostic` | each deny entry, top level and nested, is dropped; other project keys still apply |
| `TestDeniedKeyMatchIgnoresCase` | `DefaultProjectTrust`, `HTTPPROXY` in the project are dropped |
| `TestInvalidKnownValueGivesDefaultAndDiagnostic` | `thinkingFold: 3`, `defaultModel: " x"`, `defaultProjectTrust: "maybe"` |
| `TestCorruptSettingsReported` | corrupt user file: diagnostic with path, user layer empty, project still loads when trusted |
| `TestProjectReaderRefusesSpecialFiles` | a FIFO, a directory and a 2 MiB file give a diagnostic and do not block (test has a 2 s deadline) |
| `TestProjectReaderAcceptsCommittedMode` | mode 0644 file of the current user loads |
| `TestUserLayerReadsUserFileOnly` | `UserLayer` with a project present never opens it (mode 000 project file, no error) |
| `TestSourcesNameDefaultForMissingKeys` | empty files give `default` for each shipped key |

## Refactor

None. The new code reuses the read helpers.

## Tests After

All "Tests Before" pass. `internal/settings/settings_test.go` and `thinking_fold_test.go` pass unchanged.

## Regression gate

```text
go test -race ./internal/settings
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Todo

- [ ] Tests Before
- [ ] `merge.go`
- [ ] `project.go`
- [ ] `Layered` and `Effective` in `settings.go`
- [ ] Regression gate green

## Success criteria

- An untrusted project file is never opened (test with mode 000 proves it).
- `merge.go` exists, so the README promise (`internal/settings/README.md:8,94`) is true.

## Risk assessment

| Risk | L x I | Mitigation |
|---|---|---|
| A symlinked project `settings.json` points outside the tree | M x L | Read is allowed after trust (Pi follows links); the target must be a regular file within the cap |
| Raw-JSON merge changes number formatting of unknown keys | L x L | Merge keeps `json.RawMessage` bytes for leaves |

## Security considerations

The deny list is applied whatever the trust state. Diagnostics never include file content, only path, key and a fixed text.

## Rollback

Revert the commit. No consumer uses `Layered` yet.

## Next steps

Phase 3 builds the writer on these types and moves the existing consumers to them.
