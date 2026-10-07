# Inline resize replay candidate

Run these commands from `source/` with Go 1.27.0 and GOWORK=off.
The copied dependencies permit a build without network access.

```sh
GOTOOLCHAIN=local GOWORK=off go test -count=1 -timeout=120s ./...
GOTOOLCHAIN=local GOWORK=off go test -race -count=1 -timeout=120s ./...
GOTOOLCHAIN=local GOWORK=off go vet ./...
GOTOOLCHAIN=local GOWORK=off go build -o /tmp/askcore-replay-candidate .
```

Run the native checks from the repository root.

```sh
python3 e2e/tui/run.py --target /tmp/askcore-replay-candidate --artifacts /tmp/askcore-replay-native
```

The application retains all committed blocks.
A size change starts a generation-tagged 120 ms timer, including height-only changes.
New commits pause until the current physical write is acknowledged.
The renderer writes one synchronized repair: `ESC[2J ESC[3J ESC[H`, startup, confirmed transcript, and current live view.
Repair does not acknowledge a commit.
Later commits resume through the existing insertion path.
This accepted contract clears history that existed before application startup.

The final worker checks passed 43 Go tests, race, vet, build, and format.
The renderer regression checks one synchronized BEGIN/END pair per successful repair.
A real PTY check writes the BEGIN prefix, then fails the repair output.
The owner latches the failure and restores termios and terminal modes without retrying repair content.
All 18 current headless native cases passed on Alacritty and Ghostty.
The pending-write check resizes 13x6 to 40x12 and retains all 2000 lines.
Ghostty at 52x15 retains only 1096 or 1097 lines in independent plain-output controls.
The application still retains all 2000 lines, but full native retention at that size is not certified.
The capacity controls and original failure remain in `evidence/capacity-controls/`.
Terminal.app and iTerm2 were not checked.
D14 remains false.

The purge-contract and superseded-generation logs record behavioral RED/GREEN.
The partial-replay RED records the missing fixture injection seam, not a renderer defect.
Early timeout logs record fixture status matching errors.
The first native long-draft failures record a viewport oracle error corrected in the owning runner.
Ordinary commit and error contracts remain unchanged.
See `PROVENANCE.md`, `patches/`, and `source/archive-sha256.json`.
