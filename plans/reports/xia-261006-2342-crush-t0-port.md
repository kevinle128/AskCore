# Crush logic for the Ask T0 inline gate

## Outcome and scope

Mode: `ak:xia --port`, report only.
The goal is to identify logic that can support the T0 gate without importing Crush application behavior.
This report completes recon, mapping, analysis, and challenge.
Recommendations are not approved implementation decisions.
No product code, dependencies, public SDK types, or roadmap scope changed in this task.
No implementation plan or cook handoff is created because this request asks for a report and the Xia challenge gate remains pending.

## Source manifest

| Field | Value |
|---|---|
| Source | `charmbracelet/crush` |
| Local checkout | `/Users/dale/Desktop/workspace/opensources/crush` |
| Branch | `main` |
| Resolved commit | `65865e01950368379ad1f9746b620a77fb7a5da8` |
| Go directive | `go 1.27.0` |
| Terminal dependencies | Bubble Tea v2.0.9, Bubbles v2.2.1, Lip Gloss v2.0.6 |
| Source license file | `LICENSE.md`: FSL-1.1-MIT |
| Packed evidence | `/private/tmp/crush-t0-repomix.xml`; temporary, not a durable project artifact |

Repomix packed selected UI, entry-point, manifest, license, and test files.
Source inspection used direct reads plus GitNexus context for `updateLayoutAndSize` and `DrawCenterCursor`.
A scoped researcher checked keyboard, paste, and cleanup paths without changing files.
Source paths and line references below refer to the resolved commit, not an unspecified current upstream version.
The pinned [source tree](https://github.com/charmbracelet/crush/tree/65865e01950368379ad1f9746b620a77fb7a5da8) is the durable source reference.

The source license is not an MIT license today merely because its name includes an MIT future license.
This report does not decide license compatibility or authorize source transplantation.
Keep provenance for any later source-derived patch and check the applicable license before copying code.
Algorithm recommendations below do not include copied source bodies.

## Local authority

The [roadmap T0 section](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t0-inline-prototype-gate-next-priority) puts T0 in a scratch module outside the root module.
It overrides the older testkit location in the [TUI inventory](../260930-2254-pi-feature-inventory-go-roadmap/inventory-tui.md#3-inline-prototype-gate).
G1, G2, G3, and G6 block D14; G4 and G5 are later checks.
Start with stock `tea.Println`; choose a custom committer or fork only after a failing terminal test supplies evidence.
The root module currently uses Bubble Tea v1.3.10 and Lip Gloss v1.1.0.
The [accepted TUI architecture](../../docs/tui-architecture.md) and [component scaffold](../../internal/tui/README.md) are the future integration surfaces, not a working gate harness.
No database, leader, provider, or extension process is required to exercise T0 terminal behavior.

## Source anatomy and reusable logic

All source paths in this section are relative to the Crush checkout.

| Area | Source evidence | Behavior | Port recommendation |
|---|---|---|---|
| Program entry | `internal/cmd/root.go:131-154` | Root model, context-owned program, filter, subscription, Run, then exit text | Use program lifecycle shape; exclude workspace and subscription from the terminal-only gate |
| Root view | `internal/ui/model/ui.go:3769-3807` | Alt-screen enabled; screen buffer drawn and flattened; hardware cursor supplied | Use cursor and component composition concepts; do not copy fullscreen canvas composition |
| Layout reconciliation | `internal/ui/model/ui.go:4182-4211` | Calculate regions, set widths, then reconcile once if soft-wrap changes editor height | Port the two-pass dependency rule for G2/G3 |
| Editor height updates | `internal/ui/model/ui.go:4217-4230` | Recalculate when textarea height changes; maintain chat follow | Recalculate inline live height; omit fullscreen chat follow behavior |
| Region sizing | `internal/ui/model/ui.go:4298-4350` | Width updates invalidate frames; active editor height uses its content width | Keep one width and height calculation shared by measurement and render |
| Cursor translation | `internal/ui/model/ui.go:3718-3747` | Hide cursor if editor unavailable; translate child cursor into root coordinates | Port geometry rules, not Crush's fixed attachment-row and margin offsets |
| Dialog clamp | `internal/ui/dialog/dialog.go:275-286` | Clamp component dimensions before positioning and cursor translation | Adapt to the inline editor slot, not a centered fullscreen overlay |
| Input ownership | `internal/ui/dialog/dialog.go:233-250` | Route input to the active dialog | Use one active selector and restore editor focus for G2 |
| Keyboard report | `internal/ui/model/ui.go:1167-1172` | Record capability response and change help when disambiguation is supported | Use response-driven help and input policy; negotiation still needs PTY evidence |
| Newline binding | `internal/ui/model/keys.go:139-145`; `ui.go:3314-3318` | Shift+Enter or Ctrl+J inserts newline | Preserve Ask's Alt+Enter fallback; test Ctrl+Enter as required by G6 |
| Paste handling | `internal/ui/model/ui.go:1438-1453,5955-6007,6067-6077` | Distinct PasteMsg path; CRLF normalization; clipboard text converges on PasteMsg | Port ordinary-text normalization and routing; preserve the whole gate paste payload |
| Mouse filter | `internal/ui/model/filter.go:36-68` | Coalesce mouse samples; pass keyboard and paste through | Do not enable mouse reporting for the inline gate; retain the no-key/no-paste-drop invariant |
| Capability state | `internal/ui/common/capabilities.go:45-72` | Window, color, pixel, device, and focus reports update local capability data | Keep only gate-required reports; do not bring image and notification dependencies into T0 |
| Item versioning | `internal/ui/list/item.go:10-63` | Version and finished state control cached rendering | Useful in T1; not a native-scrollback commit acknowledgement |

## Gate coverage and gaps

| Gate | What Crush contributes | What remains unproved |
|---|---|---|
| G1: 2,000 committed lines, oversized commit, shrinking live tail | Editor-height reconciliation and component state discipline | Stock inline insertion, scrollback order, oversized output, output confirmation, and cursor after shrink |
| G2: 12-row selector over a 3-row live area | Region clamping, active dialog routing, cursor translation | Grow/shrink of an inline region, stale-row erasure, and intact scrollback |
| G3: resize with CJK and emoji at width-1 | Shared content width, soft-wrap reconciliation, child cursor translation | Actual terminal column agreement, width-1 behavior, committed-history preservation, and no duplicate editor |
| G6: negotiation and a 2,000-line paste | Capability-aware help, PasteMsg route, CRLF normalization | Negotiation on/off, Shift/Ctrl+Enter reports, Alt+Enter fallback, one paste event, full payload integrity, and terminal restoration |

Crush's alt-screen renderer is not evidence that native scrollback insertion is correct.
The inspected layout tests validate model geometry, not terminal scrollback or escape-sequence effects.
Do not record any gate as passed from this report.

## Execution paths and failure boundaries

### Resize and editor growth

A size event changes root dimensions, then layout calculates child regions.
Width updates can change soft-wrap height, so the root reconciles layout once with the new height.
The final child cursor is translated using the final region.
Ask must also reconcile the live-region height with the commit boundary.
A second pass is sufficient in Crush's inspected flow, but it is not proof that arbitrary extension components converge in two passes.

### Selector open and close

Opening the selector transfers input ownership and changes the editor-slot height.
Rendering must use the same clamped height that layout measures.
Closing restores the draft and valid prior focus, then recomputes the smaller live region.
Only a terminal test can show whether the renderer erases the removed rows without damaging scrollback.

### Keyboard and paste

Keyboard capability messages update the help and supported-key policy.
Crush delegates terminal negotiation to the terminal stack rather than implementing it in the inspected root model.
Ordinary paste is routed as a message, normalized, then applied to the focused editor.
Inline editors intercept PasteMsg before the root normalization path, so Ask must specify normalization once at its input boundary.
Crush converts sufficiently large paste into an attachment; this does not satisfy Ask's editor-paste gate unchanged.
The source threshold counts bytes per line, not terminal columns.

The trailing-backslash Enter branch at `internal/ui/model/ui.go:3243-3249` removes the backslash but does not visibly insert the newline described by its comment.
This is a source concern from inspection, not a reproduced source bug.
Do not port this branch as a fallback.

### Shutdown

Crush starts the Bubble Tea program with the command context and runs cleanup after it exits.
That is lifecycle evidence, not proof of terminal restoration after signals, errors, or panic.
T0 must observe the terminal mode and cursor after each exit path.
Keep test-owned processes and PTYs under cancellation and wait for their exit.

## Dependency matrix

`EXISTS` means an existing local owner or scaffold, not a complete runtime.
`NEW` means behavior or a fixture is required in the isolated gate.
`CONFLICT` means the source policy does not match the local contract.

| Source concern | Local equivalent | Status | Boundary |
|---|---|---|---|
| Bubble Tea root loop | `cmd/tui/main.go`, future `internal/tui/model.go` | EXISTS / CONFLICT | Root is v1; gate evaluates v2 separately |
| Layout and cursor | `internal/tui/layout.go`, `focus.go` scaffolds | EXISTS / NEW | Translate algorithms into the scratch harness first |
| Text editor | `internal/tui/editor.go` scaffold | EXISTS / NEW | Gate uses a real minimal editor, not full T1 undo/history scope |
| Selector | `internal/tui/selector.go` scaffold | EXISTS / NEW | Real open/close and input handling in the editor slot |
| Committed transcript | `internal/tui/transcript.go`, `render_inline.go` scaffolds | NEW | Crush provides no native inline committer in the inspected root |
| Terminal oracle | No T0 harness implemented | NEW | PTY plus terminal emulator assertions |
| Large paste attachments | Ask's intact editor paste requirement | CONFLICT | Exclude attachment conversion |
| Mouse and fullscreen overlays | Inline-first native terminal interaction | CONFLICT | Do not bring mouse or alt-screen policy into T0 |
| Workspace, auth, subscriptions | Existing harness capabilities | EXISTS / OUT OF SCOPE | Do not connect them to the terminal gate |

No root config, migration, or dependency change is required to produce the report or run an isolated future gate.
The later gate will need its own manifest, executable, PTY fixture, and terminal-emulator checks.
Exact implementation files remain a planning decision after the challenge gate.

## Challenge questions

| Question | Source answer | Local answer and recommendation | Risk if wrong |
|---|---|---|---|
| Does Crush solve native scrollback? | Root View enables alt-screen | No; use stock inline Bubble Tea and prove G1 | Core renderer rework; critical |
| Can fullscreen dialogs be transplanted? | Dialogs draw onto a screen buffer | Adapt ownership and clamp into the editor slot | Stale rows or lost cursor |
| Does two-pass layout cover all components? | Root reconciles textarea/inline height once | Use it for bounded gate components; verify extension convergence separately | Oscillating geometry or later rework |
| Is Crush paste policy compatible? | Large paste becomes attachment | Keep 2,000 lines as editor input and count PasteMsg before transformation | Gate falsely passes while content changes |
| Are keyboard bindings identical? | Shift+Enter and Ctrl+J | Keep Ask's Alt+Enter fallback; verify requested modified keys | Users cannot enter newlines |
| Does capability handling prove negotiation? | Root consumes reports; terminal stack negotiates | PTY tests with supported and unsupported responses | False G6 success |
| Is process cleanup terminal restoration proof? | Context-owned program and deferred cleanup | Inspect actual modes and cursor after exits | Broken user terminal; critical |
| Can source code be copied without a license decision? | License file is FSL-1.1-MIT | No compatibility conclusion here; preserve provenance and review before copying | License/distribution mismatch; critical if source is copied |

## Decision matrix

| Decision | Source way | Local way | Recommendation |
|---|---|---|---|
| Screen | Fullscreen canvas | Inline live area and native scrollback | Local |
| Geometry | Shared regions plus soft-wrap reconciliation | Root-owned live geometry | Adapt algorithm |
| Cursor | Translate child cursor through root layout | Translate through final live region | Adapt algorithm |
| Selector | Fullscreen overlay stack | Editor-slot replacement | Adapt input ownership and clamping only |
| Paste | Text or attachment | Intact editor paste for gate | Local policy |
| Newline | Shift+Enter / Ctrl+J | Negotiated keys plus Alt+Enter fallback | Local policy |
| Commit | View/list rendering | Ordered native-scrollback insertion | New gate-specific behavior |
| Filter | Mouse coalescing | Native terminal mouse in inline mode | Exclude filter from gate |
| Dependency versions | Source v2.0.9 | D14 candidate in scratch module | Do not change root pin or silently replace candidate |
| Source reuse | Source implementation | Local rewrite from verified behavior | Do not transplant source bodies in this task |

## Risk and validation status

Critical assumptions: inline insertion, terminal restoration, and source licensing if copying is selected.
Under the Xia challenge scale, three critical assumptions make this a medium-risk port candidate.
Layout generalization and custom-editor responsiveness remain later UI-extension risks, not reasons to expand T0.
An inline trade-off exercise compared fullscreen transplantation, local algorithm adaptation, and a custom committer.
Local adaptation preserves the gate purpose; the committer choice still requires a failing empirical check.

Completed checks: source manifest, scoped Repomix pack, direct code paths, GitNexus symbol context, local roadmap and component boundaries.
Not run: source tests, Ask runtime tests, PTY tests, real-terminal tests, negotiation experiments, or gate benchmarks.
The report does not claim T0 completion or Bubble Tea compatibility.

## Unresolved decisions

- The user confirmed internal-only use on 2026-10-06 during T0 planning.
  The inspected license expressly permits internal use; the plan may copy or adapt relevant code with source provenance and applicable notices.
  Algorithm-only reuse is no longer a mandatory boundary.
  Reassess the license route if distribution changes.
- Resolve the committer route from G1–G3 evidence, not from Crush's fullscreen renderer.
- Measure the G6 keys, paste integrity, and terminal restoration in the scratch gate.
