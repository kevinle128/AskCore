---
title: "H6a: layered settings and the project trust gate"
description: "Load user and trusted project settings with Pi semantics and Ask file safety, and gate project .ask files behind a leader-owned trust decision."
status: completed
priority: P1
effort: 40h
branch: develop
tags: [feature, backend, security, settings, trust, tui, acp, leader]
blockedBy: []
blocks: []
created: 2026-11-03
---

# H6a: layered settings and the project trust gate

## Outcome

After H6a, every Ask session reads two settings layers: `<ASK_HOME>/settings.json` (user) and `<project>/.ask/settings.json` (project). The project layer loads only after a trust decision for the canonical session cwd. The leader owns that decision and `<ASK_HOME>/trust.json`. Clients only ask for the state and send grant or revoke. A plain folder with no gated files is trusted with no question. A folder with gated files and no saved decision gets a startup prompt in the TUI, and is untrusted in `ask -p`, `ask acp` and old clients unless `--approve`, a saved decision, or `defaultProjectTrust: "always"` says otherwise.

Ask keeps Pi's semantics and fixes Pi's flaws: no project read and no project write before the decision, no implicit trust that is saved, atomic writes with fsync, a corrupt file that is never overwritten and always reported, and a dump that names the layer of each value.

## Constraints

- User decisions of 2026-11-03 are final (trust model like Pi, startup prompt plus flags, leader owns trust). Do not reopen them. See [Settled decisions](#settled-decisions).
- D2 (`roadmap.md:52`): settings are files that `internal/settings` owns. D3 (`roadmap.md:53`): no tool permission popups. The trust prompt is a startup gate, not a tool approval, so D3 stands.
- Import rules: `internal/settings` imports the standard library only (`.golangci.yml:144-149`). Only `internal/app` and `cmd/*` import `internal/config` (`.golangci.yml:285-294`). The leader imports only `pkg/protocol`, `logs`, `x/sys/unix` and zap (`.golangci.yml:210-222`). H6a adds no new import edge between internal packages.
- Keep the `AuthStore` file discipline: sidecar flock with a 5 s wait (`internal/settings/lock.go:11`), 0600 temp file, fsync, rename, directory sync (`internal/settings/auth.go:268-336`).
- Tests run the real entry path. Acceptance uses the built `ask` with a temporary `ASK_HOME` (phase 8). No test-only harness bypasses `ask -p`, `ask acp` or the leader.
- Code comments, test names and file names never name a plan, a phase or a finding code. Docs use ASD-STE100.
- TUI code waits for an approved `design.md` (skill `.claude/skills/ask-tui-design`).

## Non-goals

- H6b: prompt sections, `SYSTEM.md`/`APPEND_SYSTEM.md` assembly, context-file assembly, skills, `defaultTools` (H-PROMPT-*, H-TOOL-10). H6a only detects those files as gated.
- The Pi `project_trust` extension event (H11). No stub is added.
- Cordis layering, `!!js`, PID lock takeover, file-watch hot reload (DeepSeek items the user skipped).
- Keys without a consumer today (see [Key disposition](#key-disposition-h-conf-03-to-06)).
- A tool permission policy (M2).

## Acceptance criteria

1. In an untrusted project, the project `.ask/settings.json` is never opened. An unreadable or invalid project file gives no diagnostic while untrusted, and gives a diagnostic with its path once trusted.
2. A plain folder (no gated files) starts a trusted session and `trust.json` does not change.
3. A corrupt or unsafe `trust.json` gives an untrusted session and one warning with the path. No process crashes. A grant is refused and the file keeps its bytes. `defaultProjectTrust: "always"` does not override it.
4. A corrupt user or project `settings.json` is never overwritten, and each refused write reports the path.
5. A project cannot set `defaultProjectTrust` or any key on the deny list, whatever its trust. A trusted project may override every other shipped key, as the project file exists to override the user file. Each attempt gives one diagnostic.
6. `/model` with a save answers "not saved" with a reason when the trusted project sets `defaultModel`.
7. `ask -p` in a project with gated files prints one untrusted warning on stderr, and `ask -p --approve` loads the project layer. `ask acp --approve` does the same for an editor.
8. The TUI shows the approved trust prompt at startup for a folder with gated files and no saved decision. A choice reaches the leader, and the leader writes `trust.json`. `/trust` shows and changes the decision for the next session.
9. `ask settings show` names the source layer of each value and never reads an untrusted project file.
10. `go test ./...`, `go vet ./...` and golangci-lint pass. The phase 8 acceptance runs pass on the built `ask`.

## Phases

| Phase | Name | Status | Effort | Depends on |
|---|---|---|---|---|
| 1 | [Trust store and decision core](./phase-01-trust-store-and-decision-core.md) | Pending | 5h | none |
| 2 | [Layered settings read path](./phase-02-layered-settings-read-path.md) | Pending | 5h | 1 |
| 3 | [Settings writer discipline and user consumers](./phase-03-settings-writer-and-user-consumers.md) | Pending | 5h | 2 |
| 4 | [Trust composition, wire and headless](./phase-04-trust-composition-wire-and-headless.md) | Pending | 8h | 3 |
| 5 | [TUI trust prompt design (approval gate)](./phase-05-tui-trust-prompt-design.md) | Pending | 3h | none (reads phase 4 wire shape from this plan) |
| 6 | [TUI trust prompt and /trust](./phase-06-tui-trust-prompt-and-trust-command.md) | Pending | 6h | 4, 5 approved |
| 7 | [Settings dump, CLI surface and docs](./phase-07-settings-dump-cli-and-docs.md) | Pending | 4h | 4, 6 |
| 8 | [Real entry path acceptance](./phase-08-real-entry-path-acceptance.md) | Pending | 4h | 1-7 |

### Inventory row ownership

| Row | Owning phase | Pinned by |
|---|---|---|
| H-CONF-01 layering | 2 | `TestLayeredProjectOverridesUser`, `TestMergeNestedObjectsAndReplacesArrays`, `TestProjectCannotSetUserOnlyKeys` |
| H-CONF-02 write policy | 3 | `TestCorruptSettingsNotOverwrittenAndReported`, `TestUpdateKeepsUnknownAndExternalKeys`, `TestUpdateKeepsOldBytesWhenRenameFails` |
| H-CONF-03 to 06 key groups | 2 (read), 3 (write), 4 (consumers) | [Key disposition](#key-disposition-h-conf-03-to-06); `TestDecideThroughFactory`, `TestSaveDefaultReportsShadowed` |
| H-CONF-09 flags | 4 (parse), 6 (TUI), 7 (docs) | `TestParseApproveFlags`, `TestACPArgsAcceptApprove` |
| H-CONF-12 env vars | 7 | `TestAskHomeHasOneRootFunction` |
| H-CONF-14 config files | 1 (`trust.json`), 2 (project `settings.json`), 7 (docs) | `TestTrustStoreUpdateKeepsOtherEntries`, `TestProjectReaderAcceptsCommittedMode` |
| H-SEC-01 trust gate | 1 (gated list), 2 (no read), 4 (composition) | `TestGatedFilesList`, `TestUntrustedProjectSettingsNeverOpened`, `TestTrustE2EPrintUntrustedWarns` |
| H-SEC-02 decision order | 1 (`Decide`), 4 (wire), 6 (prompt) | `TestDecideOrder`, `TestImplicitTrustIsNeverSaved`, `TestTrustPromptShownForUndecidedProject` |
| H-SEC-05 hygiene | 1 (0600 trust store), 2 (project reader: no FIFO, cap, no create), 3 (temp file removed, lock outside repo) | `TestCorruptTrustStoreGivesUntrusted`, `TestProjectReaderRefusesSpecialFiles`, `TestLoadNeverCreatesProjectDir`, `TestProjectUpdateWritesTempInProjectDir` |

### Dependency graph

```text
P1 -> P2 -> P3 -> P4 --+--> P6 --> P7 --> P8
                       |    ^
P5 (design, user gate) +----+
```

P5 runs in parallel with P1 to P4: it writes only `design.md` and its artifacts. P6 starts only when `design.md` has `design-status: approved`. Phases 1 to 4 share `internal/settings` and `internal/app`, so they run one after the other.

### File ownership

| Phase | Owns (create or modify) |
|---|---|
| 1 | `internal/workspace/project.go`, `internal/workspace/trust.go` (+ tests), `internal/settings/trust.go` (+ test) |
| 2 | `internal/settings/settings.go`, `internal/settings/merge.go`, `internal/settings/project.go` (+ tests) |
| 3 | `internal/settings/auth.go` (commit only), `internal/settings/settings.go` (write side), `internal/settings/thinking_fold_test.go`, `cmd/tui/thinking_fold.go`, `internal/app/module_acp.go` (store wiring only) |
| 4 | `pkg/protocol/acp_trust.go`, `pkg/protocol/acp.go` (state fields), `internal/acp/host.go`, `internal/acp/agent.go`, `internal/acp/trust_methods.go`, `internal/acp/ask_methods.go`, `internal/leader/policy.go`, `internal/app/trust.go`, `internal/app/module_acp.go`, `internal/app/default_model.go`, `internal/tuiclient/trust.go`, `internal/tuiclient/client.go` (session/new meta), `cmd/tui/args.go`, `cmd/tui/acp.go`, `cmd/tui/headless.go`, `cmd/tui/leader.go` |
| 5 | `plans/261103-2158-h6a-settings-trust/design.md`, `plans/261103-2158-h6a-settings-trust/artifacts/*` |
| 6 | `internal/tui/*` (trust dialog, `/trust`, notices), `internal/tui/client.go`, `internal/tuiclient/client.go` (`NewSession` signature), `cmd/tui/interactive.go`, `cmd/tui/tui_client.go`, `cmd/tui/headless.go` (one call: pass the flag to `runInteractive`) |
| 7 | `cmd/tui/settings_command.go` (+ test), `cmd/tui/ask_home_root_test.go`, `cmd/tui/headless.go` (dispatch line only), docs listed in phase 7 |
| 8 | `cmd/tui/trust_e2e_test.go`, `e2e/tui/tui-test.toml` (one case), `e2e/tui/README.md` |

## Data flow

```text
session/new {cwd, _meta["ask.dev/trust"]?}            (TUI, connect, editor, in-process)
  -> leader router: policy check, route meta injected (other _meta keys kept)
  -> acp.Adapter.NewSession -> Host.NewSession -> Factory(ctx, SessionRequest)
  -> app trust composition:
       workspace.ProjectRoot(cwd, askHome)      canonical cwd; none when .ask is ASK_HOME
       workspace.GatedFiles(root, userHome)     Lstat only, never opens a file
       settings.TrustStore.Read                 trust.json bytes -> map, or a store error
       settings user layer (user file only)     defaultProjectTrust
       workspace.Decide(inputs)                 -> {trusted, source}
  -> settings.NewLayered(home, Project{Root, Trusted})
       user layer always; project layer only when Trusted
       merge (Pi rules) -> deny/user-only filter -> typed Effective + Sources + Diagnostics
  -> start model choice (defaultModel), agent built
  -> _ask/session/state carries trust {trusted, source} and diagnostics
```

Trust writes: TUI prompt or `/trust` -> `_ask/trust/set` -> leader host -> `settings.TrustStore.Update` (lock, fresh read, refuse when corrupt, atomic replace).

## Settled decisions

Each decision has a recommendation and its reason. "Confirm" marks a decision the user should confirm before the phase that uses it starts.

| # | Decision | Choice | Reason |
|---|---|---|---|
| S1 | Corrupt or unsafe `trust.json` | Untrusted for every project, one warning with the path, grants refused, file never overwritten. `defaultProjectTrust: "always"` does not override it. A missing file is an empty store. | Fail closed. A damaged store cannot say which folders the user refused. Pi throws and may stop at startup (Pi report, question 4). |
| S2 | Package ownership | `internal/settings` owns the bytes of `trust.json` (`trust.go`) and of both settings files. `internal/workspace` owns path rules (canonical root, gated files, nearest ancestor) and the pure decision function. `internal/app/trust.go` composes them. Settings receives `Project{Root, Trusted}` through the `NewLayered` constructor. | Keeps both packages free of new imports: settings is strict stdlib (`.golangci.yml:144-149`), workspace README allows stdlib, `norm`, `store` (`internal/workspace/README.md:47`). Matches `docs/ask-architecture-reference.md:380` ("runtime settings ... are files that internal/settings owns") and the ownership tables (`internal/settings/README.md:17`, `internal/workspace/README.md:19`). |
| S3 | Typed API shape | One per-session value `settings.Layered` built by `NewLayered(home, Project)`. `Load` returns `Effective`: typed fields with defaults applied, `Sources` (key to `default`/`user`/`project`), `Diagnostics` (layer, path, key, text). `Update(ctx, Scope, edit)` is the only writer. | One read model for every consumer, as Pi's typed getters, without Pi's in-memory setter queue. Go has a synchronous lock, so no write queue (Pi report, "Adapt"). |
| S4 | Compat of existing fields | File format does not change. `defaultModel`: same key, user write path unchanged, a trusted project may set it, a user save that the project shadows is refused with a reason. `thinkingFold`: a trusted project may override it (user decision 2026-11-04). The client still reads the user file for the first frame, then applies the effective value that the leader returns in `_ask/session/state`. Existing files stay valid. Unknown keys stay. No migration step. | `thinkingFoldSetting` runs in the client before any session (`cmd/tui/thinking_fold.go:19-29`, `cmd/tui/interactive.go:68`). It cannot know the leader's trust decision, so the user file gives the first frame and the session state gives the final value. Same as Pi, where a project may set `hideThinkingBlock`. **Decided (user, 2026-11-04):** the project settings file overrides the global file. |
| S5 | `merge.go` | Create it (promised by `internal/settings/README.md:8,94`). Plain objects merge recursively. Arrays and scalars of the project replace the user value. A JSON `null` in the project is ignored. | Pi rules (`settings-manager.ts:191-212`) with the nested-object fix (Pi `CHANGELOG.md:818`). |
| S6 | Keys shipped | `defaultModel`, `thinkingFold`, `defaultProjectTrust`. All other H-CONF-03..06 keys are deferred (table below). | Only these keys have code that reads them, or (for `defaultProjectTrust`) code that H6a adds as the trust consumer. |
| S7 | Gated paths and deny list | See [Gated paths](#gated-paths) and [Project key policy](#project-key-policy). One list each, in one file each. | Pi keeps one list for both the check and the docs (`trust-manager.ts:30-39`). |
| S8 | Wire shape | `_ask/trust/state` and `_ask/trust/set` (new `pkg/protocol/acp_trust.go`). Session override in `session/new` `_meta["ask.dev/trust"]` = `"approve"` or `"deny"`. `_ask/session/state` gains optional `trust`, `diagnostics` and the effective `thinkingFold`. No leader protocol version bump. | The router keeps other `_meta` keys (`internal/leader/rpc.go:158-175`). An unknown method is already `unsupported` (`internal/leader/router.go:251-254`). Additive fields follow the `commitTime` precedent (`internal/leader/README.md:43`). |
| S9 | Which links serve `_ask/trust/*` | The leader link and the in-process TUI link. `ask acp` answers `unsupported` for both methods and takes `--approve`/`--no-approve` instead. A new `ACPParams.TrustRequests` field selects it. | The in-process TUI is the same `ServeStdio` host over a pipe with no route (`cmd/tui/tui_client.go:100-135`), so `RequireRoute` cannot be the switch. An editor link has no user-facing prompt in Ask. **Confirm.** |
| S10 | Project root | The canonical cwd (`filepath.Abs` then `EvalSymlinks`; the raw clean path when the cwd does not resolve). No upward search for `.ask/`. Trust entries resolve by nearest ancestor. The project layer is absent when `<root>/.ask` is the canonical `ASK_HOME`. | Pi finds `.pi/settings.json` in the cwd only (`settings-manager.ts:297-302`). An upward search would let a parent folder such as `/tmp` change a session. The `ASK_HOME` rule fixes the Pi `$HOME` bug (Pi `CHANGELOG.md:1536`). |
| S11 | Project write lock | Lock sidecar in the Ask home: `<ASK_HOME>/project-settings-<first 16 hex of sha256(canonical root)>.lock`. The temp file is in `<root>/.ask/` for the rename and is always removed. | A lock file in the project shows in `git status`. The rename must stay in one directory (`auth.go:272` puts the temp file in the home today). |
| S12 | Trust cache | None. The leader reads `trust.json` at each `session/new` and each `_ask/trust/state`. | One small file. No cache, no invalidation bug (Pi caches per cwd, `main.ts:724-776`). |
| S13 | `--approve` reach | Root command (`ask`, `ask -p`) and `ask acp` accept `--approve`, `-a`, `--no-approve`, `-na`. `ask leader` takes none (it is shared). `ask connect` takes none in H6a. | Pi grammar (`cli/args.ts:232-235`). A leader-wide flag would grant trust for other clients. **Confirm** `ask connect` stays without the flag. |
| S14 | Settings dump | `ask settings show [--json]` for the current cwd. Read only. It runs the same decision with no UI. It never opens an untrusted project file. | DeepSeek `--dump-config` (DeepSeek report, section 1 "Diagnostics"). **Confirm** the command name. |
| S15 | Session-only prompt choices | Carried by the same `_meta["ask.dev/trust"]` override as the flags; nothing saved. The design phase picks the options. | One mechanism for flags and session-only choices. |
| S16 | Key match rule | Deny and user-only checks compare top-level keys without regard to case. The typed read takes exact keys from the merged map, never `json.Unmarshal` into a struct. | Go's decoder matches struct fields without regard to case, so `"DefaultProjectTrust"` in a project file would otherwise pass an exact-case deny check. The router already guards the same class of bug (`internal/leader/README.md:44`). |
| S17 | `defaultThinkingLevel` | Not shipped unless the user confirms. Cost if confirmed: about 1 h in phase 4 (start level in `newNativeAgentOn`, `cmd/tui/headless.go:308-311`, through `acpParams` `NewAgent`, `cmd/tui/acp.go:155-158`). | The start level is set in code today, so the key has a place to land, but it is new wiring and a behavior change. **Confirm.** |

### Gated paths

A project needs a decision when any of these exist (checked with `Lstat`; no file is opened):

- In `<root>/.ask/`: `settings.json`, `SYSTEM.md`, `APPEND_SYSTEM.md`, `mcp.json`, `skills/`, `prompts/`, `extensions/`.
- `.agents/skills/` in `<root>` and in each ancestor, except `<user home>/.agents/skills/`.

Not gated (always load, by user decision 1): `AGENTS.md`, `CLAUDE.md`, and any other file. An empty `.ask/` needs no decision. Pi's `.pi/themes` has no Ask owner and is left out.

### Project key policy

| Rule | Keys | Effect in the project layer |
|---|---|---|
| User-only (key has a consumer) | `defaultProjectTrust` | Dropped, one diagnostic. |
| Deny by prefix (top-level key and its whole subtree; no consumer yet, reserved so a later phase cannot accept it from a project by accident) | `httpProxy`, `env`, `shellPath`, `shellCommandPrefix`, `npmCommand`, `providers`, `hooks`, `cacheWarming`, `deviceId`, `trust` | Dropped, one diagnostic. The rest of the project layer loads. |

Reason for drop-and-report instead of DeepSeek's fail-the-launch: a cloned repository must not stop the user's session start; the diagnostic still makes the attempt visible. **Confirm** the deny entries.

### Key disposition (H-CONF-03 to 06)

| Key (Pi) | Ask | Consumer today or owner phase |
|---|---|---|
| `defaultModel` | Ship | `internal/app/default_model.go:39-50` (`startChoice.pick`), `internal/acp/ask_methods.go:167-171,263-265` |
| `defaultProvider` | Diverge: not added | Ask model ids are `provider/id@api`, so `defaultModel` names the provider |
| `defaultThinkingLevel` | Decision S17 | `cmd/tui/headless.go:308-311` |
| `hideThinkingBlock` | Adapt: Ask key `thinkingFold`, project may override | `cmd/tui/thinking_fold.go:19-29` |
| `modelThinkingLevels`, `thinkingBudgets`, `enabledModels`, `showCacheMissNotices` | Deferred | H7 model selection follow-up / H17 scoped models |
| `cacheWarming` | Deferred, deny-listed for projects | H17 |
| `steeringMode`, `followUpMode` | Deferred | No queue mode exists in `internal/agent` (checked: no match for a mode type) |
| `externalEditor`, `doubleEscapeAction`, `treeFilterMode` | Skip | TUI-only in Pi; T2 |
| `defaultProjectTrust` | Ship (user-only) | Phase 4 trust composition |
| `defaultTools`, `codemode.*` | Deferred | H6b (H-TOOL-10) |
| `sessionDir` | Deferred | H8 (and never read before trust) |
| `shellPath`, `shellCommandPrefix`, `npmCommand` | Deferred, deny-listed for projects | `internal/sandbox/shell.go:10-20` finds bash itself; X3 |
| `images.*` | Deferred | T2 |
| `compaction.*`, `branchSummary.*` | Deferred | H10 |
| `retry.*`, `retry.provider.*` | Deferred | H9 (defaults in `internal/providers/retry.go:15`) |
| `transport`, `httpIdleTimeoutMs`, `websocketConnectTimeoutMs`, `httpProxy` | Deferred (`httpProxy` deny-listed) | H17 |

### Flags, env vars and files (H-CONF-09, 12, 14)

| Item | Ask in H6a | Note |
|---|---|---|
| `--approve`/`-a`, `--no-approve`/`-na` | Add | Root and `ask acp`; both given is a usage error (exit 1). `-a`/`-na` are a parse change: an unknown `-x` is an error today (`cmd/tui/args.go:176-178`) |
| `--system-prompt`, `--append-system-prompt`, `--tools`/`-t`, `-nt`, `-nc` | Deferred | H6b |
| `--session*`, `-c`, `-r`, `--fork`, `--no-session`, `--session-dir`, `--export` | Deferred | H8 |
| `--models`, `--list-models` | Deferred | H7 follow-up / H17 |
| `--offline` | Diverge | `ASK_DISABLE_MODEL_REFRESH` covers the only startup network call; no alias |
| `--verbose`, `-v` | Deferred | `ask version` exists; H12 |
| `ASK_HOME` | Keep | One root function `settings.ResolveHome` (`internal/settings/paths.go:71-86`); phase 7 adds a check that no other code builds `~/.ask` |
| New env var for trust | None | Pi has none; CI uses `--approve` or `defaultProjectTrust` |
| `settings.json` (user, project), `trust.json` | Add or extend | This plan |
| `mcp.json`, `models.json`, `SYSTEM.md`, `APPEND_SYSTEM.md` | Detect only (gated) | H16, H17, H6b own reading |

### Wire compatibility

| | Old leader | New leader |
|---|---|---|
| Old client | No change. | Sessions decide trust with no UI: untrusted unless saved, `always`, or no gated files. No prompt. The old TUI shows no trust notice. |
| New client | `_ask/trust/state` answers `unsupported`; the TUI skips the prompt and shows nothing; the old leader reads no project file. In practice a newer client build replaces a client-started older leader (`internal/leader/README.md:67`). | Full behavior. |

## Pi and DeepSeek conformance

| Behavior | Pi (4c6fb7cfe) | DeepSeek (5badb15) | Ask | Reason | Test that pins it |
|---|---|---|---|---|---|
| User + project layers, project wins | yes | no project layer | copy | user decision 1 | `TestLayeredProjectOverridesUser` |
| Objects merge, arrays and scalars replace | yes | whole-row replace | copy | nested-object fix | `TestMergeNestedObjectsAndReplacesArrays` |
| Project file in cwd only | yes | n/a | copy | S10 | `TestProjectRootIsCanonicalCwd` |
| Project read before trust (`sessionDir`) | yes (`main.ts:669`) | n/a | diverge-deliberate | the gate must be closed | `TestUntrustedProjectSettingsNeverOpened` |
| Project write before trust (migration) | yes (`migrations.ts:257-263`) | n/a | diverge-deliberate | same | `TestLoadNeverCreatesProjectDir` |
| Write mechanics | in place, 200 ms lock | temp + rename, no fsync | adapt: keep AuthStore commit | crash safety | `TestUpdateKeepsOldBytesWhenRenameFails` |
| Merge-on-write, unknown keys kept | yes | patch: yes | copy | external edits survive | `TestUpdateKeepsUnknownAndExternalKeys` |
| Corrupt file never overwritten | yes, silent | last good kept | copy + report | Pi drops silently | `TestCorruptSettingsNotOverwrittenAndReported` |
| Validate whole candidate | no | yes | copy | S3 | `TestUpdateRefusesInvalidCandidate` |
| Refuse a shadowed write | no | yes | copy | S4 | `TestSaveDefaultRefusedWhenProjectSetsDefaultModel` |
| Rollback on failure | no | yes | adapt: old bytes kept, view unchanged | Ask has no live apply | `TestUpdateKeepsOldBytesWhenRenameFails` |
| Global-only keys | yes | n/a | copy | S7 | `TestProjectCannotSetUserOnlyKeys` |
| Deny by prefix | n/a | `.env` names | copy for project keys | S7 | `TestProjectDeniedKeysDroppedWithDiagnostic` |
| Case-insensitive key bypass | n/a | case-insensitive | copy | S16 | `TestDeniedKeyMatchIgnoresCase` |
| Source dump | no | `--dump-config` | copy | S14 | `TestSettingsShowNamesSourceLayer` |
| Read-time validation, default on bad value | yes | fail loud | copy + diagnostic | S3 | `TestInvalidKnownValueGivesDefaultAndDiagnostic` |
| Load migrations | yes | legacy import | skip | no legacy format | none |
| No gated files means trusted | yes | always trusted | copy | user decision 1 | `TestPlainFolderTrustedWithoutDecision` |
| Gated list | `.pi/*` + `.agents/skills` | none | adapt (no themes) | Gated paths | `TestGatedFilesList` |
| Context files ungated | yes | yes | copy | user decision 1 | `TestAgentsFileIsNotGated`; H6b hand-off |
| Decision order | flag, event, saved, default, UI | n/a | adapt: no event step until H11 | no stub | `TestDecideOrder` |
| Nearest ancestor, canonical path | yes | n/a | copy | Pi test `trust-manager.test.ts:24-37` | `TestNearestEntryChildBeatsParent` |
| Corrupt `trust.json` | throws | n/a | diverge-deliberate | S1 | `TestCorruptTrustStoreGivesUntrusted` |
| Implicit trust saved on `/reload` | yes (`interactive-mode.ts:5225-5248`) | n/a | diverge-deliberate | a model can create `.ask/` | `TestImplicitTrustIsNeverSaved` |
| Home `.pi` taken as project | bug, fixed | n/a | copy fix | S10 | `TestAskHomeIsNotAProject` |
| Session-only choices | yes | n/a | copy via meta | S15 | `TestSessionOverrideIsNotSaved` |
| Per-cwd trust cache | yes | n/a | diverge-deliberate | S12 | `TestTrustReadAtEachSession` |
| No UI: only flag, saved, `always` trust | yes | n/a | copy | user decision 3 | `TestPrintModeUntrustedWithoutApprove` |
| Startup prompt | yes | no | copy | user decision 2 | phase 6 goldens |
| `/trust` applies after restart | yes | n/a | adapt: next session | leader is per session | `TestTrustCommandAppliesToNextSession` |
| PID lock takeover, hot reload, Cordis | n/a | yes | skip | user skip list | none |

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| A model creates `.ask/settings.json` and gains trust | Medium | High | Implicit trust never saved; decision at each `session/new`; test `TestImplicitTrustIsNeverSaved`. Trust is not a boundary against `bash` editing `trust.json` (H-SEC-03), stated in docs. |
| Commit refactor weakens credential durability | Low | High | Keep `AuthStore.commit` as a thin wrapper; run `internal/settings` auth fault tests unchanged (phase 3). |
| `Factory` signature change breaks callers | High | Low | Enumerated callers in phase 4; compile and test gate. |
| FIFO or huge project file blocks session start | Low | Medium | `Lstat` regular-file check, open with `O_NOFOLLOW` after resolving, `O_NONBLOCK`, 1 MiB cap. |
| Trust prompt delays the first frame | Medium | Medium | State call bounded; design phase places the prompt in the prompt pane slot. |
| Case-folded key bypasses the deny list | Medium | High | S16 and `TestDeniedKeyMatchIgnoresCase`. |
| macOS case-insensitive paths make an entry miss | Medium | Low | A miss fails closed (prompt again); documented. |
| Design approval delays phase 6 | Medium | Medium | Phase 5 starts at once, in parallel with phases 1 to 4. |
| User confusion when a project shadows `defaultModel` | Medium | Low | Reason text on the `/model` result; `ask settings show` names the layer. |

## Rollback

Each phase is one or more focused commits. Revert in reverse order. The settings file format does not change, so a revert leaves user files valid. A revert after phase 4 leaves `trust.json` on disk; older builds ignore it. No database migration.

## Hand-off note for H6b: ungated context files

By user decision 1, project `AGENTS.md` and `CLAUDE.md` load in untrusted projects. A cloned repository can put instructions there that steer the model (prompt injection). H6a does not solve this. H6b should consider DeepSeek's delivery: context files as one user-role message, escaped (`</system-reminder>`), with a byte budget (DeepSeek `agent-instructions/README.md:105,130`). H6b must also replace the sentence "project `AGENTS.md` load only after trust" in `internal/workspace/README.md:55`; phase 7 already rewrites it for H6a.

## Extension trace

The next credible variant is the H6b skills consumer: it reads the same `workspace` gated-path list and the session's `Project.Trusted` from the phase 4 Factory input, so it adds a consumer and no trust code. H6a adds no scaffolding for it.

## Validation (every phase)

- `go test ./internal/workspace ./internal/settings ./internal/app ./internal/acp ./internal/leader ./pkg/protocol ./internal/tuiclient ./internal/tui ./cmd/tui`
- `go vet ./...`
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`
- Before merge: `go test ./...` and `go test -run '^TestReferenceDocs$' ./cmd/tui`

## Decisions confirmed by the user (2026-11-04)

1. `thinkingFold`: a trusted project may override it. The project settings file overrides the global file for every key except `defaultProjectTrust` and the deny list. The client applies the value from the session state (phase 6).
2. `ask acp` answers `unsupported` for `_ask/trust/*` and takes `--approve` and `--no-approve`.
3. `ask connect` gets no trust flag in H6a. `ask leader` never takes one.
4. The dump command is `ask settings show [--json]`.
5. `defaultThinkingLevel` is deferred.
6. Deny list confirmed (11 keys above). A denied project key is dropped and reported; the session still starts.
7. Gated paths include `.agents/skills/` of the project and its ancestors (Pi parity).
8. `ask -p` uses `defaultModel` when no --provider/--model is passed (user, 2026-11-04); trust-gated for the project layer.

## Unresolved questions

None open from the user decisions. Open for the cook phase: the exact text and layout of the trust prompt (phase 5 design file, user approval needed before phase 6).
