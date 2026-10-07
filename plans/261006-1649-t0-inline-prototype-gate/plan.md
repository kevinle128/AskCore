---
title: "T0 inline prototype gate"
description: "Prove native scrollback, live layout, keyboard input, and terminal restoration before D14."
status: completed
priority: P1
effort: ""
tags: [tui, prototype, tdd]
created: 2026-10-06
---

# T0 inline prototype gate

## Outcome and boundaries

Produce reproducible terminal evidence for D14 before T1 starts.
Use an isolated Go module with stock `tea.Println` first.
G1, G2, G3, and G6 block acceptance; G4 and G5 are recorded M2 checks.
Do not implement Leader, ACP, authentication, providers, extensions, public SDK APIs, or the full T1 editor.
Do not change root source, root dependencies, generated files, or root Go configuration.
The current root uses Go 1.27.0 and Bubble Tea v1.3.10; the older roadmap Go-bump description is historical.

Runtime scratch location: `/private/tmp/askcore-t0-09kf6w9r`, with its own `go.mod` and `go.sum`, no root imports, no root replacement, and no `go.work`.
This external location keeps the experiment outside the root module.
Before cleanup, archive scratch source with checksums and evidence under `artifacts/` in this plan directory.
Use this external runtime location and keep durable evidence with the plan.
Bubble Tea v2.0.10 is the gate candidate; D14 selects the product pin after results.
Research verified its [module manifest](https://raw.githubusercontent.com/charmbracelet/bubbletea/v2.0.10/go.mod): `charm.land/bubbletea/v2`, Go 1.26.0.
The root Go 1.27.0 exceeds that requirement, but root dependencies remain unchanged.
Verify the resolved renderer graph and compile the scratch fixture before use.
Pin the tested graph in the scratch module only.
Prefer reuse or adaptation per component when the applicable license permits the intended use.
For source reuse, retain source commit, provenance, copyright, license notices, and any changes.
The user confirmed: “Ask chỉ dùng nội bộ” (Ask is for internal use only).
Internal-use source copy or adaptation is permitted through the applicable FSL route with attribution and notices.
Review provenance and license again if future distribution or commercial use changes this boundary.
A close paraphrase is not an automatic license exemption.
Crush fullscreen rendering does not prove inline native scrollback.

## Dependencies and authority

Read [T0 roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t0-inline-prototype-gate-next-priority), [inventory section 3](../260930-2254-pi-feature-inventory-go-roadmap/inventory-tui.md#3-inline-prototype-gate), [TUI architecture](../../docs/tui-architecture.md), [package ownership](../../internal/tui/README.md), and [Crush report](../reports/xia-261006-2342-crush-t0-port.md).
See [terminal research and phase scout](../reports/researcher-261006-t0-terminal-and-phase-scout.md) for pinned source evidence and fixture limits.
The roadmap scratch-module location overrides the inventory's older root testkit location.
T0 does not depend on H13, authentication, or a working leader.
Its output is a prerequisite for D14 and T1; the parent inventory plan is reference context, not a wholesale blocking dependency.
Phases run in order because each needs the prior fixture or behavior.

## Current accepted phases

| # | Phase | Status |
|---|-------|--------|
| 1 | [Terminal oracle and isolated module](phase-01-start.md) | Accepted |
| 2 | [Editor and input, G6](phase-02-editor-and-input.md) | Accepted |
| 3 | [Ordered scrollback, G1](phase-03-ordered-scrollback.md) | Accepted |
| 4 | [Selector and resize, G2/G3](phase-04-selector-and-resize.md) | Accepted |
| 5 | [Evidence and D14 decision](phase-05-evidence-and-decision.md) | Accepted |

## Historical acceptance criteria

> - [ ] Module location and candidate pin are validated before implementation.
> - [ ] G1, G2, G3, and G6 pass with independent terminal-state assertions and real-terminal evidence.
> - [ ] G4, G5, and optional capability probes have a result or a stated unsupported/not-run reason.
> - [ ] Ordered terminal writes, confirmed commit progress, and partial-write failure behavior are proved.
> - [ ] Normal, error, panic, SIGINT, SIGTERM, and SIGHUP exits restore terminal state.
> - [ ] Output observation preserves the terminal FD; real ioctl/SIGWINCH resize reaches the model without injected messages.
> - [ ] Recoverable partial-frame errors reset synchronized output; permanent output failures report restoration limits explicitly.
> - [ ] Stock failure evidence precedes each renderer fix; no more than five kiln-class fixes precede a fallback decision.
> - [ ] D14 records the measured route and unresolved limits without changing root dependencies.

## Decision boundaries

A `tea.Cmd` result confirms command execution, not terminal output.
A stock `Println` command does not expose a commit acknowledgement by itself.
Capture the renderer-owned output bytes and inspect terminal state before treating content as confirmed.
Never let Update or another command write terminal bytes outside that owner.
On a partial write, stop transcript and live-frame output and retain failure metadata; do not replay the block or advance its confirmed frontier.
Permit terminal-mode restoration through the same owned cleanup path and record cleanup writes separately.

Test stock output before trying these five fix classes: flush before insertion, full live redraw, cursor repaint after insertion, first-insert flush, and erase on shrink.
Track each class and its failing reproduction separately.
If more changes are required, stop and present the raw committer and vendored-fork alternatives with evidence.
The user chooses the fallback; this plan does not choose it in advance.
A fallback preserves one ordered output owner and reruns all blocking gates.

## Validation log

User validation: internal-use question answered “Ask chỉ dùng nội bộ”; real-terminal question answered “iTerm2”.
Use iTerm2 for manual acceptance and record its exact version during execution.
If iTerm2 lacks a negotiated key capability, record unsupported capability separately and prove supported negotiation in the PTY fixture; Alt+Enter fallback in iTerm2 remains mandatory.
The user approved all three proposed review corrections: PTY FD preservation, synchronized-output failure cleanup, and initially passing stock baselines.
Final dependency pins and terminal evidence belong to execution, not plan approval.
No terminal gate has run or passed in this planning task.
No effort estimate is asserted.

### Questions and answers

1. “Để đánh giá việc copy code Crush trong plan T0, Ask dự kiến được sử dụng theo cách nào?”
   Options: “Chưa chốt phân phối; T0 là thử nghiệm nội bộ (Recommended)” | “Chỉ dùng nội bộ” | “Phát hành sản phẩm thương mại”.
   Answer: “Chỉ dùng nội bộ”.
2. “Terminal nào cần kiểm tra trực tiếp cho T0 trên máy của bạn?”
   Options: “Terminal hiện có; ghi rõ tên và phiên bản khi chạy (Recommended)” | “Kitty” | “iTerm2”.
   Answer: “iTerm2”.
3. “Áp dụng cả ba sửa đổi đã review vào plan T0: giữ PTY Fd cho resize, kiểm tra reset synchronized output khi ghi lỗi, và cho phép baseline stock PASS ngay?”
   Options: “Áp dụng cả ba (Recommended)” | “Xem chi tiết từng sửa đổi” | “Giữ nguyên bản plan”.
   Answer: “Áp dụng cả ba (Recommended)”.

## Red Team Review

Three independent reviewers found two High and one Medium issue.
The user approved all three; all are applied.
See the [review evidence](../reports/red-team-261006-t0-inline-plan.md).

| Finding | Result | Applied to |
|---|---|---|
| Preserve terminal FD through output observer | Accepted | Phases 1 and 4 |
| Reset synchronized output after recoverable partial-frame failure | Accepted | Phases 2 and 3, index |
| Keep TDD without forcing a stock baseline failure | Accepted | All five phases |

### Verification and whole-plan consistency sweep

All six plan files were reread after propagation.
Source verification covered local manifests, scaffold state, test counts, roadmap scope, and pinned upstream terminal paths.
The fact reviewer sampled 15 claims per phase and distinguished future requirements from existing implementation.
The other reviewers traced failures and checked contracts without an exact claim count.
The three accepted changes are consistent across phase steps, scenario matrices, success criteria, and the index.
No unresolved contradiction remains; exact emulator and PTY pins are explicit Phase 1 work.
Plan format and local link checks pass.
Runtime task tools are unavailable; the unchecked phase checklists are the durable execution record.
No background process was started during planning.

<!-- slug: t0-inline-prototype-gate -->

## Partial execution checkpoint: 2026-10-07

Phase 1 automated implementation is available in the [source archive](artifacts/phase-01-source/README.md).
Six scratch tests, the race check, vet, and build pass in worker evidence.
Final code review accepted the two fixes.
The user approved independent tests and continuation to Phase 2.
Independent tests are in progress; a real iTerm2 smoke check remains pending.
Phase 1 is not complete; phases 2–5 have not started.
No blocking terminal gate or D14 decision is complete.

The full-plan sweep found 0 of 65 phase checklist items checked and 0 of 5 phases complete.
The plan status remains `in-progress`.
The earlier phase table and phase frontmatter retain their original pending state under this append-only assignment.
This checkpoint records the active partial work.
The CLI phase checkbox operation changes all items at once, so it was not used for incomplete Phase 1.
See [Phase 1 execution details](phase-01-start.md#execution-checkpoint-2026-10-07) and the [progress report](../reports/pm-261007-t0-phase01-progress.md).

### Independent verification and continuation

The user approved code review, independent verification, and Phase 2 continuation.
Independent verification passed all six tests, race, vet, build, and formatting checks.
All 27 product-boundary hashes matched; source isolation and archive integrity were verified.
The [independent logs](artifacts/phase-01-source/evidence/tester/) are preserved with the Phase 1 source.
Phase 2 implementation has started; real iTerm2 evidence remains pending.

## Phase 2 partial execution checkpoint: 2026-10-07

The [Phase 2 source archive](artifacts/phase-02-source/README.md) is frozen with 72 file hashes.
Final source review returned DONE.
Worker evidence records 12 top-level tests, race, vet, and build passing.
Independent Phase 2 tests await the next user approval.
Real iTerm2 3.7.3 checks remain pending.
Phase 1's immutable archive has 34 file hashes; all 27 product-boundary hashes still match the controller's initial snapshot.

The full-plan sweep still finds 0 of 65 phase checklist items checked and 0 of 5 phases complete.
Phases 1 and 2 have automated implementation evidence; their real-terminal acceptance remains incomplete.
Phases 3–5 have not started.
Full G6, T0, and D14 acceptance are not complete.
The plan remains `in-progress`; completion checkboxes and the earlier accepted phase table are unchanged.
The CLI supports index notes and evidence, but its phase status is file-owned and its checkbox commands change all items at once.
Phase 2 index notes record this partial checkpoint without a false completed status.
See [Phase 2 execution details](phase-02-editor-and-input.md#execution-checkpoint-2026-10-07) and the [progress report](../reports/pm-261007-t0-phase02-progress.md).

## Phase 2 independent verification and Phase 3 continuation: 2026-10-07

The [independent Phase 2 tests](../reports/tester-261007-t0-phase02.md) pass.
The narrow run passes 6 top-level tests and 12 subtests; the full race run passes 12 top-level tests and 17 subtests.
Vet, build, and formatting checks exit 0.
The immutable [Phase 2 archive manifest](artifacts/phase-02-source/archive-sha256.json) now covers 79 files, including independent evidence.
The controller verified the previous 72 archived files; this checkpoint verifies all 79 hashes.
All 27 protected product hashes match the initial snapshot.

The user said: “Code xong thì cứ tiếp tục chạy tiếp đi, không phải hỏi.”
This instruction removes intermediate human code-review approval pauses; mandatory source review and independent tests remain required.
Phase 3 started after the independent Phase 2 checks passed.
Real iTerm2 evidence, full G6, T0, and D14 acceptance remain incomplete.

The full five-phase sweep retains 0 of 65 phase checklist items checked and 0 of 5 phases complete.
Phases 1 and 2 have automated implementation evidence, Phase 3 is active, and phases 4 and 5 have not started.
The existing phase table and checkboxes remain unchanged under this append-only checkpoint.
Plan status remains `in-progress`.
See the [checkpoint report](../reports/pm-261007-t0-phase02-independent.md).

## Phase 3 partial execution checkpoint: 2026-10-07

The [Phase 3 archive](artifacts/phase-03-source/README.md) contains 123 hashed files.
Worker evidence passes 27 top-level tests, race, vet, build, and formatting.
Final source review and independent tests are pending at this checkpoint.
Stock placement passes; no kiln-class renderer fix, fork, or custom committer was used.
The controller verified the 27 protected product hashes and unchanged Phase 1 and Phase 2 archive hashes.
The user waived intermediate human approval pauses; mandatory review and independent tests continue.

The full five-phase sweep retains 0 of 65 checklist items checked and 0 of 5 phases complete.
Phases 1–3 have automated implementation evidence, Phase 4 has read-only preparation, and Phase 5 has not started.
Real iTerm2 3.7.3 checks remain pending; full G1, G6, T0, and D14 acceptance are incomplete.
The plan remains `in-progress`; accepted phase tables and completion checkboxes are unchanged.
The CLI phase status is file-owned and its check command changes all items at once, so no completion operation was used.
See [Phase 3 execution detail](phase-03-ordered-scrollback.md#execution-checkpoint-2026-10-07) and the [progress report](../reports/pm-261007-t0-phase03-progress.md).

## Independent verification update: 2026-10-07

Final source review returned DONE with no findings.
Independent checks pass: the narrow run has 14 top-level tests and 13 subtests; the full race run has 27 top-level tests and 30 subtests.
Vet, build, and formatting exit 0.
The tester verified 123 archive hashes, 16 frozen source hashes, all 27 protected product hashes, and unchanged 34-file Phase 1 and 79-file Phase 2 archives.
Phase 4 implementation is now authorized after this preparation checkpoint.
Real iTerm2 evidence and full gate acceptance remain pending; completion checkboxes are unchanged.

## Phase 4 partial execution checkpoint: 2026-10-07

The [Phase 4 archive](artifacts/phase-04-source/README.md) contains 148 hashed files.
The final worker race run passes 33 top-level tests; vet, build, and formatting pass.
The regular suite passed 32 tests before the final boundary assertion was added.
Final review and independent verification are pending at this checkpoint.
Zero kiln-class renderer fixes were used, with no fork or custom committer.
The controller verified all 27 protected product hashes and unchanged prior archives.
Phase 3's archive now has 130 hashes after independent evidence was added; its previous 123 hashes match.

The full five-phase sweep retains 0 of 65 checklist items checked and 0 of 5 phases complete.
Phases 1–4 have automated implementation evidence; Phase 5 has read-only preparation only.
Real iTerm2 3.7.3 checks and native height-shrink history reflow remain pending.
Full G1/G2/G3/G6, T0, and D14 acceptance are incomplete.
The plan remains `in-progress`; accepted phase tables and completion checkboxes are unchanged.
See [Phase 4 execution detail](phase-04-selector-and-resize.md#execution-checkpoint-2026-10-07) and the [progress report](../reports/pm-261007-t0-phase04-progress.md).

## Independent verification update: 2026-10-07

Final review returned DONE with no findings.
Independent narrow checks pass 6 top-level tests; the full race run passes 33 top-level tests and 30 subtests.
Vet, build, and formatting exit 0.
The tester verified 148 archive hashes, 20 frozen source hashes, all 27 protected product hashes, and unchanged prior archives with 34, 79, and 130 files.
Phase 5 implementation is authorized.
Real-terminal evidence remains pending and completion checkboxes remain unchanged.

## Final automated checkpoint: 2026-10-07

The [Phase 5 archive](artifacts/phase-05-source/README.md) contains 179 hashed files at this checkpoint.
Clean regular and final race worker runs each pass 39 top-level tests; vet, build, formatting, and final validation pass.
All 10 machine-manifest artifact references match after capture completion.
Final source review and independent Phase 5 checks remain pending at this checkpoint.
The controller verified the 27 protected product hashes and unchanged prior archives of 34, 79, 130, and 155 files.

Automated G1/G2/G3/G6 results pass; required iTerm2 3.7.3 observations remain pending.
G4/G5 and optional probes are explicitly not run.
D14Ready is false, with stock Println plus disclosed output-owner adaptations as the measured candidate.
Zero kiln renderer fixes, no fork, and no custom committer were used.
No product dependency migration, roadmap decision, commit, or push was made.

The full five-phase sweep retains 0 of 65 acceptance checkboxes checked and 0 of 5 phases complete.
The plan remains `in-progress`; the accepted phase table and checklist state are unchanged.
The user waived intermediate human approval pauses, while mandatory review and independent tests continue.
See [Phase 5 execution detail](phase-05-evidence-and-decision.md#automated-execution-checkpoint-2026-10-07), the [result report](../reports/t0-261007-inline-prototype-result.md), and the [final PM checkpoint](../reports/pm-261007-t0-final-checkpoint.md).

## Final independent verification: 2026-10-07

Final Phase 5 source review returned DONE with no findings.
Independent fresh-copy checks pass: the focused run has 6 top-level tests and 14 subtests; the full race run has 39 top-level tests and 44 subtests, with 6 explicit capability skips.
Vet, compilation, formatting, post-run packaging, and final manifest validation all exit 0.
The tester verified the original 179 archive files, 24 source hashes, all 27 protected product hashes, all 10 manifest artifact references, and unchanged prior archives with 34, 79, 130, and 155 files.
The controller's final Phase 5 archive contains 189 hashed files after independent logs were added; all previous 179 hashes still match.
The result report includes the independent verification and its local links resolve.

Automated implementation, review, and independent verification are complete.
Required real iTerm2 observations remain pending, including native history reflow and physical Unicode placement.
G4/G5 and optional probes retain explicit not-run reasons.
D14Ready remains false; zero kiln renderer fixes and the disclosed output-owner adaptations remain the measured route.
The final five-phase sweep retains 0 of 65 acceptance checkboxes checked and 0 of 5 phases complete.
The plan remains in progress with no claim of full T0 or D14 acceptance.
No source, archive, journal, commit, or push change is part of this append-only update.

## Accepted resize route checkpoint: 2026-10-07

The user accepted the [inline resize repair](../261007-1156-inline-resize-fix/plan.md) with a generation-tagged 120 ms settle delay.
For resize repair only, the output owner can purge the screen and native scrollback and replay committed content plus the live frame at the latest size.
This supersedes earlier no-purge and no-replay requirements for resize in this record.
It does not permit replay after a partial output failure or duplicate ordinary commits.
The application retains all 2,000 fixture entries; native history capacity remains a measured terminal limit.
The reviewed frozen candidate is selected by the [E2E runner](../../e2e/tui/run.py).
Independent checks passed 43 Go tests, race, vet, build, the local renderer test, and all 18 default Alacritty/Ghostty cases.
See the [verification report](../reports/tester-261007-1230-inline-replay.md) and [plan sync](../reports/pm-261007-1230-inline-replay.md).
The 52×15 Ghostty capacity probe remains failed and is outside default acceptance.
Real iTerm2 observations remain pending, so all five physical-terminal phases and full T0 acceptance are not complete.
Earlier archive evidence remains immutable and records its original contract.

## Current decision: T0 accepted and D14 closed

The user reported “Terminal.app/iTerm2 done, tiếp D14 đi”.
T0 is accepted for the tested scope based on the retained independent automated results and this user-reported physical acceptance.
The report does not include independent physical captures, exact tested terminal versions, or a per-scenario manual record.
The earlier requirement for those artifacts is superseded for this closure; the historical requirements and checkpoint evidence remain visible above.
All five phases are reconciled against the [current decision report](../reports/pm-261007-1312-d14-tui-decision.md).
This is not a claim that all 65 historical checkbox items have independent evidence.

D14 selects `charm.land/bubbletea/v2` v2.0.10 with the tested minimal local Bubble Tea renderer patch.
Ultraviolet stays at exact unmodified upstream `v0.0.0-20260703014108-f5a850f9c2b7`; Go 1.27.0 remains.
Use the [frozen candidate and provenance](../261007-1156-inline-resize-fix/artifacts/replay-candidate/PROVENANCE.md).
The accepted inline route keeps one output owner, ordinary exact-once commits, and complete ordered resize replay after 120 ms.
T1 owns root dependency migration and a reproducible product patch or immutable fork packaging route.
No remote immutable fork release has been produced by this decision.
Known Ghostty 52×15 capacity and Alacritty 1×1 limits remain.
Historical manifests with `d14_ready: false` retain their original evidence-time meaning and are not edited.
