# Inline replay implementation review

Status: DONE

The frozen candidate is ready for promotion after independent verification and protected-file hash checks finish.
The archive manifest SHA256 is `788dee6e7aadb89d076ec4c4f9348f27f067539f0e3efae16df4125c6a1f2aa2`, which this reviewer checked.
No runtime tests or native sessions were started by this reviewer.

The reviewed model pauses new commits during resize and waits for a pending physical write acknowledgement before taking its replay snapshot.
The transcript stores confirmed blocks without a 4000-row truncation.
The command copies the entry slice, and the renderer serializes replay under its existing lock.
Replay uses the byte-write path, while ordinary commit observations use the insertion string-write path.
Replay results do not advance the transcript frontier.
Changed sizes reset the in-flight flag and start a generation-tagged timer, which addresses the stale-result scheduling concern.
Height-only changes also start repair.

The shared replay oracle checks ordered marker counts in every observed purge generation.
The shared and pending-resize oracles now reject alternate-screen modes 47, 1047, and 1049.
The pending case verifies actual replay dimensions and retains strict ordinary commit and acknowledgement counts.

The real PTY replay failure check now writes an eight-byte synchronized-output BEGIN prefix and then fails the owner write.
It checks the failure latch, terminal restoration after Run, synchronized-output reset, and absence of purge or content retry.
The renderer regression checks one synchronized BEGIN/END pair and a complete live redraw.
The inspected output path restores its original writer and synchronization setting before the physical owner write.

Using 13-column, 6-row and 40-column, 12-row sizes for the default pending-write case is sound if the retained plain-process controls establish full 2000-marker capacity there.
The count, order, uniqueness, and application-state checks must remain strict.
The observed 52-column, 15-row Ghostty capacity failure must remain a separate failed probe with its plain-process controls.
Passing supported sizes does not certify native history retention at 52 columns or at arbitrary sizes.

The retained worker logs report 43 Go tests, race, vet, build, and format passing.
The native result file records all 18 cases passing across Alacritty and Ghostty, with D14 false and headless-only true.
The runner copies the vendored graph and checks selection of every top-level source test.
The separate Bubble Tea renderer test is retained worker evidence and is not part of the runner's root module test selection.
Provenance records Ultraviolet as unmodified, so the abandoned origin-mapping patch is not part of this replay candidate.
The tool hook blocked direct vendor-tree access during this review.
Independent tester or controller verification of vendor hashes and local patch equality remains required before promotion.
No blocking code defect was found in the reviewed replay paths.
Root production contracts and prior immutable archives remain outside the candidate scope.
Real iTerm2 and D14 acceptance remain pending.
