# T0 plan review

## Status

Three reviewers read the index and all five phase files.
The lenses were security and facts, failure paths, and assumptions and contracts.
No reviewer ran builds or terminal tests.
All gate behavior remains proposed.
The security reviewer sampled 15 claims per phase, separating existing source facts from future acceptance requirements.
The failure reviewer traced pinned upstream paths but did not track an exact claim count.
The assumption reviewer checked scope and TDD against local authority.

## Proposed decisions

| Finding | Severity | Proposed disposition | Target |
|---|---|---|---|
| Output observer can hide the terminal FD | High | Accept | Phases 1 and 4 |
| Partial frame can leave synchronized output active | High | Accept | Phases 2 and 3, index |
| TDD forces a failure for a possibly correct stock baseline | Medium | Accept | All phase TDD sections |

User review was requested before these edits, as required by the planning skill.
The user approved all three corrections; they are applied to the index and affected phase files.
The confirmed internal-only use and iTerm2 acceptance remain unchanged.

## Evidence and concrete edits

### Terminal FD

Phase 1 lines 99–104 propose a synchronized output observer.
The pinned [TTY setup](https://raw.githubusercontent.com/charmbracelet/bubbletea/v2.0.10/tty_unix.go) at lines 32–34 requires a `term.File` output with a terminal FD.
The pinned [program](https://raw.githubusercontent.com/charmbracelet/bubbletea/v2.0.10/tea.go) at lines 646–653 starts resize handling only with terminal output.
Require the observer to preserve this interface and forward the PTY slave FD.
Assert that ioctl and SIGWINCH change model dimensions without direct size-message injection.

### Synchronized output

Phase 3 lines 75 and 90–92 define partial-write cleanup.
The pinned [renderer](https://raw.githubusercontent.com/charmbracelet/bubbletea/v2.0.10/cursed_renderer.go) at lines 539–564 wraps frames with mode 2026 enable and disable sequences.
Its close path at lines 163–263 has no explicit mode-2026 reset.
Inject recoverable failure after enable and before disable.
Require cleanup through the output owner to disable synchronized output.
Add this mode to explicit exit assertions.
Record permanent writer failure as an inability to restore terminal bytes, never a successful restore.

### Valid RED evidence

Phase 1 line 90 requires a failing stock normal-exit check without a reproduced defect.
The repeated phase checklists also require RED for every missing test.
A correct stock baseline can pass immediately.
Keep test-first development for new fixture behavior and regressions.
Record initial PASS for already-correct baseline controls.
Never manufacture a failure or weaken a test to satisfy TDD bookkeeping.
Require a reproduced failing terminal assertion before any renderer fix.

## Rejected concerns

The inspected FSL license permits internal use and therefore does not prohibit the user-confirmed reuse route.
The plan retains notices and provenance and requires reassessment if distribution changes.
No network control service, personal terminal-history capture, or unowned process termination is proposed.
Do not add unrelated product security scope.
