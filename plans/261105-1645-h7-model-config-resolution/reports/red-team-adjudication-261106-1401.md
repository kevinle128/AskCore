# Red-team adjudication: H7 model configuration and resolution

Date: 2026-11-06. Four reviewers (security, failure modes, assumptions, scope). Raw reports:
[security](./red-team-security-261106-1354.md), [failure](./red-team-failure-261106-1354.md), [assumptions](./red-team-assumptions-261106-1354.md), [scope](./red-team-scope-261106-1354.md).

32 raw findings were merged into 15. Every merged finding has `file:line` evidence. The lead checked the load-bearing citations against the code at `97c3052`: `internal/auth/service.go:247-258`, `internal/app/module_acp.go:303-314`, `internal/settings/auth.go:100-109`, `internal/acp/session_options.go:36-40`. Each matches the claim.

Severity: 3 Critical, 9 High, 3 Medium. Disposition: 13 Accept, 2 Accept pending a user decision.

| # | Finding | Sev | Sources | Disposition | Phases |
|---|---|---|---|---|---|
| 1 | A models.json key fails `keySnapshot` for every custom provider (`service.go:247-258`); the local-provider trace fails | Critical | assum 1 | Accept: a config snapshot that binds only `ConfigKey.Endpoint == m.BaseURL` for a provider the file defines; test with a provider that has no registered method | 2 |
| 2 | A provider with only a file key is never usable: `providerUsable`, `firstUsable`, saved default, `setModel` guard and `usableOnly` read `Status` only (`module_acp.go:303-314`, `default_model.go:60-65`, `ask_methods.go:192,263`) | Critical | fail 1 | Accept: `usable` takes the session view; leader test covers start choice, saved default, `setModel`, `usableOnly` | 5, 9 |
| 3 | Project `$NAME` / `${NAME}` sends any env var to a host the project chose (`service.go:145-154`, `openai/profiles.go:129-136`) | Critical | sec 1 | Accept pending QE: project values are literals only (recommended) | 1, 2, 5 |
| 4 | Project `apiKey` on an existing provider replaces the user's env key (account takeover, same host) | High | sec 3, assum 6 | Accept pending QF: refuse and report (recommended) | 5 |
| 5 | Project `models[]` row replaces a built-in row and changes `api`/`compat`/model headers (`compat.go:31-38`); account filter runs before a user `baseUrl` change (`model_set.go:55-58`) | High | sec 4, assum 5 | Accept: project `models[]` may only add new ids; existing rows change only by `modelOverrides` with the phase 3 limits; model headers on an existing provider are refused in the project layer; a provider whose `baseUrl` a layer changed has unknown account access | 3, 5 |
| 6 | "Existing provider" is defined three ways and compared by exact case while resolution folds case; ids outside `[a-z0-9-]` fail in `Store.Read` (`settings/auth.go:100-109`) | High | sec 2, assum 2 | Accept: provider ids must match the store rule (lower case `[a-z0-9-]`), bad id is a provider problem; one definition of existing provider (built-in ids, registered auth methods, user-file providers); two rows that match one `provider/id` are ambiguous | 1, 4, 5 |
| 7 | Colon-tagged ids (`qwen3-coder:30b`) give "unknown level" at `parseArgs` before rows exist; `ResolvePattern` repeats `findIn` (`catalog.go:126-151`, `ask_methods.go:121-134`) | High | scope 5, 6; assum 3 | Accept: try the full id first; split `:level` only when the rest resolves; `ErrUnknownLevel` only when the base resolves and the suffix is not a level word; exact steps call `findIn`; `parseModelID` reuses the parser; level check moves after resolution | 4, 6 |
| 8 | View rebuilt on every request (breaks D14, file I/O under `loadMu`); key and headers looked up by provider name only, so a frozen row and a new file mix (`module_auth.go:19-46`, `model_refresh.go:187-213,272-273`) | High | scope 3; fail 4, 6 | Accept: build the view at session start, `/model` open and `setModel`, store it in the session; `AuthRunner` reads the stored table by `(Provider, API, BaseURL)`; no match gives "model configuration changed; choose the model again"; a wire flag for a row no longer in the config instead of `notInAccount` | 5 |
| 9 | Phase 1 copies the hardened readers (`settings.go:156-184`, `project.go:44-89`); false lock claim (`settings.go:249`); two stamp layers; stamp taken before open | High | scope 1, 2, 4; sec 7 | Accept: split each reader into a byte reader and a decoder and share them; stamp from `f.Stat()` of the opened file; read outside the lock, store under it; one `ModelsFiles` per process with a generation; FIFO and symlink tests | 1, 5 |
| 10 | `!command`: a cancelled first caller is cached as a failure and starts the 30 s backoff for all sessions (`sandbox/run.go:16-22`); the forget after 401 races the next request (`stream.go:84-95`) | High | fail 3, 5 | Accept: the cache entry owns the run on `WithTimeout(WithoutCancel(ctx), 10s)`; each caller also selects on its own `ctx.Done()`; a caller cancellation is never cached; a relay stream calls `Forget` before it forwards the terminal 401; `-race` tests | 2, 5 |
| 11 | Older leader drops `--model`/`--models` with no error (`session_options.go:36-40`); new fields have no bounds; cycling needs two RPCs (`acp.go:60-70`) | High | fail 2, 8; scope 7 | Accept: host echoes applied options (`omitempty`), client fails the start on a mismatch; bounds and level check in `flagsFromMeta`; raw patterns stored with the session flags; `ThinkingLevel` (`omitempty`) on `ACPModelRequest` under one guard; ignore cycling presses while a switch runs | 6, 8 |
| 12 | `ask -p` Token Plan branch skips grammar, ModelSet and models.json (`headless_faux.go:82-83`, `anthropic/provider.go:50-59`) | High | assum 4 | Accept: Token Plan goes through the same `ModelSet` path; only `faux` stays on the switch | 6 |
| 13 | `--api-key` accepted at any config endpoint, so a project endpoint receives it (`service.go:138-140`) | Medium | sec 5 | Accept: only for a provider from the user layer | 2 |
| 14 | `saveDefault` writes a project-layer row into user settings (`settings.go:569-583`); last valid copy is memory only and lost on leader replacement (`leader/README.md:68`); no `ask acp` last-valid test | Medium | fail 7, 9; assum 7 | Accept: refuse to save a project-layer row as the user default with a fixed reason; narrow the acceptance wording to a process that loaded a valid copy; rollback section documents the manual fix for a saved default; add `TestACPKeepsLastValidModelsFile` | 5, 9, plan |
| 15 | Diagnostics print user key names (breaks the `Diagnostic` promise); project reader follows a symlink out of the project root (`project.go:43-63`) | Medium | sec 6 | Accept: refuse a resolved path outside the project root; `Diagnostic.Key` prints schema segments and positions for user ids | 1 |

## Merged without a separate row

- `KeySource` repeats the order of `ResolveWith` (scope 8, assum 8). The planner added it; the user did not request it. Accept the cheaper fix: `KeySource` and `ResolveWith` share one source-selection function with a dry-run flag, and M3 text matches phase 2 requirement 4. Ask the user only if they want the display removed (QG).
- Custom rows default to reasoning off, so `local/x:high` runs with thinking off and no notice (assum, open note). Accept: the level clamp reports a notice when a requested level is dropped. Phase 4 or 6.

## Rejected

None. Every finding had code evidence and a real failure path.

## Questions for the user

- QE: are project `models.json` values literals only (no `$NAME`, no `${NAME}`)? Recommended: yes. This extends Q2, which banned only `!command`.
- QF: may a trusted project set `apiKey` on a provider that already exists? Recommended: no, refuse and report it.
- QG: keep the per-provider key source in `ask settings show` (on a shared function), or drop it?

## User review log (2026-11-06)

- QE: moot. The user dropped the project `models.json` (Q2b). Finding 3 is closed by Q2b.
- QF: the user chose "allow"; moot after Q2b. Finding 4 is closed by Q2b.
- QG: keep the key source display on a shared function.
- Finding 1: Apply. Finding 2: Apply. Finding 5: Apply (after Q2b only the user-layer part remains: account access is unknown for a provider whose `baseUrl` the user file changed).
- Finding 6: Apply. Finding 8: Apply. Finding 9: Apply.
- Finding 7: the user chose "do it as omp does". The colon rule follows `oh-my-pi/packages/coding-agent/src/config/model-resolver.ts:900-987` (`parseModelPatternWithContext`):
  1. Match the whole pattern exactly first, so a literal id such as `qwen3-coder:30b` or `x:max` wins.
  2. If the text after the last colon is a valid level: a match whose real id ends in `:<level>` wins; otherwise strip the suffix and resolve the rest, so `kimi:high` never matches `kimi-highspeed`.
  3. With no valid level suffix, try fuzzy and substring on the whole pattern.
  4. With an invalid suffix, resolve the text before the last colon and use the default level with a warning. CLI resolution (`--model`) turns this fallback off (`model-resolver.ts:2359-2366`), so an unknown level on the command line gives "unknown model".
  The exact steps reuse `findIn`; D10 (reject an ambiguous bare id) stays, because the user chose omp only for the colon rule.
- Finding 10: Apply. Finding 11: Apply. Finding 12: Apply. Finding 13: Apply (after Q2b: `--api-key` is accepted for a provider that the user `models.json` defines; merged into Finding 1).
- Finding 14: Apply (the project-row part of `saveDefault` is moot after Q2b). Finding 15: Apply (the symlink-out-of-root part is moot after Q2b).
- Reasoning notice: Apply. When a requested level is dropped because the row has no reasoning, the run shows a notice.

Final: 13 findings applied (1, 2, 5-15), 2 closed by Q2b (3, 4). No finding rejected.
