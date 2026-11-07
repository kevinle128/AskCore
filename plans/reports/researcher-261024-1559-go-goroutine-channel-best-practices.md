# Go goroutines, channels, and pools for an async task runtime

Date: 2026-10-24. Scope: background shell commands, sub-agents, timers, file watchers, MCP notifications, out-of-process hooks. Requirement: a completion wakes the agent with no sleep and no polling.

## 1. Outcome (read this first)

Recommended design, ranked:

1. **Goroutine per task, started when work arrives, with one `semaphore.Weighted` per task class as a bulkhead.** Classes: `shell`, `subagent`, `hook`. Timers and file watchers use no slot. Rejected alternatives: a generic worker pool (Mills argues against it), `ants` (exists to reuse goroutines for memory), `conc` (last release Feb 2023, pre-1.0).
2. **Completions are data. Wake-ups are signals.** Store each completion in a mutex-protected pending list. Wake the agent loop with a 1-buffered, non-blocking-send `chan struct{}`. The agent loop drains the whole list after each wake. This gives batching for free and never loses a completion.
3. **One root `context` with a cause per runtime.** Every blocking send, receive, and `Acquire` selects on `ctx.Done()`. Every task has an owner that waits for it (`sync.WaitGroup.Go` under an admission mutex).
4. **Per-task `recover` inside the task body.** The standard library offers no panic safety (`errgroup` and `WaitGroup.Go` both refuse to recover). A misbehaving tool must not kill the daemon.
5. **Polling is allowed only inside one isolated `poller` component** for remote systems without push. It uses a `time.Timer`, backoff with jitter, and `ctx`. It emits the same `Completion` type as every other source.
6. **Fallback if the team wants a bounded queue, `TrySubmit`, and metrics built in:** `alitto/pond` v2 (MIT, release 2026-10-17). Not recommended as the first choice. See section 12.

Go version: `go.mod` declares `go 1.27.0`, and the local toolchain is `go1.27.0` (checked). Every API in this report is available without flags. `golang.org/x/sync v0.22.0` is an **indirect** requirement in `go.mod`. Promote it to a direct requirement when the first import is added (not done here; this task changes no other file).

Verified Go 1.25 to 1.27 facts that change old advice:

- `testing/synctest` is GA since 1.25. `synctest.Sleep` was added in 1.27. [synctest docs](https://pkg.go.dev/testing/synctest), [1.25 notes](https://go.dev/doc/go1.25), [1.27 notes](https://go.dev/doc/go1.27)
- `sync.WaitGroup.Go` exists since 1.25. Its `f` must not panic. [sync docs](https://pkg.go.dev/sync#WaitGroup.Go)
- Timer channels are unbuffered since 1.23. In 1.27 the `asynctimerchan` setting is removed, so the "Stop then drain then Reset" pattern is obsolete. [1.23 notes](https://go.dev/doc/go1.23), [1.27 notes](https://go.dev/doc/go1.27)
- The `goroutineleak` pprof profile is GA in 1.27. It finds goroutines blocked forever on unreachable primitives. [1.27 notes](https://go.dev/doc/go1.27)
- `os/signal.NotifyContext` cancels with a cause that names the signal (1.26). [1.26 notes](https://go.dev/doc/go1.26)
- The vet analyzer `waitgroup` was renamed `waitgroupgo` in 1.27.

## 2. Source credibility

| Tier | Source | Used for |
|---|---|---|
| Primary | Go release notes, `pkg.go.dev` package docs, Go memory model, Go Code Review Comments, Go blog (pipelines) | Rules and API semantics |
| Primary (code) | `golang/sync` source (`errgroup.go`, `semaphore.go`), read directly | Panic and FIFO behavior |
| Maintainer-style guides | Uber Go Style Guide, Dave Cheney "Practical Go", Effective Go | Lifetime and channel-size rules |
| Talk | Mills, GopherCon 2018 (video listing, Sourcegraph liveblog) | Mutex vs channel, pools, futures |
| Vendor README | `ants`, `pond`, `conc` READMEs, GitHub metadata via `gh api` | Pool comparison |

Limit: the Mills slide deck is behind a Google sign-in and could not be read. Claims about it come from the liveblog only (section 15).

## 3. Goroutine lifetime

- **Rule: when you start a goroutine, make clear when and how it ends.** A goroutine blocked on a channel is never garbage collected. [Code Review Comments: Goroutine Lifetimes](https://go.dev/wiki/CodeReviewComments#goroutine-lifetimes)
- **Rule: never start a goroutine without knowing when it will stop. A function that starts one must give the caller a way to stop it.** [Cheney, Practical Go section 8](https://dave.cheney.net/practical-go/presentations/qcon-china.html)
- **Rule: every goroutine has a predictable end or accepts a stop signal, and the owner can block until it finishes.** [Uber: Don't fire-and-forget](https://github.com/uber-go/guide/blob/master/style.md#dont-fire-and-forget-goroutines), [Uber: Wait for goroutines to exit](https://github.com/uber-go/guide/blob/master/style.md#wait-for-goroutines-to-exit)
- **Rule: no goroutines in `init()`. Expose an object with `Close` or `Shutdown` that signals and waits.** [Uber](https://github.com/uber-go/guide/blob/master/style.md#no-goroutines-in-init)
- **Rule: prefer synchronous functions. Callers add concurrency by calling from their own goroutine.** Removing concurrency later is hard. Synchronous code also needs no polling in tests. [Code Review Comments: Synchronous Functions](https://go.dev/wiki/CodeReviewComments#synchronous-functions)
- **Rule: use `WaitGroup.Go` (1.25+) instead of `Add` plus `go` plus `defer Done`.** It removes the "Add inside the goroutine" bug. [sync docs](https://pkg.go.dev/sync#WaitGroup.Go)
- **Rule: a goroutine owned by a type is started by a method, not by the constructor, unless the constructor documents it.** Owner type pattern:

```go
type Runtime struct {
    mu     sync.Mutex
    closed bool
    wg     sync.WaitGroup
    ctx    context.Context
    cancel context.CancelCauseFunc
}

// Go admits a task only while the runtime is open. The admission check and
// wg.Go share one critical section, so Wait cannot start between them.
func (r *Runtime) Go(f func(ctx context.Context)) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.closed {
        return ErrRuntimeClosed
    }
    r.wg.Go(func() { f(r.ctx) })
    return nil
}
```

  Why the mutex: `WaitGroup.Go` on an empty group must happen before `Wait`. [sync docs](https://pkg.go.dev/sync#WaitGroup.Go)
- **Rule: a task that starts child tasks registers the child before it returns.** Otherwise shutdown can miss nested tasks. (Skill `agent-background-task-push-notifications`, step 2.)
- **Leak detection in production:** the 1.27 `goroutineleak` profile at `/debug/pprof/goroutineleak`. It misses leaks on primitives reachable from globals or from runnable goroutines. [1.27 notes](https://go.dev/doc/go1.27)

## 4. Channel ownership

- **Rule: the creator owns the channel. Only a sender closes it. A receiver never closes it.** A send on a closed channel panics. [Pipelines](https://go.dev/blog/pipelines), [Code Review Comments](https://go.dev/wiki/CodeReviewComments#goroutine-lifetimes)
- **Rule: with many senders, no sender closes. One coordinator does `wg.Wait()` and then `close(c)`.** The coordinator starts after all `wg.Add` calls. [Pipelines](https://go.dev/blog/pipelines)
- **Rule: if you cannot prove all senders are done, do not close the data channel. Signal shutdown with a separate `done` channel or `ctx`.** Cheney reads `cap(done)` values instead of ranging over a channel that cannot be closed safely. [Cheney](https://dave.cheney.net/practical-go/presentations/qcon-china.html)
- **Rule: close is a broadcast.** A receive on a closed channel always proceeds, and the close is synchronized before that receive (memory model). [Pipelines](https://go.dev/blog/pipelines), [Memory model](https://go.dev/ref/mem)
- **Rule: use directional types (`chan<- T`, `<-chan T`) in signatures.** The compiler then enforces who can send and who can close. [Go spec: Channel types](https://go.dev/ref/spec#Channel_types)
- **Rule: a nil channel blocks forever. In `select`, set a channel variable to `nil` to disable that case.** [Go spec: Select statements](https://go.dev/ref/spec#Select_statements)

```go
for in != nil || pending > 0 {
    select {
    case v, ok := <-in:
        if !ok { in = nil; continue } // disable the case once closed
        handle(v)
    case <-ctx.Done():
        return context.Cause(ctx)
    }
}
```

- **Rule: do not create a channel of channels without a reason.** A request that carries its own reply channel is the accepted form. Make the reply channel 1-buffered so the replier never blocks. [Effective Go: Channels of channels](https://go.dev/doc/effective_go#chan_of_chan)

### Buffered vs unbuffered

- **Rule: size is none or one. Any other size needs a written reason: how it was chosen, what happens when it fills.** [Uber: Channel Size is One or None](https://github.com/uber-go/guide/blob/master/style.md#channel-size-is-one-or-none)
- A buffer is a **correctness tool** in three cases:
  1. Exactly one result per sender, and the receiver may leave early (future): `make(chan Result, 1)`. [Pipelines](https://go.dev/blog/pipelines), [liveblog](https://github.com/sourcegraph/gophercon-2018-liveblog/issues/35)
  2. A coalescing signal: `make(chan struct{}, 1)` (section 7).
  3. A counting semaphore: capacity equals the limit. [Memory model](https://go.dev/ref/mem), [Effective Go](https://go.dev/doc/effective_go#channels)
- A buffer is a **hiding place for bugs** when you pick a number so that a leak or a deadlock does not show in tests. The Go blog calls the "buffer of 1 so the sender fits" fix in `merge` "bad code" because it breaks when inputs change. [Pipelines](https://go.dev/blog/pipelines)
- **Rule: an unbuffered channel proves hand-off. A buffered channel does not.** For capacity C, only the k-th receive is synchronized before the (k+C)-th send. [Memory model](https://go.dev/ref/mem)

## 5. Cancellation

- **Rule: `ctx` is the first parameter. Do not store it in a struct. The one exception is a method that must satisfy an existing interface.** [Code Review Comments: Contexts](https://go.dev/wiki/CodeReviewComments#contexts), [context docs](https://pkg.go.dev/context)
  Note: a runtime that owns a root context (the `Runtime` above) is an owner, not a request. Document it. Pass `ctx` explicitly into every task function.
- **Rule: always call `cancel`. A `CancelFunc` does not wait for the work to stop.** Pair it with a `WaitGroup`. [context docs](https://pkg.go.dev/context)
- **Rule: every blocking send, receive, and `Acquire` selects on `ctx.Done()`.**

```go
select {
case out <- ev:
case <-ctx.Done():
    return context.Cause(ctx)
}
```

- **Rule: use `WithCancelCause` for the runtime root.** `ctx.Err()` stays `context.Canceled`. `context.Cause(ctx)` returns your reason (`ErrShutdown`, `ErrUserAbort`). The first cancel sets the cause. [context docs](https://pkg.go.dev/context#WithCancelCause)
- **Rule: use `WithTimeoutCause` / `WithDeadlineCause` for deadlines.** The returned `CancelFunc` does not set the cause. [context docs](https://pkg.go.dev/context#WithTimeoutCause)
- **Rule: use `context.WithoutCancel` for work that must outlive the request context** (for example, persisting a final completion after abort). Give it its own timeout. It returns no `Done` channel and no cause. [context docs](https://pkg.go.dev/context#WithoutCancel)
- **Rule: use `context.AfterFunc(ctx, f)` to bridge `ctx` to an API that does not take a context.** `f` runs in its own goroutine. The returned `stop` does not wait for `f`. Coordinate completion yourself. [context docs](https://pkg.go.dev/context#AfterFunc)

```go
stop := context.AfterFunc(ctx, func() { conn.Close() }) // unblocks conn.Read
defer stop()
```

- **Rule: cancellation of child processes needs `Cmd.Cancel` and `Cmd.WaitDelay`** (section 11). [os/exec docs](https://pkg.go.dev/os/exec#Cmd)

## 6. Mutex vs channel

- **Rule: protect state with a mutex. Use channels to move ownership of data or to signal events.** Effective Go itself says "share memory by communicating" "can be taken too far" and that a reference count may be best done with a mutex. [Effective Go](https://go.dev/doc/effective_go#sharing)
- **Rule: the zero-value `sync.Mutex` is valid. Use a named field `mu sync.Mutex`, not an embedded one, and never copy a struct that holds a mutex (pointer receivers).** [Uber](https://github.com/uber-go/guide/blob/master/style.md#zero-value-mutexes-are-valid), [Code Review Comments: Receiver Type](https://go.dev/wiki/CodeReviewComments#receiver-type)
- **Rule: a map or slice returned from under a lock escapes the lock. Return a copy.** [Uber](https://github.com/uber-go/guide/blob/master/style.md#copy-slices-and-maps-at-boundaries)
- **Rule: a mutex guards a short critical section. Never hold it across a channel send, a `Wait`, or a callback.** (Judgment rule. It follows from the mutex rules in the memory model: a held lock blocks every other caller.) [Memory model](https://go.dev/ref/mem)
- Mills' point (talk, per the liveblog): condition variables share memory to communicate, and they have four problems. Spurious wake-ups from `Broadcast`, forgotten signals, starvation of pickier waiters, and unresponsive cancellation (`Cond.Wait` cannot select on `ctx`). [liveblog](https://github.com/sourcegraph/gophercon-2018-liveblog/issues/35)
- **Rule: replace `sync.Cond` with channels.** The `sync` docs say the same: "Broadcast corresponds to closing a channel, and Signal corresponds to sending on a channel." [sync.Cond docs](https://pkg.go.dev/sync#Cond)
- If you keep `sync.Cond`: wait in a `for !cond() { c.Wait() }` loop, and hold `c.L` while you change the condition. [sync.Cond docs](https://pkg.go.dev/sync#Cond)

Decision table:

| Need | Use |
|---|---|
| Guard a map of tasks, a counter, a pending list | `sync.Mutex` |
| Wake a consumer when state changed | 1-buffered signal channel plus re-read state |
| Tell everybody "we are stopping" | `close(done)` or `ctx.Done()` |
| Hand one result to one waiter | 1-buffered result channel |
| Limit concurrent use | `semaphore.Weighted` or buffered channel |
| Wait for a condition with cancel | channel + `select` (not `sync.Cond`) |

## 7. Wake and notify without polling

This section is the core of the harness requirement.

### 7.1 Signals versus completions (central trap)

- A **signal** says "state changed, go look." It may be coalesced. A 1-buffered channel with a non-blocking send is correct only when the waker **re-reads the state after waking**.
- A **completion** (process exit, sub-agent result, hook return, MCP notification) is **data**. It must never be coalesced or dropped.
- Therefore: store completions in a mutex-protected list, and use the signal channel only to wake the consumer. "Events can be data" and "events can be completions" is the Mills framing. [liveblog](https://github.com/sourcegraph/gophercon-2018-liveblog/issues/35)

```go
type Inbox struct {
    mu      sync.Mutex
    pending []Completion
    wake    chan struct{} // cap 1
}

func NewInbox() *Inbox { return &Inbox{wake: make(chan struct{}, 1)} }

// Post never blocks. Completion data stays in pending; only the signal coalesces.
func (b *Inbox) Post(c Completion) {
    b.mu.Lock()
    b.pending = append(b.pending, c)
    b.mu.Unlock()
    select {
    case b.wake <- struct{}{}:
    default: // a wake-up is already queued; the consumer will drain pending
    }
}

// Drain returns every completion posted so far. One wake needs one Drain: a Post
// that lands after Drain queues a new signal, so no completion is left unsignaled.
func (b *Inbox) Drain() []Completion {
    b.mu.Lock()
    defer b.mu.Unlock()
    out := b.pending
    b.pending = nil
    return out
}

// Agent loop (idle):
//   for { select { case <-in.wake: batch := in.Drain(); runOnce(batch)
//                  case <-ctx.Done(): return } }
```

- Draining the full list gives batching: several completions near each other start one agent run, not one run each. This matches the installed skill (`agent-background-task-push-notifications`): "batch many completions into one request."
- Lost-wake-up check: `Post` appends before it signals. A consumer that receives the signal then calls `Drain` sees the data. If `Post` runs between `Drain` and the next receive, it queues a new signal. No completion waits without a signal.
- **Bound the list.** Each `Completion` carries a capped output preview (or a path), and the task registry caps the number of unconsumed completions. State the cap in the type's doc comment.
- Alternative (simpler code, slightly worse): an intake channel with `cap = sum of class limits`. Senders use `select { case ch <- c: case <-ctx.Done(): }`. A slow agent loop then blocks task goroutines (and holds their slots). That is backpressure, but it delays process reaping if the send precedes cleanup. Release OS resources before the send.
- Running vs idle (skill guidance): while a run is active, the consumer takes completions at a turn boundary. While idle, the dispatcher selects on `wake`. Do not hold a run open with a timer or `recv_timeout`.

### 7.2 Other wake patterns

- **Close to broadcast.** One-shot, many waiters (shutdown, "task finished"): `close(done)`. Do it exactly once, for example with `sync.OnceFunc` (1.21+). If the function panics, every later call panics with the same value. [sync docs](https://pkg.go.dev/sync#OnceFunc)

```go
closeDone := sync.OnceFunc(func() { close(t.done) })
```

- **Condition with a timeout:** `select` on the event channel and a `time.Timer`. Never `time.Sleep` in a loop to wait for a condition.
- **Timers (1.23+ semantics, mandatory in 1.27):**
  - Timer channels are unbuffered. After `Stop` or `Reset`, no stale value arrives. Delete the old drain code. `len(t.C)` is always 0. [1.23 notes](https://go.dev/doc/go1.23)
  - Unreferenced timers are garbage collected, but call `Stop` anyway when you cancel. [1.23 notes](https://go.dev/doc/go1.23)
  - For "run this at time T" use `time.AfterFunc(d, f)`. No goroutine exists while it waits. `f` runs in its own goroutine, so `f` must post to the `Inbox` and return, and must respect shutdown. (Behavior from the `time` package docs: [time.AfterFunc](https://pkg.go.dev/time#AfterFunc); not re-fetched here.)
  - For repeating work, create one `time.Timer`, `Reset` it from the loop `select`. Do not use `time.After` inside a hot loop: since 1.23 it does not leak, but it allocates a new timer and channel on each pass.
- **`sync.Cond`:** avoid (section 6). Use `context.AfterFunc(ctx, c.Broadcast)` only if you must keep a legacy Cond cancellable.
- **File watchers:** the watcher library delivers events on a channel. Debounce with one `Timer` and treat each fire as a signal ("rescan"), not as a count of changes. Library details were not verified in this research (section 15).
- **MCP notifications:** the transport reader goroutine must never block on a slow consumer. It calls `Inbox.Post` (non-blocking) and continues to read.

## 8. Backpressure and bounded queues

- **Rule: every queue has a bound and a stated policy when full.** Choose one per data kind:

| Data kind | Policy when full |
|---|---|
| Completion (process exit, sub-agent result) | Never drop. Bound by registry cap, so admission rejects new tasks first |
| Task admission | Block with `ctx`, or reject (`TryAcquire`), or queue with a counted bound |
| Streaming output of a shell task | Ring buffer with byte cap and a `truncated` flag. Drop oldest |
| File-change events | Coalesce into one "rescan" signal |
| Status or progress ticks | Coalesce (latest value wins) |

- **Rule: an unbounded queue turns overload into out-of-memory.** The pipelines post limits workers with a fixed count (20) because one goroutine per file can exhaust memory. [Pipelines](https://go.dev/blog/pipelines)
- **Rule: the limit protects a downstream resource** (process table, file handles, LLM rate limit), not the goroutine count. See section 12.
- **Rule: the producer of a bounded queue must be able to block on it with `ctx`, or reject.** A silent drop of a completion is a bug.

## 9. Panics and errors

- **Rule: a panic in any goroutine without `recover` terminates the whole process.** Recover per task, inside the task body, at the top of the goroutine.
- **Fact: `errgroup` does not recover panics, on purpose.** The source comment gives four reasons: it delays panics, it hides the stack from crash monitors, it can deadlock `Wait`, and see issues #53757, #74275, #74304, #74306. [errgroup.go](https://github.com/golang/sync/blob/master/errgroup/errgroup.go)
- **Fact: `WaitGroup.Go` requires that `f` does not panic.** [sync docs](https://pkg.go.dev/sync#WaitGroup.Go)
- Libraries differ: `conc` re-panics at `Wait()` with the child stack. `pond` v2 recovers by default and returns the panic as an error. [conc README](https://github.com/sourcegraph/conc), [pond README](https://github.com/alitto/pond)
- **For this harness (must survive a faulty tool), one wrapper is the only stdlib-compatible option:**

```go
func safely(id TaskID, f func() (Result, error)) (res Result, err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("task %s panicked: %v\n%s", id, r, debug.Stack())
        }
    }()
    return f()
}
```

  The wrapper turns a panic into a `Completion` with an error. The agent then sees the failure. Do not recover in goroutines you do not own (for example, inside library callbacks).
- **Rule: every task posts exactly one completion on every exit path: success, error, panic, kill, cancel.** Put the post in a `defer`. (Skill `agent-background-task-push-notifications`, step 3.)
- **Rule: use `errgroup.WithContext` for a fixed set of sibling tasks where the first error cancels the rest** (for example, a sub-agent fan-out inside one turn). It cancels with the error as the cause. `Wait` returns the first non-nil error. [errgroup docs](https://pkg.go.dev/golang.org/x/sync/errgroup)
- **Rule: do not use `errgroup` as the long-lived task runtime.** The docs say a group "should not be reused for different tasks." [errgroup docs](https://pkg.go.dev/golang.org/x/sync/errgroup)

## 10. Shutdown

Order of operations (each step has a reason):

1. **Stop admission.** Set `closed = true` under the same mutex that guards `wg.Go`. New work now gets `ErrRuntimeClosed`.
2. **Cancel the root context with a cause:** `cancel(ErrShutdown)`. All tasks see `ctx.Done()`. Child processes receive `Cmd.Cancel` (graceful signal first).
3. **Wait for tasks with a deadline:** `wg.Wait()` in a goroutine, and `select` against a `time.After`-style timer and the caller's context. `WaitGroup.Wait` has no timeout of its own.
4. **Flush completions.** Tasks that finished while cancelling still post their completion. Use `context.WithoutCancel` plus a short timeout for the final persistence step.
5. **Close the wake channel last**, and only after all `Post` callers are gone. In practice: never close it; the consumer selects on `ctx.Done()` instead.

- **Rule: shutdown must not lose a completion and must not leak a goroutine.** Test both (section 13).
- **Rule: a `Close` method that signals and then waits is the pattern.** [Uber: No goroutines in init](https://github.com/uber-go/guide/blob/master/style.md#no-goroutines-in-init)
- **Rule: reuse of a `WaitGroup` requires that new `Go` calls happen after all previous `Wait` calls returned.** Do not reuse. Create a new runtime. [sync docs](https://pkg.go.dev/sync#WaitGroup.Go)

## 11. When polling is the only option

Push exists for most sources. Use it.

| Source | Push mechanism | Needs a bound or isolation |
|---|---|---|
| Child process exit | `cmd.Wait()` in the task goroutine | Yes: `Cmd.WaitDelay`, see below |
| Timer, deadline | `time.AfterFunc` or `Timer` | No |
| File change | OS notify library (event channel) | Debounce |
| MCP notification | Transport reader goroutine | Non-blocking `Post` |
| Hook (out of process) | `cmd.Wait()` or stdout EOF | `Cmd.WaitDelay` |
| Sub-agent in process | Its own completion channel | Cancel link to parent `ctx` |
| Remote HTTP job without callback | **None: polling** | Isolate in `poller` |

- **`cmd.Wait` is a push, but it can hang.** `Wait` also waits for the I/O copying goroutines. With a zero `WaitDelay`, pipes are read until EOF, which may not happen until orphaned grandchildren close them. [os/exec docs](https://pkg.go.dev/os/exec#Cmd)
- **Rule: set `WaitDelay` on every `exec.Cmd` the runtime starts.** After it expires, the child is killed and the pipes are closed. `Wait` may then return `ErrWaitDelay`. [os/exec docs](https://pkg.go.dev/os/exec#Cmd)
- **Rule: set `Cmd.Cancel` to a graceful signal (for example SIGTERM) instead of the default `Kill`, and let `WaitDelay` force the kill.** `Cancel` must not call `Wait`. Wait must not run from two goroutines. [os/exec docs](https://pkg.go.dev/os/exec#Cmd)
- Killing grandchildren needs a process group (`SysProcAttr`). This is OS-specific and was not verified here (section 15).
- **Isolation rule for real polling:** one `poller` component, one goroutine per remote job (or one shared loop for all), with these properties:
  - `ctx`-cancellable, and stops when the job reaches a terminal state.
  - Exponential backoff with jitter, a maximum interval, and an overall deadline. ([AWS Builders Library](https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/), not re-fetched here.)
  - Waits with `Timer.Reset` and `select`, never `time.Sleep`.
  - Emits the same `Completion` into `Inbox.Post`. The agent loop never knows polling exists.
  - The LLM never polls. The tool returns a task ID at once and the completion arrives as a follow-up message. (Skill `agent-background-task-push-notifications`, "Evaluate polling".)
- A short retry loop for a busy host call is not task polling. Review each `time.Sleep` on its own merits. (This research did not audit `internal/acp/host.go`.)

## 12. Goroutine pools and bounded concurrency

### 12.1 Do you need a pool?

- Goroutines are cheap, but "lightweight, not free" (Mills, per the liveblog). [liveblog](https://github.com/sourcegraph/gophercon-2018-liveblog/issues/35)
- **Rule: the reason to limit concurrency is the downstream resource**, not the goroutine. Here that is: OS processes (shell, hooks), LLM provider rate limits and cost (sub-agents), file descriptors. A limit on goroutine count alone has no value here.
- **Rule: a pool of long-lived idle workers is not needed.** Mills: do not "spool up idle workers that will just fill up your goroutine dumps"; treat the limit as a resource (semaphore) and start a goroutine when work arrives. [liveblog](https://github.com/sourcegraph/gophercon-2018-liveblog/issues/35)
- Counter-point: Effective Go and the Go blog both show a fixed worker pool as valid, and the pipelines post uses one (20 digesters) for bounded parallelism. A pool is justified for CPU-bound, uniform, very high-volume work, or when worker reuse saves real allocation (that is the `ants` pitch). None of these hold for this harness: tasks are few, heavy, and I/O-bound. [Effective Go](https://go.dev/doc/effective_go#channels), [Pipelines](https://go.dev/blog/pipelines)

### 12.2 Patterns

| Pattern | Use | Source |
|---|---|---|
| Buffered channel as semaphore | Simple count limit, no ctx on acquire | [Memory model](https://go.dev/ref/mem), [Effective Go](https://go.dev/doc/effective_go#channels) |
| `semaphore.Weighted` | Limit with `Acquire(ctx)`, `TryAcquire`, weights | [semaphore docs](https://pkg.go.dev/golang.org/x/sync/semaphore) |
| `errgroup.SetLimit` / `TryGo` | A bounded batch of siblings with first-error cancel | [errgroup docs](https://pkg.go.dev/golang.org/x/sync/errgroup) |
| Fixed worker pool over a channel | CPU-bound uniform work | [Pipelines](https://go.dev/blog/pipelines) |
| Bulkhead: one limiter per class | Isolate failure and starvation between classes | Design pattern (not from a Go source) |

Verified behavior of `semaphore.Weighted` (read from `golang/sync` source):

- `Acquire(ctx, n)` returns `ctx.Err()` and leaves the semaphore unchanged on cancel. It fails even if the semaphore was free, when `ctx` is already done.
- **Waiters are served in strict FIFO order.** The fast path requires `waiters.Len() == 0`. `notifyWaiters` stops at the first waiter that does not fit. The source comment says this is deliberate, to prevent starvation of large requests.
- Consequence for mixed weights: a large request at the head blocks later small requests even when tokens are free (head-of-line blocking). Use weights only when sizes are similar. Otherwise use separate semaphores.
- `TryAcquire` also returns false while any waiter is queued, even if tokens are free. It keeps FIFO fairness.
- `Release` panics with "released more than held". Pair every `Acquire` with one `defer Release`.
- `errgroup.SetLimit`: `Go` blocks when the group is at the limit. `TryGo` returns false instead. The limit must not change while goroutines are active. `SetLimit(0)` admits nothing.

### 12.3 Admission policy when full

- **Block with `ctx`** (`Acquire(ctx, 1)`): for internal callers that can wait, such as a sub-agent fan-out. The wait is cancellable. FIFO makes it fair.
- **Reject** (`TryAcquire`): for tool calls from the LLM, such as "start background shell." Return a clear error ("shell limit 8 reached"). The model can then decide. This avoids an invisible queue the model cannot see.
- **Queue with a bound:** only if a visible queue is a product need. Keep a counter and reject above the bound. `semaphore.Weighted` has no queue-depth API, so count waiters yourself with an `atomic.Int64` around `Acquire`.
- **Priority:** `semaphore.Weighted` has none. Use separate classes (a separate limiter for user-initiated work). Do not build a priority queue unless a real requirement appears.

### 12.4 Pool lifecycle, panics, metrics

```go
type Class struct {
    name     string
    sem      *semaphore.Weighted
    inflight atomic.Int64
    waiting  atomic.Int64
}

// Go runs f in a new goroutine under the class limit.
// block=false rejects at once when the class is full.
func (r *Runtime) GoIn(ctx context.Context, c *Class, block bool, f func(context.Context)) error {
    if block {
        c.waiting.Add(1)
        err := c.sem.Acquire(ctx, 1)
        c.waiting.Add(-1)
        if err != nil {
            return err
        }
    } else if !c.sem.TryAcquire(1) {
        return fmt.Errorf("%s: %w", c.name, ErrClassFull)
    }
    if err := r.Go(func(ctx context.Context) {
        defer c.sem.Release(1)
        c.inflight.Add(1)
        defer c.inflight.Add(-1)
        f(ctx) // f owns recover and posts exactly one Completion
    }); err != nil {
        c.sem.Release(1) // admission failed after Acquire
        return err
    }
    return nil
}
```

- **Start:** no workers exist until work arrives. **Drain/stop:** section 10 covers it; `Release` happens in `defer` so cancel and panic free the slot. **Panic:** `safely` inside `f`. **Metrics:** you own `inflight` and `waiting` (3 lines each). Export them to the existing event stream.
- Suggested class limits (starting values to tune, not researched numbers): `shell` and `hook` bound by process table and CPU; `subagent` bound by the LLM provider rate limit. Make them config fields.

### 12.5 Third-party pools (metadata from `gh api`, README via fetch, 2026-10-24)

| Library | License | Latest release | Status | Fit |
|---|---|---|---|---|
| `panjf2000/ants` v2 | MIT | v2.12.0 (2026-03-20) | Active | Goroutine reuse for memory and speed at huge task counts. The model Mills argues against. Needs `Release` on stop. Not justified here. |
| `alitto/pond` v2 | MIT | v2.7.3 (2026-10-17) | Active | Bounded queue (`WithQueueSize`), `TrySubmit`, per-pool context, panic to error, `StopAndWait`, metrics (`RunningWorkers`, `WaitingTasks`, dropped, failed), subpools. Zero dependencies. Closest to the needs here. 2.2k stars. Queue is unbounded by default. |
| `sourcegraph/conc` | MIT | v0.3.0 (2023-02-26) | Pre-1.0, no release in 3.5 years (repo pushed 2026-09-01) | Structured concurrency, re-panics at `Wait`. Abandonment and breaking-change risk. Not recommended. |
| `golang.org/x/sync` | BSD-3-Clause | module, pushed 2026-10-15 | Active, Go team | Already an indirect dependency. Recommended. |

- Pros of a library: ready queue, metrics, and panic policy. Cons: another dependency, a pool model the harness does not need, and a different panic policy than the harness wants (`conc` re-panics, which would crash a daemon). The stdlib plus `x/sync` solution is about 60 lines (section 12.4).
- **Fallback decision rule:** adopt `pond` v2 only if the team wants a visible bounded queue plus metrics and does not want to write 60 lines. Create one pool per class (a bulkhead), set `WithQueueSize`, and use `TrySubmit` for tool calls.

### 12.6 What experts recommend today

- Mills (2018): start goroutines when there is work; limit with a semaphore; avoid idle workers. [liveblog](https://github.com/sourcegraph/gophercon-2018-liveblog/issues/35)
- Uber: do not leak goroutines; every goroutine has a stop signal and a wait. [Uber](https://github.com/uber-go/guide/blob/master/style.md#dont-fire-and-forget-goroutines)
- The Go team keeps `errgroup.SetLimit` and `semaphore.Weighted` in `x/sync` as the standard tools, and the `sync` docs recommend `WaitGroup.Go`. [errgroup docs](https://pkg.go.dev/golang.org/x/sync/errgroup), [sync docs](https://pkg.go.dev/sync#WaitGroup.Go)
- Limit of this finding: no benchmark was run. Mills also states his patterns are for clarity and robustness, not speed.

## 13. Testing

- **Rule: no `time.Sleep` for synchronization in tests.** Wait on a channel, a `WaitGroup`, or `synctest`. The sleep either wastes time or flakes.
- **`goleak`** (`go.uber.org/goleak v1.3.0`, already in `go.mod`):
  - `defer goleak.VerifyNone(t)` per test, or `goleak.VerifyTestMain(m)` per package.
  - `VerifyNone` is incompatible with `t.Parallel`. Use `VerifyTestMain` for parallel tests.
  - Options: `IgnoreCurrent`, `IgnoreTopFunction`, `IgnoreAnyFunction`. [goleak docs](https://pkg.go.dev/go.uber.org/goleak)
  - Maintenance: latest release v1.3.0 (2023-10-24), repo pushed 2026-09-15, MIT. Low risk, slow release cadence.
- **`testing/synctest`** (GA 1.25):
  - `synctest.Test(t, func(t *testing.T){...})` runs the code in a bubble with a fake clock. Time jumps only when every goroutine in the bubble is durably blocked. `synctest.Wait()` waits for that state. `synctest.Sleep(d)` (1.27) is `time.Sleep(d)` plus `Wait()`. [synctest docs](https://pkg.go.dev/testing/synctest), [Go blog](https://go.dev/blog/testing-time)
  - Durably blocking: bubble channels, `select` on them, `sync.Cond.Wait`, `WaitGroup.Wait` (when `Add` ran in the bubble), `time.Sleep`.
  - **Not** durably blocking: mutexes, any I/O (including loopback sockets and pipes to child processes), syscalls. A task that runs a real process cannot advance fake time.
  - Fit: test timers, backoff, `Inbox` wake logic, shutdown order, and class admission under synctest. Test `os/exec` paths with a real short-lived process and a deadline used only as a failure guard (`t.Context()` or `context.WithTimeout`), never as synchronization.
  - `Test` fails on deadlock, so "wake never arrives" bugs show as test failures instead of hangs.
- Test list from the installed skill: run ends when the model ends though a task still runs; a completion inside a run is delivered once, in the next request; a completion after the run ends starts a new run; several completions give one run; shutdown or cancel loses no completion, leaks no goroutine, and waits for child tasks.
- Use `go test -race` in CI. The memory model defines the race; a green run without `-race` proves little. [Memory model](https://go.dev/ref/mem)

## 14. Anti-patterns

1. `go f()` with no owner, no stop signal, and no wait.
2. `time.Sleep` or a ticker loop to "wait for" a task, a file, or a condition.
3. The LLM calling a `check_status` tool in a loop to see if a task finished.
4. Closing a channel from the receiver, or from one of several senders.
5. Sending on a channel in a `select` with no `ctx.Done()` case.
6. A buffered channel of arbitrary size ("ought to be enough") to hide a missing consumer. [Uber](https://github.com/uber-go/guide/blob/master/style.md#channel-size-is-one-or-none)
7. A coalescing signal channel used to carry completions (events are lost).
8. `sync.Cond.Wait` where cancellation is needed, or without a `for` loop around it.
9. Holding a mutex across a channel send, a callback, or `Wait`.
10. `wg.Add` inside the goroutine, or `Go` after `Wait` has started on an empty group.
11. Relying on `errgroup` or `WaitGroup.Go` to catch panics. They do not.
12. Using `errgroup.Group` as a long-lived runtime or reusing it for unrelated work.
13. A pool of idle workers sized by guess, with no downstream resource that the size protects.
14. Dropping a completion because a queue is full.
15. `exec.Cmd` without `WaitDelay`, and killing only the parent so grandchildren hold the pipes.
16. Storing `ctx` in a struct field that is then used per request.
17. `os.Exit` or `log.Fatal` from a goroutine (skips defers and gives other goroutines no notice). [Cheney](https://dave.cheney.net/practical-go/presentations/qcon-china.html)
18. Mutable package-level state and function variables replaced in tests. Inject a clock and dependencies. [Uber](https://github.com/uber-go/guide/blob/master/style.md#avoid-mutable-globals)
19. Treating a channel as durable storage. A task that must survive a restart needs a database. Channels live in one process only. (Skill, "Pitfalls".)
20. Treating a green health check or a passing unit test as proof that wake-up works. Verify with an end-to-end run (`ask -p`) that a background completion wakes an idle agent.

## 15. Limitations and unresolved questions

Not covered, and why it matters:

- **Mills slide deck not read.** The Drive link is sign-in gated ([wiki listing](https://go.dev/wiki/Go-Community-Slides), [slides](https://drive.google.com/file/d/1nPdvhB0PutEJzdCq5ms6UI58dp50fcAN/view?usp=sharing), [video listing](https://www.classcentral.com/course/youtube-gophercon-2018-bryan-c-mills-rethinking-classical-concurrency-patterns-235549)). The liveblog is a high-level summary. It confirms futures, queues, resource limits as channels, the `sync.Cond` problems, and the worker-pool critique. It does **not** show a 1-buffered channel used as a state-holder/lock, or broadcast by close. The `Inbox` design rests on primary sources instead: `sync.Cond` docs ("Broadcast corresponds to closing a channel, Signal corresponds to sending") and the Pipelines post ("close is effectively a broadcast"). Attribute the state-holder-channel idea to the talk only after someone watches it.
- **No audit of the existing AskCore code.** This report does not check `internal/agent`, `internal/bus`, `internal/scheduler`, `internal/acp/host.go`, or the lifecycle event pipeline report for how a `Completion` should enter the existing `Steer` / `FollowUp` queues. That mapping is the next design step.
- **No benchmarks.** Choosing semaphore-per-class over a pool is a clarity and robustness choice, as Mills frames it.
- **Not fetched:** fsnotify behavior (kqueue file-descriptor use on macOS, event duplication), process-group kill on macOS and Linux, and `time.AfterFunc` text. They come from general knowledge. Verify before relying on them.

Unresolved questions:

1. Does `goleak.VerifyNone` interact correctly with `synctest` bubbles? The goleak docs say nothing. Test it in a spike before standardizing on both in one test.
2. Is a rejected admission (`ErrClassFull`) acceptable for the LLM-facing background-shell tool, or does the product want a visible queue? This is a product decision; the default here is reject.
3. Should the pending completion list survive a daemon restart? If yes, a channel or in-memory list is not enough and needs the store. The default here is in-process only.
4. Per-class limit values need measurement (process limits, LLM provider rate limits). No numbers were researched.

## Sources

- Go release notes: [1.23](https://go.dev/doc/go1.23), [1.25](https://go.dev/doc/go1.25), [1.26](https://go.dev/doc/go1.26), [1.27](https://go.dev/doc/go1.27)
- Go docs: [context](https://pkg.go.dev/context), [sync](https://pkg.go.dev/sync), [os/exec](https://pkg.go.dev/os/exec), [testing/synctest](https://pkg.go.dev/testing/synctest), [Testing time and synctest](https://go.dev/blog/testing-time), [Memory model](https://go.dev/ref/mem), [Pipelines and cancellation](https://go.dev/blog/pipelines), [Effective Go](https://go.dev/doc/effective_go), [Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- x/sync: [errgroup docs](https://pkg.go.dev/golang.org/x/sync/errgroup), [errgroup.go](https://github.com/golang/sync/blob/master/errgroup/errgroup.go), [semaphore docs](https://pkg.go.dev/golang.org/x/sync/semaphore), [semaphore.go](https://github.com/golang/sync/blob/master/semaphore/semaphore.go)
- Style: [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md), [Cheney, Practical Go](https://dave.cheney.net/practical-go/presentations/qcon-china.html)
- Talk: [Sourcegraph liveblog of Mills, GopherCon 2018](https://github.com/sourcegraph/gophercon-2018-liveblog/issues/35)
- Libraries: [goleak](https://pkg.go.dev/go.uber.org/goleak), [ants](https://github.com/panjf2000/ants), [pond](https://github.com/alitto/pond), [conc](https://github.com/sourcegraph/conc)
