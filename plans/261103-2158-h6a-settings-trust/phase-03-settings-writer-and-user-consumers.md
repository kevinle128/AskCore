---
phase: 3
title: "Settings writer discipline and user consumers"
status: completed
effort: 5h
depends_on: [2]
---

# Phase 3: Settings writer discipline and user consumers

## Context links

- [plan.md](./plan.md): S3, S4, S11
- `internal/settings/auth.go:268-336` (`commit`: temp file in `s.home` at line 272, fault seams `s.ops.write`, `syncFile`, `closeFile`, `rename` at lines 286-312, `ErrIndeterminate` after rename)
- `internal/settings/settings.go:41-71` (`DefaultModel`, `SetDefaultModel`), `:101-117` (`ThinkingFold`), `:131-160` (`locked`)
- Consumers: `internal/app/module_acp.go:93-96,129,155-156`, `cmd/tui/thinking_fold.go:19-29`
- DeepSeek report section 1 "Write policy" (validate, shadow check, rollback)

## Overview

Priority P1. Add one writer, `Layered.Update(ctx, scope, edit)`, with Ask's commit and DeepSeek's discipline. Move `SetDefaultModel`, `DefaultModel` and `ThinkingFold` onto the layered code so that one read model and one writer exist. Behavior for users does not change in this phase (no project layer is passed yet).

## Key insights

- `AuthStore.commit` creates its temp file in the home (`auth.go:272`). A project write needs the temp file in `<root>/.ask/` so that `rename` stays in one directory. Generalize to a commit that takes the directory and the file mode, and keep `AuthStore.commit` as a wrapper so the credential path does not change.
- The project lock sidecar is in the Ask home (S11), so no lock file appears in the repository.
- "Rollback" in Ask: no live apply exists. A failed commit before rename keeps the old bytes (already true); the caller's view changes only after a successful commit; a failure after rename is `ErrIndeterminate`, as for credentials.
- Shadow check: a user-scope write of a key that the trusted project layer sets is refused with `ErrShadowed`. This is what phase 4 uses for `/model` save.
- The project write path has no product caller in H6a. It ships because the user requires "refuse writes to project settings when untrusted" and its tests pin that rule. The first caller is a later settings UI.

## Requirements

1. `Scope`: `user` or `project`.
2. `Update(ctx, scope, edit func(fields) error)`:
   - project scope with `Trusted == false` or no root: `ErrUntrusted`, nothing touched (no `.ask/` created, no lock taken).
   - lock: user scope `settings.json.lock` in the home (existing); project scope the S11 sidecar in the home.
   - fresh read under the lock; a corrupt or unsafe target file: refuse with `ErrUnsafeStore` and a diagnostic; never overwrite.
   - apply `edit` to a copy; a project-scope edit of a user-only or denied key: refuse.
   - validate the whole candidate: it decodes as an object and every shipped key it holds is valid; else refuse with the key in the diagnostic.
   - user scope: if the trusted project layer sets any key that the edit changed, refuse with `ErrShadowed` naming the key.
   - no change after the edit: no write.
   - commit: user file 0600 in the home; project file keeps its current mode (0644 when new), directory `<root>/.ask` created 0755 only now.
3. `SettingsStore.SetDefaultModel`, `DefaultModel`, `ThinkingFold` keep their signatures and behavior, implemented through `Layered` with no project. (Callers in phase 4 move to `Layered` directly where they need the project.)
4. `cmd/tui/thinking_fold.go` keeps reading the user file for the first frame. A trusted project may override `thinkingFold`; the client applies the effective value from the session state (phase 6).

## Architecture

```text
Update(scope, edit)
  scope=project && !Trusted -> ErrUntrusted
  lock(sidecar in home) -> fresh read -> corrupt? refuse
  copy+edit -> policy check -> validate candidate -> shadow check (user scope)
  -> changed? commitIn(dir, path, bytes, mode) -> view updated
```

## Related code files

Modify:
- `internal/settings/auth.go`: split `commit` into `commitIn(ctx, dir, path, b, mode)`; `commit` calls it with `s.home` and 0600.
- `internal/settings/settings.go`: `Update`, `Scope`, `ErrShadowed`, `ErrUntrusted`; `SettingsStore` methods delegate.
- `internal/settings/settings_test.go`, `internal/settings/thinking_fold_test.go`: extend.
- `cmd/tui/thinking_fold.go`: no logic change; comment states that the user file gives the first frame only (one line).

Create:
- `internal/settings/update_test.go`.

## Implementation steps

1. Write "Tests Before".
2. Refactor `commit` to `commitIn` (Refactor section). Run the auth tests before any other change.
3. Add `Update` and its errors.
4. Move `SettingsStore` methods to delegate.
5. Regression gate.

## Tests Before (written first, fail first)

| Test | Checks |
|---|---|
| `TestUpdateRefusesProjectWriteWhenUntrusted` | `ErrUntrusted`; project tree unchanged; no lock file created |
| `TestCorruptSettingsNotOverwrittenAndReported` | user and project: corrupt file bytes the same after `Update`; diagnostic names the path |
| `TestUpdateRefusesInvalidCandidate` | an edit that sets `thinkingFold: 7` is refused; an existing invalid shipped key also blocks the write and is named |
| `TestSaveDefaultRefusedWhenProjectSetsDefaultModel` | trusted project sets `defaultModel`; user-scope set returns `ErrShadowed`; user file unchanged |
| `TestUpdateKeepsUnknownAndExternalKeys` | an external process adds a key between two updates; both keys and unknown keys survive |
| `TestUpdateKeepsOldBytesWhenRenameFails` | fault in `rename`: old bytes, temp file removed; fault in directory sync: `ErrIndeterminate` |
| `TestProjectUpdateWritesTempInProjectDir` | temp file is created in `<root>/.ask/`; lock sidecar is in the home; new file mode 0644; existing mode kept |
| `TestProjectUpdateRefusesUserOnlyKey` | project-scope edit of `defaultProjectTrust` is refused |
| `TestUpdateNoChangeNoWrite` | same value: file mtime unchanged |

Existing tests that must still pass unchanged: `internal/settings/auth_test.go`, `cmd/tui/auth_fsync_test.go`, `internal/settings/settings_test.go`, `internal/settings/thinking_fold_test.go`, `cmd/tui/thinking_fold_test.go`.

## Refactor

`commit` to `commitIn`: the only change is the temp directory and the mode parameter. Protected by `internal/settings/auth_test.go` fault tests and `cmd/tui/auth_fsync_test.go`. Run them first and after.

## Tests After

All tests above pass. `go test ./internal/app` passes with no change to `module_acp.go` behavior.

## Regression gate

```text
go test -race ./internal/settings
go test ./internal/app ./cmd/tui -run 'ThinkingFold|Default|Auth'
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Todo

- [ ] Tests Before
- [ ] `commitIn` refactor with auth tests green
- [ ] `Update`
- [ ] `SettingsStore` delegation
- [ ] Regression gate green

## Success criteria

- One writer for both files. Credential tests unchanged and green.
- A corrupt file keeps its bytes in every refused path.

## Risk assessment

| Risk | L x I | Mitigation |
|---|---|---|
| Commit refactor changes credential durability | L x H | Wrapper keeps arguments; fault tests run before and after |
| Whole-candidate validation blocks `/model` save over an unrelated bad key | M x L | Diagnostic names the key and the path; the user fixes the file. Matches "never overwrite a file we cannot fully read" |

## Security considerations

No project write without trust. A project write cannot set `defaultProjectTrust` or denied keys.

## Rollback

Revert. File formats did not change.

## Next steps

Phase 4 passes a real `Project` per session and uses `ErrShadowed` for `/model` save.
