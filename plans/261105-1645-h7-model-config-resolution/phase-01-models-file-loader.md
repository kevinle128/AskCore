---
phase: 1
title: "models.json loader"
status: pending
priority: P1
effort: "1.5d"
dependencies: []
---

# Phase 1: models.json loader

## Goal

`internal/settings` reads `<ASK_HOME>/models.json` (JSON with comments and trailing commas) through the same hardened byte reader as `settings.json`, decodes it into typed rows, reports every problem as a `Diagnostic` that holds no user text, drops only a bad row, and keeps the last valid copy in memory.

<!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->
Only the user file exists. Ask does not read `.ask/models.json` (user decision Q2b, 2026-11-06). The name stays in the gated list (`internal/workspace/project.go:24`), and no code reads it.

## Context

- Decisions: D1 (user file only, Q2b), D4, D5, M5, M11, M17 ([decisions.md](./decisions.md)).
- User file rules today: `readUserFile` checks the home and the entry (`internal/settings/settings.go:346-365`), then `readSettings` opens with `openNoFollow`, checks the opened file with `f.Stat()` (regular, 0600, owner), reads at most 1 MiB, and ends in `json.Unmarshal` into a map (`internal/settings/settings.go:156-184`; limit at `settings.go:25`). `openNoFollow` adds `O_NOFOLLOW` only (`internal/settings/lock_unix.go:22-28`).
- `Diagnostic{Layer, Path, Key, Text}` must never hold file content (`internal/settings/settings.go:214-223`).
- Store rule for provider ids: lower case `a-z`, `0-9` and `-` only (`validProvider`, `internal/settings/auth.go:100-109`). `Store.Read` refuses any other id with `ErrUnsafeStore` (`internal/settings/auth.go:362-364`).
- Lint: `settings-stdlib-only` has no test exception (`.golangci.yml:155-160`). Tests use `testing` only.
- Pi schema fields: `providers.<id>{baseUrl, api, apiKey, headers, compat, models[], modelOverrides{}}`; model fields `id, name, api, reasoning, thinkingLevelMap, input, cost, contextWindow, maxTokens, headers, compat` (omp report Q1, inventory row H-PROV-17).

## Data flow

<!-- Red Team 2026-11-06: Finding 9 -->
```
readUserFileBytes(home, "models.json")      shared with settings.json
   open (O_NOFOLLOW|O_NONBLOCK) -> f.Stat() of the opened file -> stamp + bytes (<=1 MiB)
-> stripJSONC -> json.Decoder (UseNumber) -> walk known keys
-> ModelsFile{Providers map[id]ProviderEntry} + []Diagnostic
-> ModelsFiles (one per process): read outside the lock, store under the lock if newer
   -> {generation, lastValid, problems}
```

## Requirements

<!-- Red Team 2026-11-06: Finding 9 -->
1. **Shared byte reader.** Split `readSettings` into `readUserFileBytes(home, name string) (data []byte, info os.FileInfo, err error)` and a JSON object decoder. `readUserFile` and the models loader both call the byte reader, so the home check, the entry check, the open flags, the `f.Stat()` checks and the size limit exist once. The byte reader opens with `O_NOFOLLOW` and `O_NONBLOCK`, so a FIFO in place of the file cannot block the process; the existing `IsRegular` check of the opened file then refuses it. `settings.json` behavior does not change.
2. `stripJSONC` removes `//` and `/* */` comments outside strings and a trailing comma before `}` or `]`. Problem texts can give a line number, never content.
3. Types (exported, stdlib only): `ModelsFile`, `ProviderEntry`, `ModelEntry`, `ModelOverride`, `CostEntry`. Value fields (`apiKey`, header values) stay raw strings; this package does not interpret `$` or `!`.
4. Cross-field rules (from omp `models-config.ts:52-123`, adapted):
   - A provider with `models` that is not a built-in provider needs `baseUrl` and `apiKey` (a local server takes a literal such as `"ollama"`). `api` is on the provider or on each model.
   - A provider without `models` needs at least one other field.
   - `contextWindow` and `maxTokens` are positive integers when present. `input` items are `text` or `image`. Thinking map keys are protocol levels; a value is a string or `null`.
   - An empty `apiKey` string is a problem.
   The built-in provider id list arrives as a constructor argument (a `[]string`), because `settings` cannot import `providers`.
   <!-- Red Team 2026-11-06: Finding 6 -->
   - A provider id must match the store rule (`validProvider`: lower case `a-z`, `0-9`, `-`). Another id is a provider problem, and that provider is dropped, so `Store.Read` never sees an id that it refuses.
<!-- Red Team 2026-11-06: Finding 15 -->
5. **Diagnostic keys hold no user text.** `Diagnostic.Key` uses schema segments and positions only, for example `providers[2].models[0].contextWindow`. A provider id, a model id and an unknown key name are user text and never appear. An unknown key gives the text `unknown key; it is ignored`, plus `did you mean <schemaKey>?` when the unknown name is within two edits of a schema key (the suggestion is a schema name, not user text). This keeps the typo help of D5.
6. A bad model row drops that row only, with one problem. A bad provider entry drops that provider only. A file that is not valid JSONC, or breaks the file rules, is a file problem.
<!-- Red Team 2026-11-06: Finding 9 -->
7. **`ModelsFiles`** (one value per process for the user file): `Load() (file ModelsFile, problems []Diagnostic, generation uint64)`.
   - Read without the lock: the byte reader gives bytes and the `os.FileInfo` of the opened file. The stamp (mtime with nanoseconds, size) comes from that `FileInfo`, so the stamp always belongs to the bytes that were read.
   - Under the lock: when the stamp equals the stored stamp, return the stored result. When it is newer, store the new result and increase `generation`. A valid parse replaces the last valid copy. A file problem keeps the last valid copy and adds the problem `the last valid copy is used`. A missing file clears the copy (the user removed it) and increases `generation`.
   - The caller compares `generation` to decide whether it must rebuild. The lock covers only the compare and the store; there is no I/O under it.
8. The user file uses the user file rules (M11; user QB 2026-11-05: 0600 required).

## Files

| Action | Path | Notes |
|---|---|---|
| Create | `internal/settings/models_file.go` | Types, decode, cross-field rules, provider id rule |
| Create | `internal/settings/jsonc.go` | `stripJSONC` |
| Create | `internal/settings/models_files.go` | `ModelsFiles` memory with generation; file name constant `models.json` |
| Create | `internal/settings/models_file_test.go`, `internal/settings/jsonc_test.go`, `internal/settings/models_files_test.go` | stdlib `testing` only |
| Modify | `internal/settings/settings.go` | Split `readSettings` into the byte reader and the decoder; add `O_NONBLOCK` <!-- Red Team 2026-11-06: Finding 9 --> |
| Modify | `internal/settings/settings_test.go` | FIFO and symlink cases for `settings.json` too |

<!-- Red Team 2026-11-06: Finding 9 -->
Coordination: phase 6 also changes `settings.go` (the `enabledModels` key). Phase 6 starts after phase 5, which waits for this phase, so the two edits never run in parallel.

## Steps

1. Split `readSettings`; run the existing settings tests before any other change.
2. Write `stripJSONC` with a table test: comments in strings stay, escaped quotes, `*/` inside a string, trailing commas in nested arrays, CRLF.
3. Write the decoder walk with `json.Decoder` and `UseNumber`, so `128000.5` is a problem and not a silent truncation.
4. Write the cross-field rules, the provider id rule and the problem texts with schema paths.
5. Write `ModelsFiles` with the stamp from the opened file and the generation.

## Tests

- `TestStripJSONCKeepsStringsAndDropsComments`, `TestStripJSONCTrailingCommas`.
- `TestModelsFileParsesPiExample` (the Pi docs Ollama example with comments).
- `TestModelsFileUnknownKeyIsProblemAndLoads` (the text suggests `contextWindow` for `contxtWindow`).
- `TestModelsFileBadRowDropsOnlyThatRow`.
- `TestModelsFileProviderIDMustMatchStoreRule` (`Local`, `my_local` and `a/b` are provider problems). <!-- Red Team 2026-11-06: Finding 6 -->
- `TestModelsFileDiagnosticsHoldNoUserText`: a marker string is a value, a provider id, a model id and an unknown key name; no `Diagnostic` field contains it. <!-- Red Team 2026-11-06: Finding 15 -->
- `TestModelsFilesKeepsLastValidCopyAfterBadEdit` (write valid, load, write broken, load: rows equal the first copy plus two problems; generation increases once per change).
- `TestModelsFilesSameSizeEditIsRead` (two edits with the same size and a new mtime: the second load reads). <!-- Red Team 2026-11-06: Finding 9 -->
- `TestModelsFilesFIFOIsRefusedWithoutBlocking`, `TestModelsFilesSymlinkIsRefused`, and the same two cases for `settings.json`. <!-- Red Team 2026-11-06: Finding 9 -->
- `TestModelsFilesUserFileModeIsChecked` (0644 user file gives a file problem).
- `TestModelsFilesMissingFileClearsCopy`.

```
go test ./internal/settings -run 'TestStripJSONC|TestModelsFile|TestReadSettings' -count=1
go test -race ./internal/settings -run 'TestModelsFiles' -count=1
go test ./internal/settings -count=1
go vet ./internal/settings
```

## Validation

- Lint passes with `settings-stdlib-only`.
- The existing `settings.json` tests pass unchanged after the reader split.

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| The reader split changes `settings.json` behavior | Low x High | Split first, run the whole settings suite before other work |
| JSONC edge case corrupts a string | Low x Med | Table test of string states |
| Schema too strict for Pi files | Med x Low | Unknown keys load (D5); only invalid values are problems |
| A Pi file uses an id outside the store rule | Med x Low | A clear provider problem names the rule; the rest of the file loads |

## Rollback

Revert the commit. The reader split keeps `settings.json` behavior, so a revert of only the split is also safe. No caller of the models loader exists until phase 5.
