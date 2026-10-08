# Provider routing for `ak-plan`

Use this reference only after loading `ak-plan` and resolving the live route. The selected `ak-plan` reference owns counts, order, evidence budgets, output schemas, and gates; this file assigns those roles to providers without redefining them.

## Core role map

| Live `ak-plan` responsibility | Provider owner |
|---|---|
| Scope and mode evidence | Claude Code |
| Researcher, scout, planner, architect | Claude Code |
| Plan scaffolding and plan-directory writes | Claude Code |
| Candidate synthesis or selected-result materialization | A fresh Claude Code writer |
| Red-team reviewer and verification roles | Codex, read-only |
| Red-team consolidation, evidence filtering, and proposed dispositions | A fresh Codex adjudicator, read-only |
| Validation verification and question drafting | Codex, read-only |
| Applying accepted findings or user answers | Claude Code |
| Whole-plan consistency check | Codex checks; Claude repairs |
| HTML and other plan-side artifact generation | Claude Code |
| User decision presentation | Controller |
| Runtime task projection from accepted plan | Controller, mechanically |
| Kongming duties required by `--ultra` or `--advice` | Kongming, unchanged |

## Mode preservation

### Fast

Map the normal planner to Claude. Do not add a Codex review gate when the loaded fast route omits review or validation; adding one would change `ak-plan`, not merely its orchestration.

### Hard, deep, and parallel

Map research, scouting, design, and plan writes to Claude. Map every selected red-team and validation role to fresh Codex calls. Preserve deep-mode later-phase scouting instructions and parallel-mode disjoint ownership in the plan rather than executing implementation.

### Two approaches

Have Claude produce the two approaches required by `ak-plan`. Codex may verify factual claims only when the loaded route includes that gate. The controller presents the approaches and records the user's selection; Claude materializes the selected plan.

### Debate

Map every independent planner candidate to a separate Claude call with identical shared evidence and disjoint candidate report paths. Because the controller is coordination-only, delegate synthesis to a fresh Claude call that reads all usable candidates and follows the debate synthesis contract. Continue through the standard Codex gates selected by `ak-plan`.

### Ultra

Map read-only planner candidates to independent Claude calls, preserving the exact fan-out and usable-candidate gate from `ak-plan`. Retain Kongming as the anonymized verifier. After selection, a fresh Claude writer materializes the winning candidate unchanged because the controller cannot write plan files. Preserve reject-all behavior and the verifier receipt, then run the red-team and validation gates required by the loaded ultra route before task hydration.

### Advice

Retain Kongming advisory supervision and its model-routing contract. Resolve and invoke the live named-agent delegation capability prescribed by `ak-plan`; if the runtime cannot spawn Kongming, follow the loaded route's fail-closed behavior instead of substituting Codex. Claude remains the plan writer and Codex remains the independent reviewer where a review gate exists. Materialize the handover fields and Failure Protocol required by `ak-plan`.

## Subcommands and modifiers

- **Validate:** Codex performs read-only verification and drafts the critical questions. The controller asks them. Claude propagates confirmed answers, then Codex performs the consistency sweep.
- **Red-team:** Dispatch Codex reviewers with the exact live persona and evidence contracts. Dispatch a fresh Codex adjudicator to collect, deduplicate, severity-sort, cap, evidence-filter, and propose dispositions. The controller presents that child-authored review to the user. Claude applies only user-accepted findings, then Codex performs the consistency sweep.
- **Archive:** Delegate durable plan and journal updates to Claude; the controller routes authorization and reports results.
- **HTML:** Claude loads and follows the visual skills required by `ak-plan`, then writes the artifact after gates pass.
- **GitHub/Wiki:** Preserve privacy and publishing authorization. Claude performs authorized plan-side publication work; the controller reports returned URLs.
- **TDD, YAGNI, no-tasks, skip-journal, and visual switches:** Forward unchanged to every affected child. They modify `ak-plan`; this adapter does not reinterpret them.

## Concurrency

Parallelize only independent read-only research, candidates, or Codex reviews that the selected route already permits. Keep candidate/report paths disjoint. Keep a single Claude writer for shared `plan.md`, phase files, plan state, and final artifacts.
