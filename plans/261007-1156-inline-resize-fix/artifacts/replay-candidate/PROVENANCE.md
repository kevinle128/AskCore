# Source and license

The source starts from the immutable T0 phase-05 archive.
Bubble Tea is pinned to `charm.land/bubbletea/v2@v2.0.10`.
Ultraviolet is pinned to `v0.0.0-20260703014108-f5a850f9c2b7` and remains unmodified.
The exact graph remains in `source/go.mod` and `source/go.sum`.
The copied Bubble Tea dependency contains the local patch.
The separate `source/bubbletea` copy includes its renderer test.
The upstream MIT license remains in each copied library.
The modified library files are `tea.go`, `input.go`, `screen.go`, and `cursed_renderer.go`.
The new API and regression files are `inline_replay.go` and `replay_test.go`.
The patches compare modified files with the pinned source.
No Grok source was copied.
The accepted design follows the measured Grok approach in the plan report.
No root production Go file, dependency, or prior archive was changed.
