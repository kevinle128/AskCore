---
phase: 1
title: "Trust store and decision core"
status: completed
effort: 5h
depends_on: []
---

# Phase 1: Trust store and decision core

## Context links

- [plan.md](./plan.md): decisions S1, S2, S10, S12, [Gated paths](./plan.md#gated-paths)
- Pi report: `plans/reports/researcher-261103-2153-pi-settings-trust.md` section 2
- `internal/workspace/README.md` (planned `trust.go`, line 11 and 43), `internal/workspace/path.go`
- `internal/settings/auth.go:268-336` (`commit`), `internal/settings/lock.go:13-52` (`lockSidecar`), `internal/settings/paths.go:16-67` (`ensureHome`, `checkEntry`), `internal/settings/lock_unix.go:22-28` (`openNoFollow`)

## Overview

Priority P1. Build the parts of the trust gate that have no transport and no UI: the canonical project root, the gated-file check, the `trust.json` store, nearest-ancestor lookup, and the pure decision function. The result is runnable through unit tests only.

## Key insights

- `trust.json` lives in the Ask home, so the existing `AuthStore.commit` (temp file in the home, `auth.go:272`) works for it without change. The commit refactor waits for phase 3.
- `internal/workspace` stays stdlib plus `norm` (`internal/workspace/README.md:47`). It never reads `trust.json`: it receives the parsed map. This keeps the decision a pure function that tests can drive with tables.
- Pi's `$HOME` bug: with cwd = `$HOME`, the Ask home `~/.ask` would look like a project `.ask/` (Pi `CHANGELOG.md:1536`). The root resolver returns "no project layer" when `<root>/.ask` resolves to the canonical Ask home.
- The gated check uses `Lstat` only, so it never opens a project file and never follows a link out of the tree.

## Requirements

Functional:
1. `workspace.ProjectRoot(cwd, askHome)` returns the canonical root (absolute, symlinks resolved; when `EvalSymlinks` fails, the clean absolute path) and a flag "has project layer" that is false when `<root>/.ask` is the canonical Ask home.
2. `workspace.GatedFiles(root, userHome)` returns the sorted list of gated paths that exist (relative to root, with ancestor `.agents/skills` shown as absolute). It skips `<userHome>/.agents/skills`. It returns an error only for an I/O error other than "not exist"; the caller treats that error as "gated" (fail closed).
3. `settings.TrustStore` (new `internal/settings/trust.go`): `Read(ctx)` takes no lock and creates nothing (each replace is atomic; same rule as `ModelCache.Read`, `internal/settings/README.md:82`). It returns the map canonical path to bool and a typed error for a corrupt or unsafe file; a missing file or a missing home is an empty map. Only `Update` locks. `Update(ctx, edit)` takes `trust.json.lock`, reads fresh, refuses when the file is corrupt or unsafe (never overwrites), applies the edit, and commits 0600 with sorted keys and a final newline. A `nil` edit value deletes the key.
4. `workspace.NearestEntry(entries, root)` walks from root to `/`; the first key that exists wins, so a child `false` beats a parent `true`.
5. `workspace.Decide(in)` takes: override (approve, deny, none), has-gated-files, store state (ok, corrupt), nearest entry (none, true, false; and whether it is on an ancestor), `defaultProjectTrust` (ask, always, never), UI available. It returns trusted, source, and "needs prompt". Order: override; no gated files means trusted; corrupt store means untrusted; saved entry; default `always`/`never`; `ask` with UI means needs prompt; otherwise untrusted.
6. Source names are stable wire words: `flag`, `no_gated_files`, `store_error`, `saved`, `saved_ancestor`, `default_always`, `default_never`, `prompt`, `no_ui`.

Non-functional: no new import edge; no project write; trust file 0600 in a 0700 home; lock wait as `lockWait` (`lock.go:11`).

## Architecture

```text
cwd --ProjectRoot--> root, hasLayer
root --GatedFiles(Lstat)--> []gated
trust.json --TrustStore.Read--> map | ErrTrustStoreCorrupt
map, root --NearestEntry--> entry
inputs --Decide--> {trusted, source, needsPrompt}
```

## Related code files

Create:
- `internal/workspace/project.go`: `ProjectRoot`, `GatedFiles`, the gated-path list (one list).
- `internal/workspace/trust.go`: `NearestEntry`, `Decide`, input and result types, source words.
- `internal/workspace/project_test.go`, `internal/workspace/trust_test.go`.
- `internal/settings/trust.go`: `TrustStore`, `NewTrustStore(home)`, `Read`, `Update`, `ErrTrustStoreCorrupt` (wraps `ErrUnsafeStore`).
- `internal/settings/trust_test.go`.

Modify: none.

## Implementation steps

1. Write the failing tests in "Tests Before".
2. Add `project.go`: canonical root, Ask home comparison (resolve both), the gated list as one ordered slice of entries with "dir or file" and "in .ask or ancestor walk".
3. Add `trust.go` in workspace: `NearestEntry` with `filepath.Dir` until it stops changing; `Decide` as a straight sequence of returns that follows the order above.
4. Add `internal/settings/trust.go`, reusing `ensureHomeShared`, `lockSidecar`, `checkEntry`, `openNoFollow`, and `AuthStore.commit` the same way `SettingsStore` does (`settings.go:133-160`). Values: `true`, `false`; a stored `null` reads as no entry.
5. Run the regression gate.

## Tests Before (written first, fail first)

| Test | Package | Checks |
|---|---|---|
| `TestProjectRootIsCanonicalCwd` | workspace | a symlinked cwd gives the target path; a missing cwd gives the clean absolute path |
| `TestAskHomeIsNotAProject` | workspace | cwd = parent of Ask home gives `hasLayer=false`; also through a symlinked home |
| `TestGatedFilesList` | workspace | each gated entry alone is found; `AGENTS.md`, `CLAUDE.md`, empty `.ask/`, `.ask/notes.txt` are not |
| `TestAgentsFileIsNotGated` | workspace | `AGENTS.md` and `CLAUDE.md` alone give an empty list |
| `TestGatedFilesSkipsUserAgentsSkills` | workspace | `<userHome>/.agents/skills` is ignored; a project ancestor one is found |
| `TestGatedFilesNeverOpensFiles` | workspace | a gated `settings.json` with mode 000 is still detected (Lstat only) |
| `TestNearestEntryChildBeatsParent` | workspace | child false, parent true gives false; only parent true gives true from the ancestor |
| `TestDecideOrder` | workspace | table over every input combination in Requirements 5; `default_always` with corrupt store gives untrusted |
| `TestPlainFolderTrustedWithoutDecision` | workspace | no gated files, no entry, default `never` gives trusted `no_gated_files` |
| `TestTrustStoreMissingFileIsEmpty` | settings | no file, empty map, no error, no file created by `Read` |
| `TestCorruptTrustStoreGivesUntrusted` | settings + workspace | invalid JSON, a non-object, a non-bool value, mode 0644, a symlink: `Read` returns `ErrTrustStoreCorrupt`; `Decide` gives untrusted `store_error` |
| `TestTrustStoreUpdateRefusesCorruptFile` | settings | `Update` on a corrupt file returns the error and the bytes stay the same |
| `TestTrustStoreUpdateKeepsOtherEntries` | settings | two writers in sequence keep each other's keys; `nil` deletes; output sorted with final newline, mode 0600 |
| `TestTrustStoreConcurrentUpdates` | settings | 8 goroutines add distinct keys; all keys present (run with `-race`) |

## Refactor

None in this phase. Shared helpers in `internal/settings` are reused, not changed.

## Tests After

All "Tests Before" pass. Existing `internal/workspace` and `internal/settings` tests pass unchanged.

## Regression gate

```text
go test -race ./internal/workspace ./internal/settings
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Todo

- [ ] Tests Before written and failing
- [ ] `internal/workspace/project.go`
- [ ] `internal/workspace/trust.go`
- [ ] `internal/settings/trust.go`
- [ ] Regression gate green

## Success criteria

- Every test above passes, also with `-race`.
- `rg -n 'AskCore/internal' internal/workspace/*.go` shows no new import.

## Risk assessment

| Risk | L x I | Mitigation |
|---|---|---|
| `EvalSymlinks` case on macOS does not normalize case | M x L | A miss asks again (fail closed); documented in phase 7 |
| A gated check I/O error (permission denied on an ancestor) | L x M | Treat as gated: the user decides, nothing loads silently |

## Security considerations

`trust.json` is owner-only and refused when unsafe. Trust is not a boundary against a tool that edits the home (H-SEC-03).

## Rollback

Delete the four new files and their tests. Nothing else uses them yet.

## Next steps

Phase 2 uses `ProjectRoot` output as the project directory of the settings layer.
