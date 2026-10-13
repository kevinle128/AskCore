# Spike: bounded output buffer versus file as stdout

Date: 2026-10-10. Host: macOS 25.6 (arm64, 16 cores, Go 1.27.0). Linux was not run.
Status: the spike code is deleted. This report is the only artifact.

## Question

Is a mutex-guarded in-memory buffer (head 20 KB, tail 30 KB, inside 50 KB, 2000-line cut for the model view) enough under very heavy output? Does it stall the child, burn CPU, grow memory, or delay the kill? Compare with giving the child a file as stdout and stderr.

Source of the design: `plans/architectures/h5-builtin-tools-architecture.md` (Process runner) and `plans/reports/xia-261010-1727-h5-builtin-tools.md` (decision 2).

## Method

One Go test file with build tag `spike` in `internal/sandbox`. It ran `bash -c <cmd>` with `Setpgid: true`, stdin nil. Each scenario ran in three variants:

- `pipe`: two `os.Pipe` pairs, one reader goroutine per stream (32 KB read buffer), one shared buffer with a mutex. The buffer keeps the first 20 KB, a 30 KB tail ring, the total count, and the dropped count (invariant `dropped = total - head - tail` was checked). A 50 ms poller took a snapshot and built the 2000-line view, to test lock contention.
- `file`: the child writes to one `*os.File` (stdout and stderr share it). The head and tail were read with `ReadAt`. The same 50 ms poller read the tail with `ReadAt`. A guard killed the group when the file passed 4 GiB so the spike could not fill the disk (56 GB free).
- `null`: stdout and stderr go to `/dev/null`. This is the baseline for "no consumer cost".

Kill: `kill(-pgid, SIGTERM)`, then `kill(-pgid, SIGKILL)` after 3 s. The SIGKILL timer stays armed after the main process exits. After `Wait`, the pipe drain was bounded to 3 s. On drain timeout the spike sent SIGKILL to the group and closed the read ends. A 30 s guard watched `Wait` and the readers. CPU: `Getrusage` SELF and CHILDREN before and after. Heap: `ReadMemStats` every 100 ms.

Each cell is one run. The numbers show orders of magnitude, not precise values.

## Results

Columns: produced = bytes counted (pipe) or file size (file); Go CPU = CPU of the Go process; child CPU = reaped children; heap/sys = peak `HeapAlloc` / peak `Sys`; kill-to-Wait = time from the kill call to `Wait` return; gone = `kill(-pgid,0)` returned ESRCH at `Wait` return (after cleanup); content = head and tail check against the known byte pattern; poll max = longest 50 ms snapshot (includes the lock wait).

### 1. `yes`, kill after 10 s

| Variant | Produced | Wall | Go CPU | Child CPU | Heap / Sys | Kill-to-Wait | Gone | Content | Poll max | Hang |
|---|---|---|---|---|---|---|---|---|---|---|
| pipe | 58.2 GB | 10.01 s | 6.34 s | 6.04 s | 3.9 / 14.3 MB | 0.00 s | yes (yes) | exact | 0.2 ms | no |
| file | 4.3 GB at guard stop | 5.35 s | 0.02 s | 1.63 s | 0.7 / 14.3 MB | 0.00 s | yes (yes) | exact | 14.4 ms | no |
| null | n/a | 10.00 s | 0.01 s | 9.97 s | 0.4 / 14.3 MB | 0.00 s | yes (yes) | n/a | n/a | no |

The file variant tripped the 4 GiB guard at 5.35 s (about 0.8 GB/s). At that rate the file would reach about 8 GB in 10 s.

### 2. `yes | head -c 2000000000` (no kill)

| Variant | Produced | Wall | Go CPU | Child CPU | Heap / Sys | Gone | Content | Poll max | Hang |
|---|---|---|---|---|---|---|---|---|---|
| pipe | 2,000,000,000 | 0.52 s | 0.31 s | 0.69 s | 1.1 / 14.3 MB | yes (yes) | exact | 0.1 ms | no |
| file | 2,000,000,000 | 2.90 s | 0.01 s | 1.58 s | 0.6 / 14.4 MB | yes (yes) | exact | 13.4 ms | no |
| null | n/a | 0.35 s | 0.00 s | 0.46 s | 0.4 / 14.4 MB | yes (yes) | n/a | n/a | no |

### 3. 50 MB without a newline

| Variant | Produced | Wall | Go CPU | Child CPU | Heap / Sys | Gone | Content | Poll max | Hang |
|---|---|---|---|---|---|---|---|---|---|
| pipe | 50,000,000 | 1.58 s | 0.05 s | 1.58 s | 3.8 / 14.4 MB | yes (yes) | exact | 0.1 ms | no |
| file | 50,000,000 | 1.69 s | 0.01 s | 1.61 s | 1.4 / 14.4 MB | yes (yes) | exact | 1.2 ms | no |
| null | n/a | 1.58 s | 0.00 s | 1.58 s | 0.4 / 14.4 MB | yes (yes) | n/a | n/a | no |

The producer (`tr`) is the limit here. The view code has no newline to cut at and still returned in 0.1 ms.

### 4. Grandchild keeps the pipe open (`(sleep 30 &); echo started`)

| Variant | Produced | Wall | Drain | Gone at Wait return | Gone after cleanup | Content | Hang |
|---|---|---|---|---|---|---|---|
| pipe | 8 | 3.01 s | timed out at 3.00 s | no | yes (0.01 s, SIGKILL) | exact | no |
| file | 8 | 0.01 s | none | no | yes (0.01 s, SIGKILL) | exact | no |
| null | n/a | 0.01 s | none | no | yes (0.01 s, SIGKILL) | n/a | no |

In all variants `Wait` returned at once with the `sleep` still alive in the group. In the pipe variant the readers stayed blocked until the drain bound (3 s). Then SIGKILL to the group plus closing the read ends unblocked both readers. No hang.

### 5. Process that ignores SIGTERM (`trap '' TERM; while true; do echo x; done`), kill after 2 s

| Variant | Produced | Wall | Go CPU | Child CPU | Heap / Sys | Kill-to-Wait | Gone | Content | Hang |
|---|---|---|---|---|---|---|---|---|---|
| pipe | 5.2 MB | 5.01 s | 3.13 s | 4.93 s | 3.9 / 14.4 MB | 3.00 s | yes (yes) | exact | no |
| file | 3.9 MB | 5.00 s | 0.01 s | 4.81 s | 0.7 / 14.4 MB | 3.00 s | yes (yes) | exact | no |
| null | n/a | 5.00 s | 0.01 s | 4.99 s | 0.4 / 14.4 MB | 3.00 s | yes (yes) | n/a | no |

### 6. Extra: both streams hot (`(yes >&2 &); yes`), kill after 5 s

| Variant | Produced | Wall | Go CPU | Heap / Sys | Kill-to-Wait | Gone at return (after cleanup) | Content | Poll max | Hang |
|---|---|---|---|---|---|---|---|---|---|
| pipe | 43.7 GB | 5.00 s | 8.50 s | 3.8 / 14.6 MB | 0.00 s | yes (yes) | alphabet only | 0.2 ms | no |
| file | 4.4 GB at guard stop | 4.21 s | 0.01 s | 0.7 / 14.6 MB | 0.00 s | no (yes, 0.01 s) | alphabet only | 12.4 ms | no |
| null | n/a | 5.00 s | 0.01 s | 0.4 / 14.6 MB | 0.00 s | yes (yes) | n/a | n/a | no |

Child CPU reads 0.00 s here because the `yes` processes are orphans reaped by launchd, not by the Go process. Content was checked by alphabet only because two streams interleave at read boundaries.

## Findings

1. Memory is bounded. Peak heap stayed at 1 to 4 MB and peak `Sys` was flat at about 14 MB in every scenario, including 58 GB and 44 GB of output.
2. No stall. The pipe variant moved 5.8 GB/s with one hot stream and 8.7 GB/s with two. Scenario 2 took 0.52 s against 0.35 s for `/dev/null`. The 32 KB read size and one goroutine per stream keep up with `yes`.
3. CPU is proportional to the output rate. Under a flood the Go process used 0.6 to 0.85 of a core per hot stream (6.3 s over 10 s; 8.5 s over 5 s for two streams). It stops when the child stops. The cost is mostly read syscalls (32 KB each), not copying: the ring copy touches at most 30 KB per read.
4. No lock contention. A 50 ms snapshot plus 2000-line view took at most 0.2 ms while two readers wrote at full speed.
5. The kill is not delayed by the buffer. A child that handles SIGTERM exits within 1 ms. A child that ignores SIGTERM is gone at 3.00 s (the grace). The process group was gone every time after the SIGKILL.
6. Pipe readers can hang on a grandchild. The bounded drain solved it: close the read ends after 3 s (this unblocks a `Read` on an `os.File` pipe) and send SIGKILL to the group.
7. The file variant protects the Go process (CPU near 0) but it costs disk and it slows the child. The child wrote at about 0.8 to 1.0 GB/s (scenario 2: 2.9 s against 0.52 s for the pipe). The file grows without bound (4 GiB in about 5 s). Without a size guard, 10 s of `yes` writes about 8 GB. On Linux with `/tmp` as tmpfs this would be memory, not disk (not tested).
8. `Wait` returned before the last group member was gone in scenario 4 (all variants) and once in scenario 6 (file). `Wait` return does not prove the group is gone.

## Conclusion against the decision rule

Rule: keep the buffer if memory stays bounded and the kill completes within the grace; otherwise switch to file-as-stdout for that case.

- Memory stayed bounded in all 6 scenarios.
- The kill completed within the grace in all 6 scenarios (0.00 s, or 3.00 s for the SIGTERM-ignoring process).
- No `Wait` or reader hang occurred with the 3 s bounded drain.

Decision: keep the in-memory buffer for every scenario. No case needs file-as-stdout.

## Facts the plan must keep

- Read buffer 32 KB; one reader goroutine per stream (stdout, stderr); one shared mutex-guarded buffer. This is enough.
- Give the child `*os.File` pipe ends (`os.Pipe`) as `cmd.Stdout` and `cmd.Stderr`, and close the write ends in the parent after `Start`. Then `Wait` does not wait for the pipe readers. If an `io.Writer` is used instead, `exec` starts its own copy goroutine and `Wait` can block on a grandchild unless `WaitDelay` is set (known Go behavior; not measured here).
- Pipe drain after `Wait` returns: bounded to 3 s (same as the kill grace). On timeout: SIGKILL to the group, then close both read ends. Measured: drain timed out at 3.00 s, readers ended at once after the close.
- Keep the SIGKILL timer armed after the main process exits. After `Wait`, check `kill(-pgid, 0)`; if the group is still alive, SIGKILL it and poll (took 10 ms). Tests must assert ESRCH after cleanup, not at `Wait` return.
- Build the model view outside the lock: copy head and tail under the lock, then cut to 2000 lines. The view code must handle a tail with no newline.
- Count dropped bytes as `total - head - tail`; the invariant held in all pipe runs.
- Spill file (open point, needs a decision): the design says "a spill file only on overflow". When output passes 50 KB, the runner must copy head and ring into the file and then write every later chunk (a tee). Under a flood the tee runs at disk speed (0.8 to 1.0 GB/s here), which slows the reader and then the child, and the file grows without bound. The tee itself was not measured. The plan needs a spill size cap (and a marker such as "output beyond the cap not saved"), or a decision that heavy output is simply dropped from the file. The `monitor` overload kill does not cover the `bash` tool.
- CPU under a flood is bounded only by the timeout or the model's kill. A very long flood costs about one core until killed. If this matters, the job table could kill a job that exceeds a byte-rate limit; the spike did not need it.

## Limits of the spike

- One run per cell, macOS only. Linux pipe capacity (64 KB default) and scheduler differ; the design does not depend on either, but the throughput numbers will differ.
- Scenario 6 content was checked by alphabet only.
- The tee to a spill file and a read buffer larger than 32 KB were not tested.

## Unresolved questions

1. Spill cap: what maximum size, and what happens beyond it (stop writing, or kill the job)? This touches the user's decision on spill files, so it needs the user.
