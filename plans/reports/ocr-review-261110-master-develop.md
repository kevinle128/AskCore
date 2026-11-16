Review partially complete: 176 finding(s); 46 of 372 selected item(s) failed.

─── e2e/tui/real_provider.py:102-103 ───
[bug · medium] `os.write` is allowed to return after writing only part of the buffer. Because this
is the path used for bracketed pastes (including the real API key), a short write can silently
truncate the input and leave the PTY in an incomplete paste state. Use a write-all loop (handling
`InterruptedError` and zero-length writes) for both this method and the `write_process_input`
callback.

      def send(self, s):
-         os.write(self.fd, s.encode() if isinstance(s, str) else s)
+         data = s.encode() if isinstance(s, str) else s
+         while data:
+             try:
+                 n = os.write(self.fd, data)
+             except InterruptedError:
+                 continue
+             if n == 0:
+                 raise OSError("PTY write made no progress")
+             data = data[n:]


─── e2e/tui/real_provider.py:33-36 ───
[bug · medium] This suite cannot start on typical Linux/CI hosts: `/private/tmp` is a macOS-specific
path and is not normally present there, so the module-level `os.makedirs(OUT, exist_ok=True)` (and
the temporary-home creation below) fails before any scenario runs. Derive these locations from
`tempfile.gettempdir()` or require an explicit configurable base directory, while retaining any
separate requirement for a non-symlink leader home.

- ASK = os.environ.get("ASK_BIN", "/private/tmp/ask-rp-bin/ask")
- OUT = os.environ.get("RP_OUT", "/private/tmp/ask-rp-out")
+ TMP = os.environ.get("RP_TMP", tempfile.gettempdir())
+ ASK = os.environ.get("ASK_BIN", os.path.join(TMP, "ask-rp-bin", "ask"))
+ OUT = os.environ.get("RP_OUT", os.path.join(TMP, "ask-rp-out"))
  ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
  os.makedirs(OUT, exist_ok=True)


─── e2e/tui/real_provider_scenarios.py:33-33 ───
[bug · medium] The scenario module independently hard-codes `/private/tmp` as the `tempfile` parent.
On hosts without that directory (including normal Linux CI), importing this file fails at `mkdtemp`,
even if `real_provider.py` is made configurable. Use a platform-derived/configurable temporary
directory and ensure the selected directory is not a symlink if that is required by the leader.

- RUN_HOME = tempfile.mkdtemp(prefix="ask-provider-run-", dir="/private/tmp")
+ RUN_HOME = tempfile.mkdtemp(prefix="ask-provider-run-", dir=os.environ.get("RP_TMP", tempfile.gettempdir()))


─── e2e/tui/real_provider.py:176-181 ───
[maintainability · low] This helper is never called by any of the added scripts, and `tail` is
unused within it. Keeping it creates an untested, misleading artifact-writing API; remove it or
implement the intended tail behavior and use it consistently for scenario screenshots.



─── e2e/tui/run.py:1498-1500 ───
[security · high] `--artifacts` is user-controlled, but the final scan recursively unlinks every
file containing an inherited secret. Pointing this option at an existing directory (including a
project or home directory) can therefore delete unrelated files; this is especially risky because
`artifacts.mkdir(..., exist_ok=True)` does not establish ownership. Restrict the output to a newly
created run-owned directory (or require an empty directory and refuse anything outside it) before
allowing the scanner to delete files.

      leaked = scan_secrets(artifacts, {**secret_values(os.environ), 'ASK_FAKE_KEY': ASK_FAKE_KEY})
      if leaked:
          results = {'secret_error': 'credential values found in artifacts; the files were removed',


─── internal/acp/updates.go:346-350 ───
[bug · high] `unfollow` cancels the subscription and then waits for `sub.done`, but this send path
uses `host.ctx` rather than the subscription context. If the output writer blocks (for example, the
peer stops reading while the connection remains otherwise alive), `NotifyExtension` can remain in
the write indefinitely; cancellation does not release it, so the unfollow response and adapter
shutdown wait forever. Make sends interruptible/close the output on this condition, or avoid waiting
for completion without a cancellation-bounded path.

- out, err := s.a.waitOut(s.a.host.ctx)
+ out, err := s.a.waitOut(s.ctx)
  	if err != nil {
  		return err
  	}
- 	if err := out.n.NotifyExtension(s.a.host.ctx, protocol.ACPEvent, n); err != nil {
+ 	if err := out.n.NotifyExtension(s.ctx, protocol.ACPEvent, n); err != nil {


─── cmd/tui/capture.go:35-37 ───
[security · high] `ASK_CAPTURE` stores full request bodies and model responses in the cassette,
while creating the capture directory as world-readable (`0755`). Although the recorder removes auth
headers, the cassette still contains prompts/responses (and provider request bodies can contain
sensitive data), and the file created by the recorder is not opened with a private mode. On a shared
machine another local user can read captured conversations; create the directory/file with
owner-only permissions (and/or explicitly document/redact the sensitive payload).

- if err := os.MkdirAll(dir, 0o755); err != nil {
+ if err := os.MkdirAll(dir, 0o700); err != nil {
  		return "", fmt.Errorf("%s: %w", captureEnv, err)
  	}


─── cmd/tui/connect.go:221-221 ───
[other · medium] The initial handshake and session setup are performed before the signal-selecting
loop, and `callForSession` passes `context.Background()` into `CallCommit`. If the leader accepts
the socket but stops sending the `initialize` or `session/new` response, these calls wait
indefinitely; SIGINT/SIGTERM cannot reach the select below because the main goroutine is blocked
here. Use a context canceled by the signal path (and/or a setup deadline) for these calls so startup
honors the command's documented signal behavior.

- res, err := c.conn.CallCommit(context.Background(), method, params, commit)
+ res, err := c.conn.CallCommit(ctx, method, params, commit)


─── cmd/tui/auth_input.go:38-40 ───
[bug · medium] For non-`*os.File` inputs, this function calls `input.Read` directly without any
cancellation mechanism. `runWithAuthSignals` cancels the login context and waits only `abortGrace`,
but a blocked pipe/custom reader can keep the login goroutine alive indefinitely after the command
has returned. This is especially relevant because `runAuth` accepts an arbitrary `io.Reader` and the
function is used for both method selection and credential/callback input. Arrange for cancellation
to close an owned reader or use a cancellation-aware input adapter before invoking the login.

  		if file != nil {
  			poll := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
  			n, err := unix.Poll(poll, 100)


─── cmd/tui/capture.go:31-34 ───
[bug · medium] Capture is configured before `printStartModel` resolves the effective provider, but
`o.provider` is initialized to `faux` and is only updated by `printStartModel` later. Consequently
`ASK_CAPTURE` is ignored for common model-selection paths such as `ask -p --model anthropic/...`
(and saved/default catalog models), even though the subsequent run makes HTTP requests. Configure
capture after resolving the start model, or validate/derive the provider from the model pattern and
selected settings before this check.

- 	if o.provider != anthropic.ProviderID && o.provider != providers.ProviderAnthropic && o.provider != providers.ProviderOpenAI && o.provider != providers.ProviderXAI {
- 		report(stderr, "ask: "+captureEnv+" ignored: provider", o.provider, "makes no HTTP requests")
- 		return "", nil
- 	}
+ 	// Check the effective provider after model selection, rather than the
+ 	// pre-resolution command-line default, before deciding whether to capture.


─── internal/acp/provider_methods.go:97-101 ───
[bug · medium] These provider callbacks are not accounted for as host operations. `ServeStdio`
starts `Cleanup` concurrently with `a.Close()`, and `Host.Close` only waits for sessions plus the
operation counter used by authentication/session creation; a `LoginWait`/`SaveKey`/logout callback
can still be executing while cleanup tears down the credential broker. Track these calls in the host
admission/lifecycle (and release on return), or make shutdown explicitly wait for them before
running cleanup.

+ if err := a.host.beginOperation(); err != nil {
+ 	return true, nil, requestError(err)
+ }
+ defer a.host.endOperation()
  out, err := handler(ctx, route.ClientID, raw)
- 	if err != nil {
+ if err != nil {
- 		return true, nil, requestError(providerSetupError(err))
+ 	return true, nil, requestError(providerSetupError(err))
- 	}
+ }
- 	return true, out, nil
+ return true, out, nil


─── cmd/tui/headless_wait.go:248-249 ───
[bug · medium] `time.Timer.Stop` does not wait for an already-running `AfterFunc` callback. A
callback that has passed the timer lock can therefore execute after `AgentStart` has called
`disarm`, observe the newly idle/open state transition at an unfortunate point, and call
`cancel(errCeilingEnded)` for the current wait. The timer needs a generation/token check (or
callback serialization with the arm/disarm state) before cancelling, so stale callbacks cannot
terminate a later run.

  func (c *idleCeiling) fire() {
+ 	c.mu.Lock()
+ 	// Verify this callback still owns the current timer/generation before
+ 	// examining Agent state and cancelling the wait.
+ 	c.mu.Unlock()
  	if idle := c.ag.Idle(); !isClosedSignal(idle) {


─── cmd/tui/headless_wait.go:56-57 ───
[bug · medium] The millisecond value is parsed successfully as an `int64` but is converted to
`time.Duration` after multiplication without an overflow check. Values above roughly 9.22e12 ms wrap
negative; `BackgroundLimit.of` treats a negative default as disabled, so a user-controlled
environment setting can silently turn off the intended background limit. Reject values where `ms >
math.MaxInt64/int64(time.Millisecond)` (or otherwise clamp/report them) before converting.

- 	if ms, err := strconv.ParseInt(getenv(printBgLimitEnv), 10, 64); err == nil && ms > 0 {
+ 	if ms, err := strconv.ParseInt(getenv(printBgLimitEnv), 10, 64); err == nil && ms > 0 && ms <= int64(math.MaxInt64/int64(time.Millisecond)) {
  		l.Default = time.Duration(ms) * time.Millisecond


─── cmd/tui/interactive.go:372-375 ───
[bug · medium] A signal received during client shutdown is consumed here and converted into a
successful close, but `runTerminal` does not retain or reprocess it. Consequently a second signal
arriving after `waitProgram` has returned cannot force the documented immediate termination; the
close goroutine may continue until its normal bound and the original signal exit code is returned.
Signal escalation should be owned by one consumer (or propagate the received signal to the
caller/process termination path).

- 	case <-sigs:
- 		return nil
+ 	case sig := <-sigs:
+ 		return fmt.Errorf("shutdown interrupted by %s", sig)
  	case <-timer.C:
  		return fmt.Errorf("the connection did not close in time")


─── cmd/tui/leader.go:150-153 ───
[bug · low] Both JSON output and the write to stdout are ignored here. A broken pipe (for example,
`ask leader status --json | head -n 0`) therefore still returns exit code 0, unlike the other
command paths that report output failures. Check `json.Marshal` and `fmt.Fprintf` errors and return
1 so automation can detect the failed command.

  	if asJSON {
- 		data, _ := json.Marshal(st)
- 		_, _ = fmt.Fprintf(stdout, "%s\n", data)
+ 		data, err := json.Marshal(st)
+ 		if err != nil {
+ 			report(stderr, "Error:", err)
+ 			return 1
+ 		}
+ 		if _, err := fmt.Fprintf(stdout, "%s\n", data); err != nil {
+ 			report(stderr, "Error:", err)
+ 			return 1
+ 		}
  		return 0


─── internal/acp/trust_methods.go:105-109 ───
[bug · medium] Trust callbacks have the same shutdown-accounting gap as provider callbacks:
`TrustSetup.State`/`Set` can still be executing when `ServeStdio` runs `Cleanup` and closes the
host. `Host.beginOperation` is used for authentication and session construction specifically to keep
such work inside the lifecycle, but this path never increments it. Admit and release the operation
around the handler (or otherwise join these callbacks before cleanup).

+ if err := a.host.beginOperation(); err != nil {
+ 	return true, nil, requestError(err)
+ }
+ defer a.host.endOperation()
  out, err := handler(ctx, raw)
- 	if err != nil {
+ if err != nil {
- 		return true, nil, requestError(trustSetupError(err))
+ 	return true, nil, requestError(trustSetupError(err))
- 	}
+ }
- 	return true, out, nil
+ return true, out, nil


─── cmd/tui/leader.go:179-182 ───
[bug · low] The JSON marshal and stdout write errors are ignored here as well. A broken pipe or
serialization failure still returns 0, falsely reporting that `ask leader list --json` succeeded.
Propagate both errors and return a non-zero status, consistent with the other output-producing
command paths.

  	if asJSON {
- 		data, _ := json.Marshal(rows)
- 		_, _ = fmt.Fprintf(stdout, "%s\n", data)
+ 		data, err := json.Marshal(rows)
+ 		if err != nil {
+ 			report(stderr, "Error:", err)
+ 			return 1
+ 		}
+ 		if _, err := fmt.Fprintf(stdout, "%s\n", data); err != nil {
+ 			report(stderr, "Error:", err)
+ 			return 1
+ 		}
  		return 0


─── cmd/tui/headless_wait.go:86-87 ───
[bug · medium] This conversion has the same overflow problem for the ceiling, stall, and interactive
background-limit environment variables. A large positive `ms` wraps to a negative duration; callers
interpret non-positive values as disabled (or otherwise change timeout behavior), so an apparently
valid setting can remove the safety limit. Validate the multiplication range before returning the
duration.

+ 	if ms > math.MaxInt64/int64(time.Millisecond) {
+ 		return def
+ 	}
  	return time.Duration(ms) * time.Millisecond
  }


─── internal/acp/agent.go:195-200 ───
[bug · low] `Close` promises to close the connection output, but `ServeStdio` passes a `gatedOutput`
here and its `Close` is intentionally a no-op until `force` sets `open` (unless there is already a
failure). Consequently a direct caller that invokes `Adapter.Close` can get success while the
underlying `Out` remains open, leaving blocked writers/resources alive and contradicting
`CloseOutput`'s contract. Either make the adapter own/use an unconditional closer or change the
contract and call `force` as part of adapter shutdown.

  func (a *Adapter) CloseOutput() error {
  	if out := a.out.Load(); out != nil && out.closer != nil {
  		return out.closer.Close()
  	}
  	return nil
  }


─── cmd/tui/tui_client.go:126-128 ───
[bug · medium] `ServeStdio` receives a context that is unrelated to the TUI/client context, so
cancellation of `openInProcessClient` (or later client shutdown) cannot cancel an active ACP run
through the context. The implementation then waits up to `inProcessStopWait`, but a tool/handler
that does not honor pipe closure can leave the serving goroutine and runtime alive after the client
has returned. Pass a shutdown context owned by this client and cancel it from the close path (while
retaining the pipe close as a secondary unblock mechanism).

- 		exited <- acp.ServeStdio(context.Background(), acp.StdioConfig{
+ 		agentCtx, stopAgent := context.WithCancel(context.Background())
+ 		exited <- acp.ServeStdio(agentCtx, acp.StdioConfig{
  			Adapter: rt.Config, In: toAgentR, Out: fromAgentW, Cleanup: rt.Cleanup,
  		})


─── cmd/tui/settings_command.go:108-112 ───
[bug · medium] After a signal, this function returns without joining the settings goroutine.
`PrepareSession`/`LoadContext` and model-file reads perform ordinary filesystem operations that do
not all observe `ctx`, so the worker can exceed `abortGrace` and subsequently write to `stdout` or
`stderr` after the command has returned. That can corrupt a caller's terminal/output and races with
any cleanup of those writers. On the signal path, either wait for the worker to finish before
returning or ensure the worker cannot write after the command's ownership of the writers ends.

  		select {
  		case <-done:
  		case <-time.After(abortGrace):
+ 			// Do not return while the worker can still write to stdout/stderr.
+ 			<-done
  		}
  		return signalExitCodes[sig]


─── cmd/tui/version.go:29-30 ───
[maintainability · low] This duplicates the ACP implementation's authoritative
`internal/acp.wireVersion` (currently also 1). There is no negotiation or compile-time linkage
between the two constants, so changing the ACP wire version can leave `ask version --json`
advertising a stale `acpVersion`, which is specifically part of the public compatibility output.
Expose the authoritative value from the ACP/protocol package and use it here.

  // acpWireVersion is the ACP wire version that ask acp and ask leader speak.
- const acpWireVersion = 1
+ const acpWireVersion = acp.WireVersion


─── cmd/tui/tui_client.go:146-147 ───
[bug · medium] If the shutdown context is added as suggested above, `stopAgent` must also be invoked
here before waiting; otherwise the context still will not cancel the ACP adapter until after
`ServeStdio` returns. Keep the cancellation function in this closure (and on the error path) rather
than introducing an unused local.

  		OnClose: func() error {
+ 			stopAgent()
  			select {


─── internal/acp/stdio.go:278-279 ───
[bug · medium] The shutdown barrier uses a set keyed by JSON-RPC ID, but the SDK accepts duplicate
IDs (the conformance test explicitly sends two concurrent requests with `id: 1`). The second
registration overwrites the same set entry; when the first response is written, `written` deletes it
and `closeAndWait` can conclude even though the second handler/response is still pending, allowing
`out.force()` to close the output prematurely. Track a count per ID (or unique request tokens)
rather than a set.

- ids    map[string]struct{}
+ ids    map[string]int
  	closed bool


─── cmd/tui/connect.go:636-642 ───
[bug · medium] When stdin reaches EOF, `finish` treats the leader connection closing as a successful
completion (`case <-c.done:` falls through to `return 0`). This is reachable if a prompt is still in
flight and the leader disconnects: `runConnect` enters `finish`, observes `c.done`, and exits 0 even
though the command's documented contract says a lost leader exits 1 (and the prompt was not
successfully completed). Track whether `c.done` won and return the connection-loss code instead.

  	case <-c.done:
+ 		return 1
  	case sig := <-sigs:
  		c.out.printf("bye")
  		return signalExitCodes[sig]
  	}
  	c.out.printf("bye")
  	return 0


─── cmd/tui/input.go:33-39 ───
[bug · high] `os.Stat` can fail for reasons other than `fs.ErrNotExist` (for example, a permission
error or a path component that is not a directory). In those cases `info` is nil, but the next
condition dereferences `info.Size()`, causing the CLI to panic instead of returning the documented
file-read error. Handle any non-nil stat error before using `info`, preserving the not-found message
where appropriate.

  		info, err := os.Stat(abs)
  		if errors.Is(err, fs.ErrNotExist) {
  			return "", fmt.Errorf("File not found: %s", abs) //nolint:staticcheck // Pi's message text
  		}
- 		if err == nil && info.Size() == 0 {
+ 		if err != nil {
+ 			return "", fmt.Errorf("Could not read file %s: %w", abs, err) //nolint:staticcheck // Pi's message text
+ 		}
+ 		if info.Size() == 0 {
  			continue
  		}


─── internal/agent/loop_stream.go:91-93 ───
[bug · medium] `StreamFn` is an exported configuration hook and `New` only validates that the
function itself is non-nil; it does not validate the returned `*providers.Stream`. A provider or
custom configuration can therefore return nil, after which this path dereferences `opened.Binding()`
(and `consumeAttempt` would also dereference it) and panics in the run worker instead of producing a
recoverable model failure. Check `opened == nil` immediately after the call and convert it to a
terminal error/aborted outcome (or reject it in configuration).

  		opened = l.cfg.Stream(attemptCtx, l.cfg.Model, req, streamOpts)
+ 		if opened == nil {
+ 			return nil, fmt.Errorf("agent: stream function returned nil stream")
+ 		}
  		var consumeErr error
  		final, resultErr, consumeErr = l.consumeAttempt(attemptCtx, attemptID, opened, attemptCancel)


─── internal/agent/follow.go:159-162 ───
[bug · medium] `Follow` drops `pubMu` before taking the snapshot from `writer`. `Reset` can replace
`a.source` and close the old writer immediately after releasing `emitMu`, so this `Entries()` call
can overlap that writer's `Close()` (and, for file-backed writers, can read a closed resource or
race with teardown). Capture/copy the entries while the writer lifetime is protected, or introduce a
writer lifetime/read lock that Reset honors before closing the old log.

  	if writer != nil {
  		entries := writer.Entries()
  		f.Entries = entries[:min(commit, len(entries))]
  	}


─── internal/agent/tool_coordinator.go:322-322 ───
[bug · high] `AfterTool` intentionally returns `(nil, nil)` when no middleware changes the result
(the registry's terminal does this, and `AfterToolCallResult.Apply` explicitly supports a nil
receiver), but this dereferences `r` before calling `Apply`. Any normal tool call with no after-tool
override therefore panics the run goroutine instead of retaining `o.result`; call `r.Apply(...)`
directly or handle nil before dereferencing.

  	o.result, o.isError = r.Apply(o.result, o.isError)


─── cmd/tui/headless.go:264-268 ───
[bug · medium] This unconditionally parses `ASK_FAUX_TPS` while selecting a model, even when the
requested/provider-selected model is not faux. Thus an invalid faux-only environment value makes
every real-provider print run fail before it can open the selected provider. The faux pacing setting
should be validated/applied only on the faux path (or the demo model should be obtained without
parsing that environment).

- 	_, demo, err := fauxStream(defaultModel, getenv)
+ 	_, demo, err := fauxStream(defaultModel, func(string) string { return "" })
  	if err != nil {
  		return providers.Model{}, nil, nil, err
  	}
  	choice, err := app.ChooseStartModel(context.Background(), view, service, demo, eff, opts)


─── internal/agent/tasks.go:170-175 ───
[bug · high] The notice API accepts any `protocol.Message`, but this validation only detects a
typed-nil `*CustomMessage`. Typed-nil pointers to the other built-in message types (for example
`(*protocol.UserMessage)(nil)`) pass through, and `protocol.CloneMessage` preserves them; later
queue/model handling calls `Role()` on the nil pointer (the built-in methods have value receivers),
which can panic the run. Reject typed-nil message interfaces generically (for example with a
nil-able-kind reflection check) before cloning/queueing.

  	case protocol.CustomMessage:
  		if v.CustomType == "" {
  			return errBadNotice
+ 		}
- 		}
+ 	}
+ 	if isTypedNilMessage(m) {
+ 		return ErrNoInput
  	}
  	return nil


─── internal/acp/host.go:500-507 ───
[bug · medium] Once `Host.latch` closes `failed`, new mutation paths still pass this check because
it only tests `closed`/`quiesced` and the route generation. `setThinking`, `setModel`, `enqueue`,
and `remove` call `mutationCommit` after their initial guard, so a request racing with an output
failure can mutate/queue input (and potentially start another run) while shutdown is already in
progress. This contradicts the host's failure latch, which is intended to stop active work and
reject further work, and can let work be admitted after `ServeStdio` has begun closing the host.
Check the host failure (or use the host admission/usable check) under the same admission/commit
synchronization before returning the commit release.

  	switch {
  	case s.closed:
  		err = agent.ErrDisposed
+ 	case s.host.failure != nil:
+ 		err = s.host.failure
  	case s.host.quiesced.Load():
  		err = ErrHostClosed
  	case route != nil && route.DriverGen != s.gen:
  		err = errStaleDriver
  	}


─── internal/acp/stdio.go:106-109 ───
[bug · high] `CheckedWriter.Observe` stores a single callback, so this call replaces the
`a.frameWritten` observer installed by `Adapter.Bind` just above. As a result, successful
`session/follow` response frames are never passed to `frameWritten`; `unready` subscriptions are
never released, and `waitFollowers` can block prompts indefinitely while waiting for follow events
to be physically written. Install one combined observer (or make `Observe` additive) that invokes
both callbacks.

  	cw.Observe(func(frame []byte) {
  		a.frameWritten(frame)
  		pend.written(frame)
  	})


─── internal/agent/agent.go:446-449 ───
[bug · medium] `SetThinkingLevel` does not check `a.disposed`, even though the public contract says
all state-changing calls return `ErrDisposed` after `Dispose` starts. Consequently, a call racing
with or following disposal can return nil and mutate `cfg.Options.Reasoning` after the agent has
been closed, unlike `SetModel`, `Reset`, and the queue APIs. Add the disposed check while holding
`mu` before changing the level.

  func (a *Agent) SetThinkingLevel(level protocol.ThinkingLevel) error {
  	a.mu.Lock()
  	defer a.mu.Unlock()
+ 	if a.disposed {
+ 		return ErrDisposed
+ 	}
  	if a.active != nil {


─── internal/agent/agent.go:232-238 ───
[bug · high] `Continue` claims the run slot before validating that the current context is
continuable. If `Steer`/`FollowUp` arrives during the subsequent validation, it sees an active run
and queues the input, but `Continue` returns before executing a loop and `end` does not set `wake`
for that queue; the Agent then becomes idle with input stranded and no future run. Validate the
source messages before `begin`, or ensure this early-return path schedules queued work.

- 	r, err := a.begin(ctx)
- 	if err != nil {
- 		return err
- 	}
- 	defer a.end(r)
- 	msgs := r.source.Messages()
- 	if !hasConversation(msgs) {
+ 	// Validate the current source before claiming the run slot, then begin and
+ 	// re-check under the Agent lock if needed to preserve the concurrent contract.
+ 	// ...


─── internal/app/trust.go:196-199 ───
[bug · medium] `ProjectRoot` can return `hasLayer == false` when `req.Cwd` is the user's Ask home
(or its canonical equivalent), but this result is ignored here. In that case `Set` persists a trust
entry for the user's home/root even though `ResolveTrust` treats it as having no project layer; this
creates a misleading store entry and can affect nearest-entry decisions for later sessions. Reject
trust mutations when `hasLayer` is false (or otherwise use the same project-scope validation as
`State`).

- 	root, _, err := workspace.ProjectRoot(req.Cwd, t.home)
+ 	root, hasLayer, err := workspace.ProjectRoot(req.Cwd, t.home)
  	if err != nil {
  		return protocol.ACPTrustStateResult{}, err
+ 	}
+ 	if !hasLayer {
+ 		return protocol.ACPTrustStateResult{}, errors.New("trust: cwd has no project layer")
  	}


─── internal/agent/agent.go:449-452 ───
[bug · medium] This check only covers an active run, but `SetThinkingLevel` is documented to return
`ErrBusy` while a listener is being dispatched as well (`busyLocked` includes `dispatching`). A
listener can therefore call this method (or another goroutine can race with delivery) and mutate
configuration during event publication, unlike the other idle-only mutations. Use the same busy
predicate used by `idleLocked` after the disposed check.

- 	if a.active != nil {
+ 	if a.busyLocked() {
  		return ErrBusy
  	}
  	a.cfg.Options.Reasoning = providers.ClampThinkingLevel(a.cfg.Model, level)


─── internal/agent/agent.go:367-371 ───
[bug · medium] The disposed-state check occurs only after model validation and key/Ready resolution.
After `Dispose` has started, `SetModel` can therefore return a validation, key, or readiness error
(and perform resolver/Ready work) instead of the documented `ErrDisposed`; it also does unnecessary
external work on a closed Agent. Check `disposed` under `mu` before validation/resolution, while
retaining the commit-time check for races with Dispose.

  func (a *Agent) SetModel(ctx context.Context, model providers.Model) error {
+ 	a.mu.Lock()
+ 	if a.disposed {
+ 		a.mu.Unlock()
+ 		return ErrDisposed
+ 	}
+ 	a.mu.Unlock()
  	if err := providers.Validate(model); err != nil {
  		return err
  	}
  	a.mu.Lock()


─── internal/app/trust.go:219-222 ───
[bug · medium] The parent-target operation is documented to remove the cwd's folder entry so the
parent decision applies, but this cleanup is conditional on `value != nil`. For `decision == clear`,
`edit[parent] = nil` removes the parent entry while the root entry remains, so the cleared result
can still be governed by the stale root-specific decision. Remove `edit[root]` for every parent
target (including clear).

  		edit[parent] = value
- 		if value != nil {
- 			edit[root] = nil
+ 		edit[root] = nil
- 		}


─── internal/app/model_refresh.go:295-296 ───
[bug · high] `LoadModelView` explicitly documents that `service` may be nil, but
`buildModelSnapshot` unconditionally dereferences it here. The nil-service test/call path therefore
panics while building the catalog-only view; guard the account-key lookup (and leave `accounts`
empty) when `service == nil`.

  	for _, provider := range providerIDs(base) {
+ 		if service == nil {
+ 			break
+ 		}
  		key := service.CurrentAccountKey(ctx, provider, data.Salt)


─── internal/app/agent_native.go:134-134 ───
[bug · medium] This process-global `sync.Once` is consumed for the first resolved home, even when
the home/tasks directory is missing or unsafe (or when the first runtime uses a different home).
Since `sweepTaskOutput` only logs and returns on those cases, subsequent agents never retry the
sweep, so stale task output in a later/now-valid home is retained for the process lifetime. Scope
the once per home or only mark completion after a successful sweep/check.

- 	sweepTasksOnce.Do(func() { sweepTaskOutput(home, root, session) })
+ 	sweepTasksOnceFor(home).Do(func() { sweepTaskOutput(home, root, session) })


─── internal/app/module_leader.go:154-154 ───
[bug · medium] When `ctx` is canceled, `Serve` deliberately replaces it with
`context.WithoutCancel(ctx)` before shutdown. `Adapter.Close` calls `Host.Close`, and each session
close waits for `agent.Dispose`; the host comments indicate this can wait for started tool bodies.
Thus a stuck tool can make the leader process remain blocked indefinitely after its serving context
has ended, with no shutdown deadline. Use an independent bounded shutdown context (or make disposal
cancellation-bounded) rather than removing cancellation and retaining an unbounded wait.

- 	stopErr := rt.Stop(context.WithoutCancel(ctx))
+ 	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
+ 	defer cancel()
+ 	stopErr := rt.Stop(shutdownCtx)


─── internal/auth/broker.go:623-629 ───
[bug · medium] `method` is only validated indirectly by `Service.Logout`, and that check is skipped
when the provider has no saved credential (`c.Method == ""`). Consequently `Logout(ctx,
knownProvider, "arbitrary-method")` succeeds and reports a removal even though the requested method
is not one of this provider's `LoginOptions`; callers can therefore send an invalid method and still
trigger removal/change notifications. Validate the provider/method pair against `LoginOptions()` (or
reject non-empty methods that do not match) before reserving the removal.

- known := false
+ valid := method == ""
  	for _, o := range LoginOptions() {
- 		known = known || o.Provider == provider
+ 		if o.Provider == provider && (method == "" || o.Method == method) {
+ 			valid = true
- 	}
+ 		}
- 	if !known {
+ 	}
+ 	if !valid {
  		return LogoutInfo{}, ErrMethod
  	}


─── internal/app/module_leader.go:96-101 ───
[bug · medium] `NewACPRuntime` starts the model refresher before returning, but both
constructor-error paths here only close the broker/adapter and never invoke `acpRT.Cleanup`. If
adapter creation or server construction fails (for example due to invalid config), the refresher's
background goroutines and credential refresh lifecycle remain alive after the constructor returns an
error. Call the runtime cleanup on these paths (and combine it with the other cleanup errors as
appropriate).

  	if err != nil {
  		setup.Close()
+ 		_ = acpRT.Cleanup(context.Background())
  		return nil, err
  	}
- 
- 	// agent input: leader writes, adapter reads. agent output: adapter writes, leader reads.


─── internal/app/module_leader.go:121-125 ───
[bug · medium] The `NewServer` failure path has the same lifecycle leak: `NewACPRuntime` has already
launched stale-refresh work, but this branch closes only the adapter and broker. A failed leader
construction therefore leaves the model refresher running. Ensure `acpRT.Cleanup` is called before
returning here.

  	if err != nil {
  		_ = a.Close()
  		setup.Close()
+ 		_ = acpRT.Cleanup(context.Background())
  		return nil, err
  	}


─── internal/agent/seed.go:66-67 ───
[bug · medium] `customTypeOf` dereferences a `*protocol.CustomMessage` without checking whether the
interface contains a typed-nil pointer. A caller can supply a prefilled `NewContext` log containing
such a `MessageEntry` (including one produced by the notice path), and `openSource` will panic in
`seedLog` instead of returning an error. Treat typed-nil messages as absent/invalid before reading
`CustomType` (and keep the fresh-log setup panic-free).

  	case *protocol.CustomMessage:
+ 		if v == nil {
+ 			return "", false
+ 		}
  		return v.CustomType, true


─── internal/app/session_start.go:344-345 ───
[maintainability · low] The documented `Warnings` contract says each skill warning line is at most
256 bytes, but `capRows` budgets only `Path + ": " + Text` and does not account for the rendered
`"Warning: skills: "` prefix. Consequently a diagnostic at the limit produces a warning longer than
256 bytes (and empty-path diagnostics are not bounded by this calculation). Budget against the
complete rendered prefix, or truncate the final rendered line.

- 		room := max(maxSkillRowBytes-len(d.Text)-len(": "), 0)
+ 		room := max(maxSkillRowBytes-len("Warning: skills: ")-len(d.Text)-len(": "), 0)
  		out[i].Path = cutBytes(d.Path, room)


─── internal/agent/tasks.go:120-125 ───
[bug · medium] The `NoticeContext` path mutates `a.in.context` and returns without posting
`a.in.update()`. Consequently context-policy task notices are invisible to subscribers (and any
queued-state event stream) until some unrelated later event, unlike wake notices and removals. Emit
the queue update before unlocking, as in the other enqueue path.

  	if n.Policy == tasks.NoticeContext {
  		id := a.in.nextID()
  		a.in.context = append(a.in.context, input{id: id, msg: msg, origin: originTask})
+ 		a.postLocked(a.in.update())
  		a.mu.Unlock()
  		return id, nil
  	}


─── internal/bootstrap/read.go:21-25 ───
[security · high] `os.Stat`/`os.Open` follows symlinks, so a trusted project can place (or replace)
`SYSTEM.md`/`APPEND_SYSTEM.md` with a symlink to an arbitrary readable local file, and
`AGENTS.md`/`CLAUDE.md` are intentionally loaded even from untrusted projects. The resulting
contents are sent to the model via the session seed/system prompt, which can disclose credentials or
other private files. Reject symlinks (and ideally open with no-follow semantics) at this trust
boundary, or otherwise constrain the resolved path to the intended directory.

- 	info, err := os.Stat(path)
+ 	info, err := os.Lstat(path)
  	if err != nil {
  		return nil, 0, err
  	}
  	if !info.Mode().IsRegular() {
+ 		return nil, 0, errNotRegular
+ 	}


─── internal/bootstrap/truncate.go:79-80 ───
[bug · medium] After removing broad files, `kept[0]` is still the broadest remaining file, not the
most specific one. This path therefore truncates or returns only the broad file and silently drops
every more-specific file that should have been retained. Replace `kept[len(kept)-1]` while
preserving `kept[:len(kept)-1]` in the trial/build and returned slice.

- 	last := kept[0]
+ 	lastIndex := len(kept) - 1
+ 	last := kept[lastIndex]
  	trial := append(skips, Skip{Path: last.path, Reason: SkipTruncated, Bytes: last.bytes})


─── internal/bootstrap/truncate.go:85-88 ───
[bug · high] When the remaining file already fits within the budget, `sort.Search` returns
`len(last.text)`. The subsequent `last.text[n]` indexes one byte past the slice and panics, so any
non-truncated single-file context (the normal case after budget filtering) can crash `LoadContext`.
Guard the end-of-string case (or only inspect `last.text[n]` when `n < len(last.text)`) before
checking `utf8.RuneStart`.

  	n := sort.Search(len(last.text), func(i int) bool { return !fits(i + 1) })
- 	for n > 0 && !utf8.RuneStart(last.text[n]) {
+ 	for n > 0 && n < len(last.text) && !utf8.RuneStart(last.text[n]) {
  		n--
  	}


─── internal/auth/identity.go:48-49 ───
[performance · medium] `oidc.NewRemoteKeySet` retains the context supplied here for its JWKS HTTP
fetches. Because this is `context.Background()`, a first-time key lookup made by `Verify(ctx, ...)`
is not cancellable by that verification context; a cancelled login/request can remain blocked until
the copied client's 5-second timeout. Construct the key set with a context whose lifetime is bounded
by the verification operation, or use a key-set implementation that passes the per-call context
through to the fetch.

- ctx := oidc.ClientContext(context.Background(), &bounded)
- 	return &ChatGPTIdentityVerifier{issuer: issuer, keys: oidc.NewRemoteKeySet(ctx, jwksURL)}, nil
+ // Keep the JWKS client configuration on the verifier and create the
+ // RemoteKeySet with a caller-scoped context when verification begins.


─── internal/auth/service.go:124-126 ───
[other · low] When `WaitRefresh` observes `idle == nil`, it returns without establishing a barrier.
A concurrent `beginRefresh` can acquire the mutex immediately after this unlock and start work, so
an independent caller of the exported `WaitRefresh` can receive success while a refresh is active.
`DrainRefresh` avoids this by calling `StopRefresh` first, but `WaitRefresh` itself should either
document snapshot semantics or synchronize with the start of new work (for example, use a
generation/condition or make callers stop admission before waiting).

  if ch == nil {
+ 		// This is only safe if admission has already been stopped; otherwise
+ 		// a concurrent beginRefresh can start after this check.
  		return nil
  	}


─── internal/auth/list_request.go:158-162 ───
[security · medium] The allow-list only counts credential headers and cookies; it does not reject
other headers. A caller-supplied `HTTPClient` can install a transport that adds headers before
delegating to this transport, or the request can otherwise carry arbitrary stateful/sensitive
headers, and `base.RoundTrip(req)` forwards them to the fixed provider host. This contradicts the
documented request boundary (“exactly one header”) and can leak caller metadata to the provider.
Reject all headers except the route’s required credential header and the explicitly generated
`Accept`/Anthropic version headers (and ensure the custom transport cannot re-add them before the
network hop).

- credentials := 0
- 	for _, name := range []string{"Authorization", "X-Api-Key", "Proxy-Authorization"} {
- 		credentials += len(req.Header.Values(name))
+ for name, values := range req.Header {
+ 	if !allowedListHeader(name, values, t.route) {
+ 		return false
  	}
- 	return credentials == 1 && len(req.Header.Values(t.route.authHeader())) == 1
+ }
+ // Then require exactly one route credential header and no proxy credential.


─── internal/app/session_start.go:363-368 ───
[bug · low] `contextLayer` classifies every skipped context path below `askHome` as user-scoped, but
`LoadContext` also walks the session/project directories and those directories can legitimately be
nested below the Ask home (for example, with `Cwd = $ASK_HOME/work`). In that case a skipped project
`AGENTS.md`/`CLAUDE.md` is reported as a user diagnostic even though it came from the project walk.
Track the user-origin paths from `LoadContext` (or compare against the actual user context
directory) rather than using a broad prefix.

  func (s SessionStart) contextLayer(path string) string {
- 	if s.askHome != "" && strings.HasPrefix(path, s.askHome+string(filepath.Separator)) {
+ 	if s.askHome != "" && filepath.Dir(path) == s.askHome {
  		return string(settings.LayerUser)
  	}
  	return string(settings.LayerProject)
  }


─── internal/bus/follow.go:113-113 ───
[bug · medium] `data` is retained both in the ring and in every `Item` sent to followers without
taking ownership of its backing array. `Add` is a public API, so a producer can reuse or mutate the
slice after this call, and a consumer can mutate `Item.Data`; either action changes replayed data
and/or the bytes delivered to other followers (and can race with `Resume`/delivery). Copy the
payload before storing/publishing, or explicitly enforce immutable ownership at the API boundary.

- 	rec := record{seq: seq, session: session, data: data}
+ 	rec := record{seq: seq, session: session, data: append([]byte(nil), data...)}


─── internal/bus/follow.go:105-106 ───
[bug · low] These additions can wrap at `math.MaxUint64`: after a ring reaches that sequence,
`head+1` becomes zero and `Add` accepts a sequence-zero event as the successor, breaking the
intended monotonic sequence contract. The same unchecked wrap in `seq+uint64(len(r.items))` can make
a valid cursor at the maximum sequence fail the retention check. Since cursors and event envelopes
intentionally support the full uint64 range, reject/handle the boundary explicitly rather than
relying on wrapped arithmetic.

- 	if seq != r.head+1 {
+ 	if r.head == ^uint64(0) || seq != r.head+1 {
  		r.stats.Gaps++


─── internal/bus/follow.go:258-258 ───
[bug · medium] Even if the ring's stored copy is protected, this sends the same backing array to
every follower. A follower that modifies `Item.Data` can change the queued value observed by other
followers (and the ring's replay record). Give each follower its own copy when enqueueing, or
document and enforce read-only ownership.

- 	case f.ch <- Item{Seq: rec.seq, Data: rec.data}:
+ 	case f.ch <- Item{Seq: rec.seq, Data: append([]byte(nil), rec.data...)}:


─── internal/jobs/table.go:742-744 ───
[security · medium] `Wait` is the only job operation that does not take a generation, even though
the table is shared across conversation generations and `shell_bash.await` calls it using only
`job.ID()`. A stale/guessed ID can therefore wait on a job from another conversation; on timeout or
cancellation this also backgrounds or kills that job. Pass the caller generation (and use
`lookupLocked(gen, id)`), or otherwise perform the same ownership check as `Read`/`Kill`/`List`.

- func (t *Table) Wait(ctx context.Context, id string, budget time.Duration) WaitOutcome {
+ func (t *Table) Wait(ctx context.Context, gen uint64, id string, budget time.Duration) WaitOutcome {
  	t.mu.Lock()
- 	j, ok := t.jobs[id]
+ 	j, ok := t.lookupLocked(gen, id)


─── internal/jobs/table.go:925-928 ───
[bug · medium] `KillNow` only sends SIGKILL and explicitly does not wait; the sandbox reader
goroutines can still be ingesting output and the owning goroutine can still be running. Removing
`spillDir` immediately can unlink an active spill file while it is being written, and
`KillRemaining` returns before the documented process/job cleanup has completed, losing the final
output/error state. Keep the spill directory until the jobs have reached `Done` (or make the caller
perform that join) before removing it.

- for _, j := range t.stopAll(closeReason) {
+ live := t.stopAll(closeReason)
+ 	for _, j := range live {
  		j.proc.KillNow()
+ 	}
+ 	for _, j := range live {
+ 		<-j.done
  	}
  	t.report(t.removeSpillDir())


─── internal/jobs/table.go:570-572 ───
[performance · medium] This holds the table mutex across `sandbox.Start`, which performs pipe
creation and `exec.Cmd.Start` (fork/exec). If process creation or exec setup blocks, all job
operations—including `Close`, `Kill`, reads, and unrelated starts—are serialized behind it. Reserve
the active slot/ID under the lock, start outside it, then publish or roll back the reservation while
preserving the cap and close semantics.

- // Starting a process is a fork and an exec; the lock keeps the cap and the
- 	// ids exact without a reservation step.
+ // Reserve the slot under t.mu, then start outside the lock so other
+ 	// operations are not blocked by fork/exec.
+ t.mu.Unlock()
- 	p, err := sandbox.Start(spec)
+ p, err := sandbox.Start(spec)
+ t.mu.Lock()


─── internal/leader/proc_linux.go:56-57 ───
[security · high] `PidfdOpen` pins whatever process currently owns the recorded PID, but
`VerifyLeaderProcess` only compares its `/proc` start time after truncating start ticks to whole
Unix seconds. If the original leader exits and the PID is reused within that one-second bucket by
another `ask leader` process, the stale lock record can still pass this check and this pidfd will
target the replacement for SIGTERM/SIGKILL. Preserve and compare the raw Linux start-tick value (or
another kernel process identity) rather than reducing it to seconds.

  const clockTicks = 100 // USER_HZ on every Linux ABI that Go supports
- 	return st.Uid, boot + ticks/clockTicks, cmdline, nil
+ 	return st.Uid, boot*clockTicks + ticks, cmdline, nil


─── internal/leader/queue.go:27-28 ───
[performance · medium] This queue has no capacity or cancellation policy, while `router.agentQ` is
populated by client-driven requests/notifications and drained only by the agent writer. A stalled or
slow agent stream therefore lets an attached client accumulate arbitrary serialized payloads in
memory; repeated traffic can exhaust the leader before the socket backpressure reaches the client.
Bound the queue or reject/block producers with a defined cancellation/error path.

+ if len(q.items) >= maxQueueItems {
+ 		return false
+ 	}
- q.items = append(q.items, v)
+ 	q.items = append(q.items, v)
  	q.cond.Signal()


─── internal/leader/proc_linux.go:148-150 ───
[other · medium] When a caller's context deadline is less than 1ms away, this forces `pidfdExited`
to poll for at least 1ms even though `budget` was computed from the remaining deadline. The eviction
operation can consequently block past its context deadline (and `pidfdExited` only observes
cancellation after the poll returns). Do not clamp a positive deadline budget upward; return the
context error or use a zero-time poll when no time remains.

- return max(d, time.Millisecond)
+ if d <= 0 {
+ 		return 0
+ 	}
+ 	return d
  	}
  	if err := ctx.Err(); err != nil {


─── internal/auth/model_list.go:80-80 ───
[security · high] When an OAuth credential has no `Subject`, `identity` remains empty but this still
returns a non-empty cache key (`provider/method/`). Every such account therefore shares the same
cached model list, allowing one account's model availability to be shown for another and
contradicting the documented “empty Key means no cache” behavior. Return `""` when `identity == ""`
(or otherwise make the key account-specific).

+ 	if identity == "" {
+ 		return ""
+ 	}
  	return provider + "/" + method + "/" + identity


─── internal/leader/frame.go:73-75 ───
[security · high] `ServerConfig.MaxFrame` is caller-controlled and can be negative, but this
comparison converts it to `uint64` without validating the sign. For a negative `fr.max`, every
possible `uint32` length passes the check, and `io.ReadAll` can consume a peer-claimed payload of up
to 4 GiB, allowing a malformed client to drive excessive allocation/OOM. Reject
non-positive/negative limits when constructing the reader or in `ServerConfig.withDefaults` before
any frame is read.

- if uint64(n) > uint64(fr.max) {
+ if fr.max <= 0 || uint64(n) > uint64(fr.max) {
  		return protocol.LeaderFrame{}, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, n)
  	}


─── internal/leader/ids.go:313-319 ───
[bug · medium] Exponent overflow is treated as a raw-text key, so numerically equivalent JSON number
IDs can produce different active-request keys. For example, `1e2147483647` and `10e2147483646`
denote the same number but take this fallback with different strings; the second request bypasses
`ErrDuplicateID`, and a cancellation using the other spelling cannot find the first route. Normalize
arbitrary exponents without materializing the expanded number, or reject IDs whose numeric form
cannot be canonicalized consistently.

  if err != nil {
- 			// The exponent does not fit. The text is the key, with its sign.
- 			if neg {
- 				return "n:raw:-" + text, nil
- 			}
- 			return "n:raw:" + text, nil
+ 			return "", ErrInvalidID
  		}


─── internal/leader/client.go:471-473 ───
[security · medium] Unlike `Connect`, management operations do not validate
`Paths.Home`/`Paths.Socket` or inspect the socket entry before dialing. Since `Paths` is exported,
`Status` and `Stop` can follow a symlink or point at any same-user Unix socket; `Stop` even enters
this path when no lock is held but the socket answers, then sends leader control frames to that
endpoint. Apply the same home/socket/path ownership and type checks before `manage`/`Stop` dials.

  func (c *connector) manage(ctx context.Context, ctl protocol.LeaderControl) (protocol.LeaderFrame, error) {
+ 	if err := EnsureHome(c.cfg.Paths.Home); err != nil {
+ 		return protocol.LeaderFrame{}, err
+ 	}
+ 	if err := CheckSocketPath(c.cfg.Paths.Socket); err != nil {
+ 		return protocol.LeaderFrame{}, err
+ 	}
+ 	// Also reject non-socket, symlinked, and incorrectly owned endpoints before dialing.
  	d := net.Dialer{Timeout: dialTimeout}
  	conn, err := d.DialContext(ctx, "unix", c.cfg.Paths.Socket)


─── internal/leader/spawn.go:186-187 ───
[security · high] `checkExecutable` only rejects mode bit `0002` (world-writable). A binary with
mode `0770`/`0775` and group-write permission is still accepted when owned by the current user (or
root), so any member of that group can replace it before `StartLeader` executes the path. Reject
group-writable files too (and, for root-owned files, ensure the group/directory is trusted) before
treating the executable as safe.

- case info.Mode().Perm()&0002 != 0:
- 		return "", fmt.Errorf("%w: %s is writable by everyone", ErrAskUnusable, real)
+ case info.Mode().Perm()&(0002|0020) != 0:
+ 		return "", fmt.Errorf("%w: %s is writable by others", ErrAskUnusable, real)


─── internal/leader/rpc.go:26-30 ───
[security · high] The envelope validator reads only exact map keys, but downstream Go JSON
unmarshalling is case-insensitive. Thus a message containing case variants such as
`{"jsonrpc":"2.0","JSONRPC":"1.0",...}` or `{"method":"allowed","METHOD":"..."}` can be
accepted/routed using one value while an ACP/host decoder interprets the other. Reject folded
duplicate keys for the top-level object (as `params()` already does) before dispatch.

  fields, err := decodeObject(raw)
  	if err != nil {
  		return nil, err
+ 	}
+ 	if hasFoldDuplicate(fields) {
+ 		return nil, fmt.Errorf("%w: message has two keys that differ only in case", ErrInvalidMessage)
  	}
  	var version string


─── internal/leader/spawn.go:138-142 ───
[bug · medium] `n` is an exported API input and is used directly in `info.Size()-n` and the
allocation length. A negative value makes the requested length larger than the file (and can
overflow), while a very large value can produce an enormous allocation or a runtime panic.
Clamp/reject non-positive and excessively large values before calculating the offset.

+ if n <= 0 {
+ 		return ""
+ 	}
+ 	if n > 2048 {
+ 		n = 2048
+ 	}
- off := info.Size() - n
+ 	off := info.Size() - n
  	if off < 0 {
  		off = 0
  	}
  	buf := make([]byte, info.Size()-off)


─── internal/leader/server.go:199-202 ───
[bug · medium] `Serve` does not require `Start` and can accept/register clients while `s.r.run` and
the agent reader/writer have not been started. In that state `Ready` was forced true by `NewServer`,
client ACP messages are queued into an inactive router (or remain stuck forever if `Start` is never
called), and `Serve` can return only after unrelated socket activity. Enforce the startup ordering
(or make `Serve` start the router/link and reject connections until `linkReady`) so a public
`Server` cannot advertise a ready but nonfunctional leader.

  func (s *Server) Serve(ln net.Listener) error {
  	s.mu.Lock()
+ 	if !s.started {
+ 		s.mu.Unlock()
+ 		_ = ln.Close()
+ 		return errors.New("leader: server not started")
+ 	}
  	select {
  	case <-s.closed:


─── internal/jobs/monitor.go:219-221 ───
[bug · low] When a batch reaches `monitorBatchMax`, the cut marker is appended after the limit, so
the emitted event is larger than the documented 3000-character cap (and can be 3000 plus the
marker). This also happens before delivery/merging, so consumers that rely on the batch limit can
receive oversized monitor events. Reserve space for the marker when truncating, or enforce the cap
after adding it.

  if m.batchCut {
- 		text += "\n" + monitorCutMarker
+ 		marker := "\n" + monitorCutMarker
+ 		text = cutRunes(text, monitorBatchMax-utf8.RuneCountInString(marker)) + marker
  	}


─── internal/leader/server.go:127-128 ───
[bug · high] If the agent reader fails during startup, it stops the router and closes `r.done`; this
branch returns without calling `Close`. That leaves `s.closed` open, the agent stream unclosed, and
the agent writer goroutine blocked forever in `agentQ.pop()` (and any accepted connection goroutines
are not shut down). Clean up the server before returning from this startup-failure path, as is
already done for the other initialization failures.

  	case <-s.r.done:
- 		return fmt.Errorf("leader: the agent link closed during initialize: %w", s.r.linkErr)
+ 		err := fmt.Errorf("leader: the agent link closed during initialize: %w", s.r.linkErr)
+ 		_ = s.Close()
+ 		return err


─── internal/leader/spawn.go:192-193 ───
[security · high] `checkExecutable` validates the resolved symlink target but returns `abs`, which
may still be the original symlink path. Between this check and `exec.Command` in `StartLeader`, a
same-user process can replace that symlink and cause an unvalidated executable to run, defeating the
ownership/mode check. Return and use the resolved `real` path (or otherwise pin the executable)
after validation.

- 	return abs, nil
+ 	return real, nil
  }


─── internal/leader/server.go:102-103 ───
[bug · high] The `r.call` failure path has the same cleanup gap: if the agent reader posts
`agentGoneEvent` before the initialize callback runs, the router is already stopped and `Start`
returns here without closing `s.closed`, the agent stream, or the writer goroutine. Call `Close`
before returning on this path too.

  	if !s.r.call(func() {
  		s.r.internalRequest("initialize", map[string]any{


─── internal/pipeline/recovery.go:23-25 ───
[bug · medium] `delay *= 2` can overflow `time.Duration` before the `min` clamp is applied. For
example, an exported `RetryPolicy` with a large positive `BaseDelay`/`MaxDelay` and a retry count
that reaches this loop wraps `delay` negative; `settleAndRecover` serializes that as a negative
`DelayMs` and then passes it to `time.NewTimer`, so the configured bounded backoff becomes an
immediate retry (and the persisted/event delay is invalid). Clamp before multiplying, or detect
overflow and set the value to `MaxDelay`.

  	for i := 1; i < in.Retry && delay < in.Policy.MaxDelay; i++ {
+ 		if delay > in.Policy.MaxDelay/2 {
+ 			delay = in.Policy.MaxDelay
+ 			break
+ 		}
  		delay *= 2
  	}


─── internal/leader/client.go:278-278 ───
[bug · medium] `ConnectConfig.Env` is documented as the environment for a started leader, but it is
never propagated to `StartConfig` here. Consequently the default `StartLeader` (and any `StartFn`
that relies on the field) always receives a nil environment and starts with the current process
environment, ignoring caller-provided overrides such as `ASK_HOME` or provider credentials. Pass
`Env: c.cfg.Env` when constructing `StartConfig`.

  return c.cfg.StartFn(StartConfig{Executable: c.exe, Paths: c.cfg.Paths, Env: c.cfg.Env})


─── internal/pipeline/command_guard.go:174-176 ───
[security · medium] When `env` is the wrapper, its assignment operands are encountered after the
wrapper has been stripped, but this loop only recognizes assignments before wrappers. Thus `env
FOO=bar rm -rf /` falls through on `FOO=bar`, calls `commandReason` for that pseudo-command, and
never reaches `rm`; the destructive command then executes despite the guard. Skip assignment words
while consuming wrapper arguments (or continue the wrapper scan until the actual command) before
calling `commandReason`.

  		case isAssignment(w):
  			words = words[1:]
  		case wrappers[path.Base(w)]:


─── internal/leader/spawn.go:189-192 ───
[security · high] `checkExecutable` validates the resolved target, but returns and later executes
the original `abs` path. If the containing directory is writable (for example a writable directory
on `PATH` or beside `CallerExe`), another local process can replace that path with a symlink or
executable after `EvalSymlinks`/`Stat` and before `exec.Command`, causing `ConnectOrSpawn` to run an
unvalidated program as the user. Execute the resolved `real` path, and/or reject untrusted/writable
parent directories (with an appropriate race-resistant open/exec strategy).

  if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid != uint32(os.Geteuid()) && st.Uid != 0 {
  		return "", fmt.Errorf("%w: %s belongs to another user", ErrAskUnusable, real)
  	}
- 	return abs, nil
+ 	return real, nil


─── internal/providers/cassette/cassette.go:153-159 ───
[bug · high] `recorder.Stop()` finalizes the go-vcr recorder (and closes its underlying
cassette/output); after this first successful request, the next `r.vcr.RoundTrip` in the documented
multi-request recording path is operating on a stopped recorder and cannot append another
interaction. This breaks the `record(..., bodies...)` flow used for retries/tool calls and leaves
replay cassettes incomplete. Keep the recorder open for the lifetime of the `Recorder` and provide a
final close/flush operation (or use a recorder API that supports incremental saves) instead of
stopping it inside every `RoundTrip`.

- 	if r.mode == Record {
- 		// Save now, so a killed run keeps every exchange it finished.
- 		if err := r.vcr.Stop(); err != nil {
- 			_ = resp.Body.Close()
- 			return nil, r.fail(fmt.Errorf("cassette %s: save: %w", r.path, err))
- 		}
- 	}
+ 	// Keep the recorder open so subsequent requests can be recorded; stop it
+ 	// from an explicit finalization method instead of per request.


─── internal/leader/client.go:278-278 ───
[other · low] Correction to the preceding review note: this line already passes `Env: c.cfg.Env` to
`StartConfig`; the reported missing propagation is not an issue and should be disregarded.

  return c.cfg.StartFn(StartConfig{Executable: c.exe, Paths: c.cfg.Paths, Env: c.cfg.Env})


─── internal/providers/model_config.go:412-414 ───
[bug · medium] `ThinkingLevelMap` uses a nil `*string` to represent an explicitly unsupported level
(for example, the catalog contains `ThinkingMinimal: nil`). When a built-in row is patched with that
level and the patch supplies the same non-nil word, `onlyNarrows` reaches this branch and
dereferences `*word` where `word` is nil, panicking during normal `NewModelSet` construction. Check
`word != nil` before dereferencing (and treat an unsupported current level as unable to accept a new
word).

  		if word, ok := m.ThinkingLevelMap[l]; ok {
+ 			if word == nil {
+ 				return false
+ 			}
  			current = *word
  		}


─── internal/leader/spawn.go:186-187 ───
[security · high] The safety check rejects only world-writable executables (`0002`), despite the
contract saying the file must not be writable by others. A group-writable executable owned by the
current user (or root) can be replaced by another member of that group after validation and then
executed by `StartLeader`; this is especially relevant for binaries found on PATH. Reject group- and
world-writable files (`0022`), and apply the same policy to the containing path as appropriate.

- case info.Mode().Perm()&0002 != 0:
- 		return "", fmt.Errorf("%w: %s is writable by everyone", ErrAskUnusable, real)
+ case info.Mode().Perm()&0022 != 0:
+ 		return "", fmt.Errorf("%w: %s is writable by others", ErrAskUnusable, real)


─── internal/providers/cost.go:114-119 ───
[bug · medium] `tokenCost` performs the multiplication before dividing, and `applyServiceTier`
multiplies the already-computed value again. Since both usage and catalog prices are `int64` and
this function is reachable with provider-reported values, sufficiently large (including malformed)
values wrap before the division/scaling and can produce incorrect or negative billing totals. Use
checked/saturating arithmetic (or a wider intermediate) and define the behavior for out-of-range
inputs.

  func tokenCost(tokens, pricePerMillion int64) int64 {
  	if tokens <= 0 || pricePerMillion == 0 {
  		return 0
  	}
- 	return tokens * pricePerMillion / tokensPerPrice
+ 	// Compute with checked/wider arithmetic so provider/catalog values cannot wrap.
+ 	return checkedTokenCost(tokens, pricePerMillion)
  }


─── internal/providers/fantasykit/fold.go:207-212 ───
[bug · medium] `blocks` counts every entry in `texts` and `tools`, including blocks that only
emitted a start event. Thus a stream containing `TextStart` (or `ToolInputStart`) followed by a
normal `stop` has `blocks > 0` despite having no text or completed tool call, and bypasses the
`CodeEmptyResponse` failure. Count only non-empty/completed blocks (and account for tool calls that
actually reached `ToolCall`).

- blocks := len(texts) + len(tools)
+ blocks := 0
+ 			for _, b := range texts {
+ 				if b.text != "" {
+ 					blocks++
+ 				}
+ 			}
+ 			for _, b := range tools {
+ 				if b.completed {
+ 					blocks++
+ 				}
+ 			}
  			for _, tb := range thinks {
  				if tb.index >= 0 {
  					blocks++
  				}
  			}


─── internal/providers/failure.go:96-97 ───
[bug · medium] `CodeStreamClosed` is documented as the code for a stream that ended after the
terminal state, but this mapping exposes `ErrStreamIncomplete` instead of the separately declared
`ErrStreamClosed`. Consequently `errors.Is(failure, ErrStreamClosed)` is false and
`errors.Is(failure, ErrStreamIncomplete)` is true for a `CodeStreamClosed` failure, conflating two
distinct conditions and breaking callers that use the sentinel contract.

  case CodeStreamClosed:
- 		return ErrStreamIncomplete
+ 		return ErrStreamClosed


─── internal/providers/fantasykit/errors.go:202-203 ───
[security · low] This exported helper returns provider response bodies and messages without applying
`CleanDiagnostic`; `classifyProviderError` uses its result as the failure message, and
`Failure.Message` is exported. A provider error containing a bearer token, API key, or URL
credentials can therefore be exposed through callers that inspect/publish the message rather than
calling `Failure.Error()`. Redact the assembled message before returning it (while retaining the raw
cause separately).

  func HTTPMessage(err error) string {
  	var pe *fantasy.ProviderError
+ 	// Return only diagnostic-cleaned provider text.


─── internal/pipeline/registry.go:280-283 ───
[bug · high] `RequestMessages` is part of the documented `RequestUpdate`, but the terminal never
compares or copies `in.RequestMessages` into the returned update. Consequently a PrepareRequest
handler that rewrites the request-only message list gets a successful result with that rewrite
silently discarded, so the request sent to the model remains unchanged. Include this field in the
change detection and populate `upd.RequestMessages` when it differs from the original.

  		if !sameMessages(in.Context.Messages, orig.Context.Messages) || in.Context.Tools != orig.Context.Tools {
  			upd.Context, changed = &in.Context, true
+ 		}
+ 		if !sameMessages(in.RequestMessages, orig.RequestMessages) {
+ 			upd.RequestMessages, changed = in.RequestMessages, true
  		}
  		if !reflect.DeepEqual(in.Model, orig.Model) {


─── internal/pipeline/registry.go:276-277 ───
[bug · high] The request passed through this point is only shallow-copied (`orig := in`), so
`Context.Messages` shares its backing array with `in`. A handler can mutate a message/block in place
and then call `next`; `sameMessages` only compares slice length and first-element address, so the
terminal sees the same slice and returns no update. The mutation can also escape into the caller
despite the API's copy/freeze contract. Deep-copy the request (including messages) before
dispatch/comparison, or compare message contents and isolate the handler input.

  	orig := in
+ 	orig.Context.Messages = cloneMessages(in.Context.Messages)
+ 	in.Context.Messages = cloneMessages(in.Context.Messages)
  	terminal := func(_ context.Context, in Request) (*RequestUpdate, error) {


─── internal/providers/openai/completions.go:93-99 ───
[bug · high] This lookup is hard-coded to `ProviderTokenPlan`, but the generic Completions adapter
is also used for models whose provider is OpenAI, XAI, or a user-defined compatible provider. In
those cases a configured `OPENAI_API_KEY` (or the provider's configured credential) is never
considered, and the resulting Alibaba-specific error is misleading. Use the effective model provider
for lookup/error reporting (while preserving the explicit `APIKey`/bound-auth precedence).

  	if key == "" {
- 		key = providers.LookupKey(providers.ProviderTokenPlan, p.cfg.env)
+ 		key = providers.LookupKey(m.Provider, p.cfg.env)
  	}
  	if key == "" {
- 		a.Fail(protocol.StopError, "no API key for alibaba-token-plan (set StreamOptions.APIKey, ASK_ALIBABA_TOKEN_PLAN_API_KEY, or ALIBABA_TOKEN_PLAN_API_KEY)", errors.New("no API key for alibaba-token-plan"))
+ 		err := fmt.Errorf("no API key for %s", m.Provider)
+ 		a.Fail(protocol.StopError, err.Error(), err)
  		return
  	}


─── internal/providers/openai/isolate.go:60-64 ───
[security · high] `Isolate` copies model headers into `extra` and applies them after the request
headers, so a model header named `Authorization` replaces the SDK-generated bearer token. Model
headers are populated from user/provider configuration (`requestModel` expands and writes arbitrary
header names), so this can both break the selected credential and send a configured unintended
credential. Filter credential-bearing headers or ensure the SDK auth header always has precedence,
as `profileClient` already does.

  	for k, vs := range t.extra {
+ 		if k == "Authorization" || k == "X-Api-Key" || k == "Cookie" {
+ 			continue
+ 		}
  		next[k] = vs
  	}
  	clone.Header = next
  	return t.base.RoundTrip(clone)


─── internal/providers/model_config.go:364-370 ───
[bug · medium] The `ModelPatch` contract says nil or empty fields are no-ops, but a non-nil empty
`Input` slice reaches `validInput`, which returns false for `len == 0`, and records a configuration
problem instead of leaving the row unchanged. This makes configurations such as `input: []`
unexpectedly reject the whole input override; treat an empty slice like nil (or gate this branch on
`len(p.Input) > 0`) before validating it.

- 	if p.Input != nil {
+ 	if len(p.Input) > 0 {
  		if !validInput(p.Input) {
  			st.problem(path+".input", "input must list text or image")
  		} else {
  			out.Input, applied = slices.Clone(p.Input), true
  		}
  	}


─── internal/providers/fantasykit/fold.go:184-186 ───
[bug · high] When a `ToolInput` stream includes a final `ToolCall` with the same ID as the start
event, this branch calls `ToolEnd` with `nil`, causing the assembler to repair/use the accumulated
delta instead of the final `part.ToolCallInput`. `ToolCallInput` is the authoritative completed
argument payload (and the comment above `ToolEnd` says final arguments replace the whole block), so
any corrected or non-streamed final arguments are silently discarded whenever the ID does not
change. Pass a `protocol.ToolCall` final value for the same-ID case as well (preserving the tool
name and using `part.ToolCallInput`), or otherwise ensure the final payload is applied.

  				} else {
- 					a.ToolEnd(b.index, nil)
+ 					args := json.RawMessage(cmp.Or(part.ToolCallInput, b.input))
+ 					a.ToolEnd(b.index, &protocol.ToolCall{ID: id, Name: part.ToolCallName, Arguments: args})
  				}


─── internal/providers/openai/profiles.go:154-154 ───
[bug · medium] The guard constructs the expected path by concatenating `u.Path` with
`"/chat/completions"` without normalizing a trailing slash. Provider configuration accepts arbitrary
`BaseURL` values, including common forms such as `https://proxy.example/v1/`; with that value the
expected path becomes `/v1//chat/completions` while the SDK sends `/v1/chat/completions`, so every
API-key request is rejected locally. Normalize the parsed base path (or trim the suffix) before this
comparison, and apply the same fix to the Responses guard.

- 	if req.Method != http.MethodPost || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || req.URL.Path != u.Path+"/chat/completions" || req.URL.RawQuery != "" || req.URL.User != nil || req.URL.RawPath != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != u.Host) || req.Header.Get("Authorization") != "Bearer "+t.token || len(req.Header.Values("Authorization")) != 1 || req.Header.Get("X-Api-Key") != "" || req.Header.Get("Cookie") != "" {
+ 	basePath := strings.TrimRight(u.Path, "/")
+ 	if req.Method != http.MethodPost || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || req.URL.Path != basePath+"/chat/completions" || req.URL.RawQuery != "" || req.URL.User != nil || req.URL.RawPath != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != u.Host) || req.Header.Get("Authorization") != "Bearer "+t.token || len(req.Header.Values("Authorization")) != 1 || req.Header.Get("X-Api-Key") != "" || req.Header.Get("Cookie") != "" {


─── internal/pipeline/command_guard.go:224-228 ───
[security · high] `recursive` returns `false` as soon as it sees `--`, discarding any recursive flag
found earlier. A normal command such as `rm -rf -- /` (and `chmod -R -- /`) therefore passes the
guard even though `--` only terminates options and the preceding `-r`/`-R` remains active. Keep the
accumulated match when encountering `--` (or return a `found` value after scanning).

+ 	found := false
  	for _, a := range args {
  		if a == "--" {
- 			return false
+ 			break
  		}
  		if a == "--"+long {
+ 			found = true
+ 		}
+ 		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(a[1:], shorts) {
+ 			found = true
+ 		}
+ 	}
+ 	return found


─── internal/providers/openai/profiles.go:58-58 ───
[bug · medium] This has the same trailing-slash failure for Responses profiles: a configured
endpoint such as `https://api.example/v1/` makes `u.Path + "/responses"` equal `/v1//responses`,
while the SDK request is `/v1/responses`. Since user/provider configuration can supply `BaseURL`,
normalize `u.Path` before the exact comparison.

- 	if req.Method != http.MethodPost || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || req.URL.Path != u.Path+"/responses" || req.URL.RawQuery != "" || req.URL.User != nil || req.URL.RawPath != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != u.Host) || req.URL.Fragment != "" || req.Header.Get("Authorization") != "Bearer "+t.token || len(req.Header.Values("Authorization")) != 1 || req.Header.Get("OpenAI-Organization") != "" || req.Header.Get("OpenAI-Project") != "" || req.Header.Get("ChatGPT-Account-ID") != "" || req.Header.Get("X-Api-Key") != "" || req.Header.Get("Cookie") != "" {
+ 	basePath := strings.TrimRight(u.Path, "/")
+ 	if req.Method != http.MethodPost || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || req.URL.Path != basePath+"/responses" || req.URL.RawQuery != "" || req.URL.User != nil || req.URL.RawPath != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != u.Host) || req.URL.Fragment != "" || req.Header.Get("Authorization") != "Bearer "+t.token || len(req.Header.Values("Authorization")) != 1 || req.Header.Get("OpenAI-Organization") != "" || req.Header.Get("OpenAI-Project") != "" || req.Header.Get("ChatGPT-Account-ID") != "" || req.Header.Get("X-Api-Key") != "" || req.Header.Get("Cookie") != "" {


─── internal/pipeline/command_guard.go:106-111 ───
[security · high] Command substitutions inside double quotes are not recursively scanned for
destructive commands. For example, `echo "$(rm -rf /)"` executes `rm -rf /`, but the quoted content
is only appended to `syntax` (which is used for fork-bomb text matching); it never becomes a segment
passed to `segmentReason`. Parse/scan the contents of `$()` (and backticks) recursively, subject to
the existing depth bound, before allowing the command.

  			case c == quote:
  				// Double quotes can contain a command substitution.
  				if quote == '"' && (strings.Contains(quoted.String(), "$(") || strings.ContainsRune(quoted.String(), '`')) {
  					syntax.WriteString(quoted.String())
+ 					// Also inspect each command substitution recursively.
  				}
  				quote = 0


─── internal/providers/anthropic/prepare.go:28-28 ───
[bug · medium] `Prepare` receives no caller context and hard-codes `context.Background()` when
invoking `Encode`, even though `Encode` explicitly accepts a context and `DryRun` uses it for
`Stream.Result`. If the dry-run stream blocks while waiting for provider setup/stream termination,
cancellation or a deadline from the caller cannot stop preparation and this request-scoped work can
continue after the request is abandoned. Propagate the preparation context through the preparer
contract/callers (or otherwise make the context ownership explicit) instead of replacing it here.

- 	enc, err := p.Encode(context.Background(), tm, pr, req, providers.AuthBinding{})
+ 	enc, err := p.Encode(ctx, tm, pr, req, providers.AuthBinding{})


─── internal/providers/anthropic/document.go:454-459 ───
[bug · medium] This applies the prompt-cache breakpoint to the last block of whichever role happens
to be last. A transcript can legitimately end with an assistant response (especially an assistant
tool-use turn), but Anthropic cache breakpoints are not valid on arbitrary assistant content blocks;
the request can then be rejected or cache the wrong boundary. Select a supported cacheable boundary
(for example the last user/tool-result content block, or system/tools when there is no such block)
rather than indexing `messages[len(messages)-1]` unconditionally.

- 	last := messages[len(messages)-1]
- 	content, ok := last["content"].([]map[string]any)
- 	if !ok || len(content) == 0 {
- 		return
+ 	for i := len(messages) - 1; i >= 0; i-- {
+ 		if messages[i]["role"] != "user" {
+ 			continue
- 	}
+ 		}
+ 		content, ok := messages[i]["content"].([]map[string]any)
+ 		if ok && len(content) > 0 {
- 	content[len(content)-1]["cache_control"] = ctl
+ 			content[len(content)-1]["cache_control"] = ctl
+ 		}
+ 		return
+ 	}


─── internal/providers/anthropic/prepare.go:61-61 ───
[bug · medium] The clamp treats four Unicode code points as one token (`RuneCount/4`), but tokenizer
costs are based on encoded text and non-ASCII scripts can require roughly one or more tokens per
rune. A sufficiently large CJK (or other non-ASCII) prompt can therefore make the computed
`max_tokens` plus the actual input exceed `ContextWindow`, producing a provider-side context-length
error despite the clamp. Use the provider's tokenizer if available, or a conservative byte/token
estimate that does not undercount non-ASCII payloads (and account for the actual request encoding).

- 	pr.MaxTokens = clampMaxTokens(tm, opts, utf8.RuneCount(doc.body)/4)
+ 	pr.MaxTokens = clampMaxTokens(tm, opts, estimateTokens(doc.body))


─── internal/providers/model_set.go:310-311 ───
[maintainability · medium] `cloneModel` claims returned rows share no storage and says
`SamplingParams` values are copied one level deep, but `maps.Clone` only copies the map; any slice,
map, pointer, or other mutable value stored under an `any` remains aliased with the ModelSet. A
caller of `All`/`Find` can mutate a nested value and change the supposedly immutable snapshot (and
later requests). Deep-copy the supported value shapes or revise the ownership contract and prevent
mutable values from entering this field.

- 	m.SamplingParams = maps.Clone(m.SamplingParams)
+ 	m.SamplingParams = cloneSamplingParams(m.SamplingParams)
  	m.Cost.Tiers = slices.Clone(m.Cost.Tiers)


─── internal/pipeline/registry.go:362-364 ───
[bug · high] This point has the same aliasing problem as PrepareRequest: `orig := in` is shallow, so
a handler that edits `in.Result.Content`, `Details`, or other nested fields in place mutates `orig`
as well. The terminal then sees `reflect.DeepEqual(in.Result, orig.Result)` and returns nil, so the
mutation is not converted into an override (and may unexpectedly mutate the loop's original result).
Clone the baseline and dispatch input before running handlers, or otherwise enforce the documented
per-dispatch copy/freeze contract.

  	// The terminal keeps the result that the innermost handler passed on.
- 	orig := in
+ 	orig := cloneToolResultInfo(in)
+ 	in = cloneToolResultInfo(in)
  	terminal := func(_ context.Context, in ToolResultInfo) (*AfterToolCallResult, error) {


─── internal/providers/cassette/cassette.go:232-234 ───
[bug · medium] `dec.More()` only has defined meaning while iterating an array/object; at the top
level it does not reliably detect a second JSON value or trailing non-JSON data. Consequently bodies
such as `{} {}` (or `{}garbage`) are canonicalized to the first value and can compare equal to `{}`
during replay, allowing a request with a different payload to consume the recorded interaction.
After decoding the first value, decode once more and require `io.EOF` (or otherwise reject trailing
content).

- 	if err := dec.Decode(&v); err != nil || dec.More() {
+ 	if err := dec.Decode(&v); err != nil {
+ 		return body
+ 	}
+ 	var extra any
+ 	if err := dec.Decode(&extra); err != io.EOF {
  		return body
  	}


─── internal/providers/modelsdev_fetch.go:124-127 ───
[bug · medium] `json.Unmarshal` accepts both `null` and `{}` here without an error, leaving
`p.Models` nil, so a malformed provider entry is converted into a valid empty provider.
`refreshOverlay` then stores the resulting payload as a successful refresh, replacing the previous
non-empty overlay and causing all dynamically discovered models for that provider to disappear until
the next refresh. Validate that the provider value is an object with a `models` object (and
similarly reject this shape in `MapModelsDev`) before accepting the result.

- 		if err := dec.Decode(&p); err != nil {
+ 		var rawProvider map[string]json.RawMessage
+ 		if err := dec.Decode(&rawProvider); err != nil {
  			return nil, fmt.Errorf("models.dev provider %s: %w", key, err)
+ 		}
+ 		rawModels, ok := rawProvider["models"]
+ 		if !ok || string(rawModels) == "null" {
+ 			return nil, fmt.Errorf("models.dev provider %s: missing models object", key)
+ 		}
+ 		var p struct {
+ 			Models map[string]map[string]json.RawMessage `json:"models"`
+ 		}
+ 		if err := json.Unmarshal(rawModels, &p.Models); err != nil {
+ 			return nil, fmt.Errorf("models.dev provider %s: models: %w", key, err)
  		}
  		models := make(map[string]map[string]json.RawMessage, len(p.Models))


─── internal/providers/anthropic/profile.go:62-66 ───
[other · medium] This wrapper consumes `req.Body` and replaces it without closing the original body.
`RoundTripper` implementations are responsible for closing request bodies, so every OAuth request
leaves the SDK-created body (and any underlying resources) unclosed; under repeated/streaming
requests this can retain resources and eventually exhaust them. Close the original body after
`io.ReadAll` succeeds (and on the read-error path as well) before assigning the replacement.

- 	body, err := io.ReadAll(req.Body)
+ 	originalBody := req.Body
+ 	body, err := io.ReadAll(originalBody)
+ 	closeErr := originalBody.Close()
  	if err != nil {
  		return nil, err
+ 	}
+ 	if closeErr != nil {
+ 		return nil, closeErr
  	}
  	req.Body = io.NopCloser(bytes.NewReader(body))


─── internal/providers/openai/completions_body.go:100-102 ───
[bug · medium] `SupportsReasoningEffort` is never consulted, so any compatible model with a
non-empty `ThinkingLevelMap` gets a `reasoning_effort` field even when its compatibility record
explicitly says that field is unsupported. Such endpoints commonly reject unknown request fields (or
silently change behavior); gate this assignment on `compat.SupportsReasoningEffort` (and retain the
qwen-specific handling).

- 	if pr.Effort != "" {
+ 	if compat.SupportsReasoningEffort && pr.Effort != "" {
  		extra["reasoning_effort"] = pr.Effort
  	}


─── internal/providers/modelsdev.go:112-116 ───
[bug · medium] The same shape-validation gap exists for callers of `MapModelsDev` directly:
unmarshalling a provider value of `null` or `{}` succeeds and produces a nil `Models` map, so
malformed cached/generated metadata is treated as a valid empty catalog rather than an error. Since
this function is also used to validate payloads before caching, that allows an invalid overlay to be
stored successfully. Require the provider value and its `models` member to be non-null objects
before iterating.

- 		var p modelsDevProvider
- 		if err := json.Unmarshal(raw, &p); err != nil {
+ 		var p map[string]json.RawMessage
+ 		if err := json.Unmarshal(raw, &p); err != nil || p == nil {
+ 			if err == nil {
+ 				err = errors.New("provider is not an object")
+ 			}
  			return nil, fmt.Errorf("models.dev provider %s: %w", provider, err)
  		}
- 		for _, rawModel := range p.Models {
+ 		rawModels, ok := p["models"]
+ 		if !ok || string(rawModels) == "null" {
+ 			return nil, fmt.Errorf("models.dev provider %s: missing models object", provider)
+ 		}
+ 		var models map[string]json.RawMessage
+ 		if err := json.Unmarshal(rawModels, &models); err != nil || models == nil {
+ 			if err == nil {
+ 				err = errors.New("models is not an object")
+ 			}
+ 			return nil, fmt.Errorf("models.dev provider %s: %w", provider, err)
+ 		}
+ 		for _, rawModel := range models {


─── internal/providers/modelsdev.go:256-262 ───
[bug · medium] Because the price fields are plain `float64`, JSON omission is indistinguishable from
an explicit zero here. A payload such as `"cost":{"input":0.5,"output":1.5}` therefore becomes a
known schedule with `CacheRead` and `CacheWrite` set to zero, and `CostUnknown` is false. Those
cache prices are optional in the models.dev data (the checked-in snapshot has entries omitting
`cache_write`), so the mapper can report a partial price as authoritative and understate
cached-token costs. Track field presence (or validate the required price fields) and return
`CostUnknown` for an incomplete cost object.

+ 	// Preserve field presence in modelsDevPrice (for example with *float64
+ 	// fields), and reject any omitted price before converting it.
  	for _, f := range []struct {
- 		usd float64
+ 		usd *float64
  		dst *int64
  	}{{p.Input, &out.Input}, {p.Output, &out.Output}, {p.CacheRead, &out.CacheRead}, {p.CacheWrite, &out.CacheWrite}} {
- 		if math.IsNaN(f.usd) || f.usd < 0 || f.usd > maxPriceUSD {
+ 		if f.usd == nil || math.IsNaN(*f.usd) || *f.usd < 0 || *f.usd > maxPriceUSD {
  			return Price{}, false
+ 		}
+ 		*f.dst = int64(math.Round(*f.usd * tokensPerPrice))
- 		}
+ 	}


─── internal/providers/openai/completions.go:215-219 ───
[bug · medium] This wrapper replaces the request body with a clone and never closes the original
`req.Body`. `RoundTripper` implementations own closing the request body; the underlying transport
only closes `clone.Body`, so the original body (which may hold a file/socket-backed reader) is
leaked on every compatible endpoint request. Close `req.Body` after obtaining the replacement
(including on `GetBody` error paths as appropriate).

  	body, err := req.GetBody()
+ 	if req.Body != nil {
+ 		defer func() { _ = req.Body.Close() }()
+ 	}
  	if err != nil {
  		return nil, err
  	}
  	defer func() { _ = body.Close() }()


─── internal/providers/partialjson/partialjson.go:260-262 ───
[bug · medium] The object parser unconditionally advances one byte here instead of verifying that it
is a `:`. For input such as `{"path" 1}`, `p.i` points at the space, this skips the space and parses
`1` as the value, so malformed tool arguments are returned as a valid object. Since
`Assembler.toolArguments` uses `partialjson.Parse` as its fallback for incomplete/non-strict raw
arguments, this can silently send an argument object that was never present in the provider data.
Only advance after confirming a colon (otherwise stop/return the partial object).

- 		// The byte at the colon position is skipped without a check.
- 		p.i = min(p.i+1, len(p.s))
+ 		p.skipBlank()
+ 		if p.i >= len(p.s) || p.s[p.i] != ':' {
+ 			return obj, nil
+ 		}
+ 		p.i++
  		val, err := p.value(depth + 1)


─── internal/providers/sse/reader.go:189-199 ───
[bug · medium] `eventBytes` is updated only for `event` and `data` fields, while
`checkLineAgainstEvent` is skipped for comments but the documented event limit is for the total
field lines. An input stream can therefore send an unbounded number of valid-size unknown fields
without ever reaching `ErrEventTooLong`; the reader keeps accumulating/processing the event until a
blank line arrives. Count every non-comment field line toward the event limit (including
unrecognized fields), while retaining only the fields needed for dispatch.

+ 	if line[0] != ':' {
+ 		r.eventBytes += len(line)
+ 	}
  	switch string(field) {
  	case "event":
  		r.name = string(value)
  	case "data":
  		r.data = append(r.data, value...)
  		r.data = append(r.data, '\n')
  		r.dataLines++
  	default:
  		return
  	}
- 	r.eventBytes += len(line)


─── internal/providers/replay.go:26-32 ───
[bug · low] This decoder returns `true` for any syntactically valid JSON, including `null`, `[]`,
`{}`, and `{"v":0}`; all of these leave `TextReplay` with an empty ID (or an invalid version) but
are reported as a usable signature. A replay caller cannot distinguish a valid encoded signature
from corrupted/persisted data and may attempt to reuse empty/incorrect continuity state. Require the
expected object shape, version, and non-empty ID before returning success (and reject
unknown/invalid shapes as appropriate).

  	if err := json.Unmarshal([]byte(sig), &r); err != nil {
  		if sig == "" {
  			return TextReplay{}, false
  		}
  		return TextReplay{ID: sig}, true
+ 	}
+ 	if r.V != 1 || r.ID == "" {
+ 		return TextReplay{}, false
  	}
  	return r, true


─── internal/providers/convert.go:15-19 ───
[bug · medium] The type switch treats typed-nil built-in pointers as valid messages and appends them
unchanged. A caller can pass `(*protocol.UserMessage)(nil)` (the function already explicitly handles
a nil CustomMessage), and downstream transcript encoders iterate the returned slice expecting a
concrete message and can dereference the nil pointer. Either drop nil pointers consistently or
handle them before appending.

- 		case protocol.SystemMessage, *protocol.SystemMessage,
- 			protocol.UserMessage, *protocol.UserMessage,
- 			protocol.AssistantMessage, *protocol.AssistantMessage,
- 			protocol.ToolResultMessage, *protocol.ToolResultMessage:
+ 		case protocol.SystemMessage:
+ 			out = append(out, m)
+ 		case *protocol.SystemMessage:
+ 			if v != nil {
+ 				out = append(out, m)
+ 			}
+ 		case protocol.UserMessage:
+ 			out = append(out, m)
+ 		case *protocol.UserMessage:
+ 			if v != nil {
- 			out = append(out, m)
+ 				out = append(out, m)
+ 			}
+ 		// Apply the same nil check to AssistantMessage and ToolResultMessage.


─── internal/sandbox/process.go:323-325 ───
[test · medium] `Clock` is documented as the runner's time source, but this cleanup path bypasses it
entirely: `Now` and `AfterFunc` from the injected clock are not used for the group polling deadline
or interval. With a fake clock, a group that remains visible still causes real `time.Sleep` calls
(up to a second), making cleanup nondeterministic and potentially blocking tests/integrations
despite the clock abstraction. Route the polling deadline/wait through the injected clock (or
otherwise make this bounded sweep clock-aware).

- deadline := time.Now().Add(groupPollLimit)
- 	for time.Now().Before(deadline) {
- 		time.Sleep(groupPollEvery)
+ deadline := p.clock.Now().Add(groupPollLimit)
+ 	for p.clock.Now().Before(deadline) {
+ 		// Use a clock-controlled wait here rather than time.Sleep.


─── internal/providers/transform.go:32-32 ───
[bug · medium] The remapping table does not ensure that normalized IDs remain unique. For example,
`NormalizeAnthropicToolCallID` truncates IDs to 64 runes, so two distinct calls whose first 64 runes
match are emitted with the same provider ID (and the OpenAI normalizers can similarly collide after
truncation/hash). `pairMessages` then sees duplicate pending IDs and assigns results by occurrence
rather than their original call, while the provider may reject the duplicate call IDs. Track/resolve
collisions per transcript (or include a deterministic unique suffix) before emitting the assistant
calls.

  idMap := map[string]string{}


─── internal/sessions/entry.go:200-205 ───
[bug · medium] Because `entry` has value receivers, `*MessageEntry`, `*RequestDelta`, etc. also
satisfy `Entry` (including typed-nil pointers). `checkEncodable` accepts a typed-nil pointer because
`json.Marshal` returns `null`, while this type switch does not clone pointer forms and returns the
original interface. `Append` therefore violates the documented deep-copy/valid-entry contract and
`Entries` can return caller-owned pointers (or typed nils). Either use pointer-aware
cloning/validation or make the entry interface implementation pointer-exclusive/value-exclusive
consistently.

- func (MessageEntry) entry()   {}
- func (CycleOpened) entry()    {}
- func (CycleClosed) entry()    {}
- func (TurnOpened) entry()     {}
- func (TurnClosed) entry()     {}
- func (AttemptSettled) entry() {}
+ func (*MessageEntry) entry()   {}
+ func (*CycleOpened) entry()    {}
+ func (*CycleClosed) entry()    {}
+ func (*TurnOpened) entry()     {}
+ func (*TurnClosed) entry()     {}
+ func (*AttemptSettled) entry() {}


─── internal/settings/auth.go:159-161 ───
[security · high] `decodeCredential` treats every method other than `api-key` as OAuth and does not
require either OAuth token to be non-empty. Since `Read`/`ReadProvider` are the validation boundary
used by the auth service, a malformed or tampered record such as
`{\"method\":\"oauth\",\"oauth\":{}}` is accepted and can reach provider requests or refresh calls
with empty credentials. Restrict methods to the supported values (or otherwise validate them at this
boundary) and require the token fields needed by the OAuth flow.

- } else if c.OAuth == nil || c.APIKey != "" {
+ 	} else if !validCredentialMethod(c.Method) || c.OAuth == nil || c.APIKey != "" || c.OAuth.AccessToken == "" || c.OAuth.RefreshToken == "" {
  		return c, fmt.Errorf("%w: invalid OAuth record", ErrUnsafeStore)
  	}
+ 	if c.RefreshState != nil


─── internal/settings/auth.go:549-551 ───
[bug · medium] This method accepts a replacement after only generic credential validation. It does
not require the replacement to remain OAuth or to have the same method as the fenced record, so a
caller can commit an API-key credential into an OAuth refresh slot, advance the generation, and
clear the fence. That violates the refresh-fence contract and can make subsequent OAuth resolution
fail or use the wrong credential type; validate `replacement.Method == c.Method` and
`replacement.OAuth != nil` (and reject `APIKey`) before committing.

+ 		if replacement.Method != c.Method || replacement.OAuth == nil || replacement.APIKey != "" {
+ 			return fmt.Errorf("%w: invalid refresh replacement", ErrUnsafeStore)
+ 		}
  		replacement.Generation = generation + 1
  		replacement.RefreshState = nil
  		d.providers[p], e = marshalCredential(d.providers[p], replacement)


─── internal/settings/lock.go:33-37 ───
[bug · medium] `lockSidecar` unconditionally references `errLockBusy`, `tryFileLock`, and
`unlockFile`, but the only definitions are in `lock_unix.go` (tagged out on Windows and other
non-Unix targets). The package therefore cannot compile for those targets, and there is no fallback
implementation. Add a platform-specific implementation (or explicitly constrain supported builds
with matching package/build configuration).

  	for {
  		err = tryFileLock(f)
  		if err == nil {
  			return func() { _ = unlockFile(f); _ = f.Close() }, nil
  		}


─── internal/settings/paths.go:44-53 ───
[security · medium] The home check validates only `home` and its immediate parent; it does not
reject symlinked ancestor components. `os.CreateTemp`, `os.Open`, and `os.Rename` then resolve those
ancestors normally, so a home such as `/trusted/link/.ask` can redirect credential, host-ID, and
model-cache files outside the intended private tree while passing these checks. Validate each
ancestor (without following symlinks), or otherwise establish and enforce a trusted root before
performing sensitive file operations.

  func checkDirectory(path string) error {
+ 	for {
- 	info, err := os.Lstat(path)
+ 		info, err := os.Lstat(path)
- 	if err != nil {
+ 		if err != nil {
- 		return err
+ 			return err
- 	}
+ 		}
- 	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
+ 		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
- 		return fmt.Errorf("%w: parent directory", ErrUnsafeStore)
+ 			return fmt.Errorf("%w: parent directory", ErrUnsafeStore)
- 	}
+ 		}
+ 		parent := filepath.Dir(path)
+ 		if parent == path {
- 	return nil
+ 			return nil
+ 		}
+ 		path = parent
+ 	}
  }


─── internal/providers/normalize.go:74-79 ───
[bug · medium] The source/target comparison here only checks `source.Model`, but `TransformMessages`
considers a model the same only when API, provider, and ID all match. If a transcript from a native
provider (for example `openai`) is replayed to another native provider with the same model ID, this
branch preserves the source `item` ID instead of converting it to `fc_<hash>`. The Responses request
can then reference an item ID that does not exist for the target model. Use the same full identity
check as `sameModel` (including provider and API) before retaining the item ID.

- 	if source.Model != model.ID {
+ 	if source.Model != model.ID || source.Provider != model.Provider || source.API != string(model.API) {
  		if item == "" {
  			return call
  		}
  		return call + "|" + "fc_" + ShortHash(item)
  	}


─── internal/settings/settings.go:664-669 ───
[bug · medium] `allowShadow` is computed from an unlocked snapshot, but `updateUser` skips
`checkShadow` whenever it is true. A concurrent project update can replace the equal value (or add a
different `defaultModel`) between `Load` and the user-file lock, so this call can write a user
default that is currently shadowed. Revalidate the project value after acquiring the user lock, or
avoid carrying this pre-lock decision into the write.

- 	eff, err := l.Load(ctx)
- 	if err != nil {
+ 	if _, err := l.Load(ctx); err != nil {
  		return err
  	}
- 	allowShadow := eff.Sources[defaultModelKey] == LayerProject && eff.DefaultModel == modelID
- 	if !allowShadow {
+ 	allowShadow := false
+ 	if err := l.checkShadow([]string{defaultModelKey}); err != nil {
+ 		// The equality exception must be checked again while writing.
+ 		return err
+ 	}


─── internal/providers/replay.go:57-63 ───
[bug · medium] This accepts any syntactically valid JSON value, including `null`, `{}`, `[]`, or an
object with no `id`, as a usable reasoning replay. `responsesReasoningPart` then constructs metadata
with an empty `ItemID`, so malformed/corrupt persisted signatures can cause the adapter to send an
invalid reasoning item instead of falling back to the text. Validate the expected object shape (at
least a non-empty `Type`/`ID`, and any required version/type constraints) before returning `ok ==
true`.

  func DecodeThinkingSignature(sig string) (ReasoningReplay, bool) {
  	var r ReasoningReplay
- 	if err := json.Unmarshal([]byte(sig), &r); err != nil {
+ 	if err := json.Unmarshal([]byte(sig), &r); err != nil || r.Type == "" || r.ID == "" {
  		return ReasoningReplay{}, false
  	}
  	return r, true
  }


─── internal/settings/trust.go:70-73 ───
[bug · low] An empty `edit` returns success before checking `ctx` or validating `trust.json`.
Consequently `Update(ctx, map[string]*bool{})` succeeds with an already-canceled context and also
masks a corrupt/unsafe trust store, despite the method's documented contract that updates refuse
corrupt stores and take the lock/read fresh data. Check `ctx.Err()` and perform the normal store
validation before treating the operation as a no-op (or explicitly document this as an unconditional
no-op).

+ 	if err := ctx.Err(); err != nil {
+ 		return err
+ 	}
  	if len(edit) == 0 {
+ 		if _, err := s.Read(ctx); err != nil {
+ 			return err
+ 		}
  		return nil
  	}
  	home := s.files.home


─── internal/sessions/memory.go:83-94 ───
[security · medium] `checkEncodable` treats successful JSON encoding as sufficient, but the safety
guarantees documented on these fields are not enforced at this boundary. A caller can append a
`Failure` with arbitrarily large/unredacted text, put credential-like values in
`PreparedRequest.Headers`, or construct a `ModelRef.BaseURLHost` containing userinfo/path; all are
accepted and exposed through `Entries` for downstream persistence/publication. The current Agent
producers sanitize these values, but `MemoryLog` is the contract boundary and its exported entry
fields make malformed entries reachable. Validate/normalize the invariants here (or make the
constructors/fields enforce them) before cloning and committing.

  func checkEncodable(e Entry) error {
  	if e == nil {
  		return fmt.Errorf("%w: nil entry", ErrUnencodableEntry)
  	}
  	if m, ok := e.(MessageEntry); ok && m.Message == nil {
  		return fmt.Errorf("%w: nil message", ErrUnencodableEntry)
+ 	}
+ 	if err := validateSafeFields(e); err != nil {
+ 		return fmt.Errorf("%w: %w", ErrUnencodableEntry, err)
  	}
  	if _, err := json.Marshal(e); err != nil {
  		return fmt.Errorf("%w: %w", ErrUnencodableEntry, err)
  	}
  	return nil
  }


─── internal/providers/partialjson/partialjson.go:79-82 ───
[bug · high] The retry unmarshals into the same destination after the first failed `json.Unmarshal`.
Go's JSON decoder can leave fields populated when it returns an error, and a subsequent successful
unmarshal does not clear absent struct fields/map entries. Consequently `StrictWithRepair` can
return nil while `v` contains stale data from the failed parse (e.g. a malformed first attempt
populates `n`, while the repaired JSON omits it). Decode into a fresh temporary value and assign it
to `v` only after success, or reset the destination before the retry.

- 	if json.Unmarshal([]byte(repaired), v) != nil {
+ 	tmp := reflect.New(reflect.TypeOf(v).Elem()).Interface()
+ 	if err := json.Unmarshal([]byte(repaired), tmp); err != nil {
  		return err
  	}
+ 	reflect.ValueOf(v).Elem().Set(reflect.ValueOf(tmp).Elem())
  	return nil


─── internal/sandbox/buffer.go:191-193 ───
[bug · medium] When `spillLimit` is configured below the amount already buffered (for example, a
small test limit), `room` is negative and `p[:room]` panics as soon as spilling starts. The limit is
documented as the maximum spill-file size, so this should cap the initial buffered output at zero
(or otherwise handle an already-over-limit file) rather than slicing with a negative bound.

  	if room := s.limit - s.written; int64(len(p)) > room {
- 		p, cut = p[:room], true
+ 		if room <= 0 {
+ 			p = nil
+ 		} else {
+ 			p = p[:room]
+ 		}
+ 		cut = true
  	}


─── internal/settings/host-id.go:16-17 ───
[bug · medium] `NewAuthStoreReader` documents that every writing method fails with
`ErrReadOnlyStore`, but `HostID` does not check `s.readOnly`. Calling `HostID` on a reader can
create the home and `host-id` via `ensureHome`/`commit`, so a supposedly read-only command still
mutates the filesystem. Return `ErrReadOnlyStore` before the creation/commit path (or provide a
non-mutating read-only variant).

  func (s *AuthStore) HostID(ctx context.Context) (string, error) {
+ 	if s.readOnly {
+ 		return "", ErrReadOnlyStore
+ 	}
  	if err := ensureHome(s.home); err != nil {


─── internal/skills/loader.go:125-129 ───
[security · high] `os.Stat(file)` accepts a `SKILL.md` symlink and `readBody` later opens that path,
so a skill inside a trusted/project root can point at an arbitrary readable file outside the
configured root and expose its contents through `/skill:<name>`. Directory symlinks are likewise
followed by the `os.Stat(child).IsDir()` check. Resolve and constrain both the skill file and
traversed directories to the corresponding root before loading/following them (or reject symlinks).

  	file := filepath.Join(dir, skillFile)
- 	if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
+ 	if info, err := os.Lstat(file); err == nil && info.Mode().IsRegular() {
  		l.add(file, filepath.Base(dir), w.scope)
  		return
  	}


─── internal/skills/loader.go:89-90 ───
[maintainability · medium] The documented traversal order is not implemented: this loop uses the
caller's order unchanged, while `visited` is shared across all roots. If a later project root
reaches a directory already reached through an earlier user root (including via symlinks or
duplicate roots), it is skipped entirely, so discovery and the documented project-first result can
depend on input order. Sort/deduplicate roots deterministically or make visitation/precedence
explicit per root.

- 	for _, r := range roots {
+ 	ordered := slices.Clone(roots)
+ 	slices.SortStableFunc(ordered, func(a, b Root) int {
+ 		return strings.Compare(a.Dir, b.Dir)
+ 	})
+ 	for _, r := range ordered {
  		dir, err := filepath.Abs(r.Dir)


─── internal/skills/loader_budget.go:27-28 ───
[bug · medium] The multiplication happens before the budget's lower bound (and without overflow
protection). A large but valid model/configured `contextWindow` can overflow `int`, producing a
negative or unexpectedly small `Tokens` value; `limit()` then treats that as a character budget or
otherwise truncates incorrectly. Divide before multiplying or explicitly saturate the calculation.

  	case contextWindow > 0:
- 		return Budget{Tokens: max(contextWindow*2/100, 1)}
+ 		return Budget{Tokens: max(contextWindow/50, 1)}


─── internal/skills/loader_listing.go:155-155 ───
[bug · medium] The budget accounting does not include escaping performed by `ReminderText.Quote` for
description content. In particular, a description containing `<system-reminder` is rewritten to
`&lt;system-reminder`, adding bytes after `full`, `shareDescriptions`, and `cost` have accepted it.
The rendered listing can therefore exceed the configured budget (and repeated matching tags can make
the excess substantial). Compute costs from the quoted representation, or use the same
escaping-aware byte/rune accounting when measuring descriptions.

- func (e *entry) descBytes(n int) int { return len(string(e.desc[:n])) }
+ func (e *entry) descBytes(n int) int {
+ 	var q protocol.ReminderText
+ 	q.Quote(string(e.desc[:n]))
+ 	return q.Len() - len("<system-reminder>") - len("</system-reminder>")
+ }


─── internal/sandbox/buffer.go:191-193 ───
[bug · medium] This slice bound can be negative when the configured spill limit is smaller than the
bytes already written during `start` (e.g. `spillLimit` is a small test limit). In that case
`s.limit-s.written` is negative and the first spill append panics instead of producing a capped
file. Handle non-positive room explicitly before slicing.

  	if room := s.limit - s.written; int64(len(p)) > room {
- 		p, cut = p[:room], true
+ 		if room <= 0 {
+ 			p = nil
+ 		} else {
+ 			p = p[:room]
+ 		}
+ 		cut = true
  	}


─── internal/sessions/entry.go:218-219 ───
[bug · medium] `protocol.Message` is an open interface (implementations only need `Role()`), and
`protocol.CloneMessage` deliberately returns unknown/custom implementations unchanged. Therefore a
`MessageEntry` containing a caller-defined message with a mutable map, slice, or pointer stores the
same object in `MemoryLog`; `Entries`/`Messages` can then expose shared mutable state and violate
this type's documented deep-copy guarantee. Either reject non-built-in message implementations at
this boundary or add a cloning/serialization strategy that actually isolates them.

  v.Message = protocol.CloneMessage(v.Message)
  		return v


─── internal/sandbox/view.go:63-68 ───
[bug · medium] The no-drop path measures truncation after `sanitize`, so bytes removed as ANSI
sequences or carriage-return progress updates are not included in `ElidedBytes` (and can make a
large raw output appear completely untruncated). `Truncation.ElidedBytes` is documented as bytes in
the output not in the text and `TotalBytes` is raw bytes; preserve/count the raw kept/elided bytes
before sanitizing, or apply the same accounting used in the dropped path.

  	if out.Dropped == 0 {
- 		full := sanitize(string(out.Head) + string(out.Tail))
- 		if countLines(full) <= maxLines && len(full) <= maxBytes {
+ 		raw := string(out.Head) + string(out.Tail)
+ 		full := sanitize(raw)
+ 		if countLines(full) <= maxLines && len(raw) <= maxBytes {
  			text = full
  			tr.ShownLines, tr.ShownBytes = countLines(full), len(full)
  		} else {


─── internal/skills/loader_listing.go:108-110 ───
[bug · high] When descriptions are shared, `used` includes each included entry's actual (possibly
non-empty) description cost. If the omission line does not fit, this loop removes an entry but
subtracts only its minimum/name-only cost. The removed entry's description cost remains counted, so
the loop can drop additional skills unnecessarily (and report them as omitted) even though the
rendered list would fit with the previously included entries. Subtract the entry's actual rendered
cost (or recompute `used` after changing `included`).

  		included--
  		omitted++
- 		used -= b.cost(&es[included], 0, 0)
+ 		used -= b.cost(&es[included], es[included].shown, es[included].descBytes(es[included].shown))


─── internal/tasks/registry.go:133-136 ───
[other · medium] `NextID` is called before the registry checks `r.closed`, so a `Start` after
`Close` still allocates an ID (and can run arbitrary callback logic) before returning the documented
`ErrClosed`. In this package the callback is `Agent.nextTaskID`, which mutates the agent-wide
sequence, so rejected admissions consume task numbers; move the closed/admission check ahead of ID
generation (while preserving duplicate checking under the registry lock).

+ 	r.mu.Lock()
+ 	if r.closed {
+ 		r.mu.Unlock()
+ 		return Handle{}, ErrClosed
+ 	}
+ 	r.mu.Unlock()
+ 
  	id := r.cfg.NextID(spec.Kind)
  	if !validID(id) {
  		return Handle{}, fmt.Errorf("%w: task id %q is not a plain name", ErrInvalidSpec, id)
  	}


─── internal/tasks/sweep.go:116-121 ───
[security · medium] The validation/removal sequence is vulnerable to a symlink race: after
`sessionFiles` confirms `dir` is a real 0700 directory, a same-user process can rename it and
replace that pathname with a symlink before `removeSession`; `os.Remove(filepath.Join(dir,
f.Name()))` then follows the directory symlink and can delete same-user files outside `root`. Use
directory-relative operations anchored to an opened directory (and verify the directory identity) or
otherwise make removal resistant to replacement.

- 	for _, f := range files {
- 		if err := os.Remove(filepath.Join(dir, f.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
- 			errs = append(errs, err)
- 		}
- 	}
- 	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
+ 	// Remove entries through an already-open directory handle, after checking
+ 	// its identity, so replacing dir cannot redirect these removals.
+ 	// (Use platform-appropriate *at/no-follow operations here.)


─── internal/tasks/notice.go:61-74 ───
[other · low] `boundField` slices to `fieldMaxBytes` and then appends the ellipsis, so a truncated
field is up to 1,027 bytes rather than the documented 1,024-byte bound. Since this helper is used
for notice title/summary-like fields and `noticeDetails.Title`, the notice size limit is
systematically exceeded for long values. Reserve the ellipsis bytes before slicing (and apply the
same UTF-8 boundary logic to that reduced limit).

- func boundField(s string) string {
- 	if len(s) <= fieldMaxBytes {
- 		return s
- 	}
- 	s = s[:fieldMaxBytes]
+ 	const suffix = "..."
+ 	s = s[:fieldMaxBytes-len(suffix)]
  	for len(s) > 0 {
  		r, size := utf8.DecodeLastRuneInString(s)
  		if r != utf8.RuneError || size > 1 {
  			break
  		}
  		s = s[:len(s)-1]
- 	}
- 	return s + "..."
- }
+ 	}
+ 	return s + suffix


─── internal/skills/loader_listing.go:105-107 ───
[bug · medium] If the budget is smaller than the omission line (which can happen with `BudgetFor`
returning a 1-token budget), `included` reaches zero and this condition breaks without checking
whether the omission line fits. The renderer then unconditionally emits `omissionLine(omitted)`, so
the result exceeds the requested budget despite the API's budget contract. Omit the line when it
cannot fit (or define an explicit minimum-budget exception).

- 		if used+b.lineCost(line) <= b.limit() || included == 0 {
+ 		if used+b.lineCost(line) <= b.limit() {
+ 			break
+ 		}
+ 		if included == 0 {
+ 			omitted = 0
  			break
  		}


─── internal/tools/file_edit.go:231-233 ───
[security · high] `FileState.Check` and the subsequent `os.ReadFile` only snapshot mtime/size before
this call; they do not protect against a writer outside this process. An external change can occur
after the check/read and before this rename, and this unconditional replacement will silently
discard it. Revalidate the file identity/version immediately before committing (or use an OS-level
locking/open-handle strategy that covers the read-to-replace interval) and fail instead of
overwriting a newer file.

+ if err := e.fs.Check(abs); err != nil {
+ 		return protocol.ToolExecutionResult{}, failf("%s changed on disk since you last read it. Read it again before you edit.", in.Path)
+ 	}
- if err := WriteFileAtomic(abs, []byte(out), 0o644); err != nil {
+ 	if err := WriteFileAtomic(abs, []byte(out), 0o644); err != nil {
  		return protocol.ToolExecutionResult{}, err
  	}


─── internal/tools/file_edit_match.go:34-34 ───
[security · high] The matcher reaches this 0.8 whole-line similarity rung when the requested
`oldText` is absent, but `applyEdits` commits the replacement without exposing the rung or requiring
confirmation. This can apply an edit to a semantically different block (especially for short/common
lines) while reporting success. Approximate matching should not be used for a mutating tool without
an explicit opt-in/confirmation, or this rung should return an ambiguity/mismatch error.

- fuzzyLooseThreshold = 0.8
+ fuzzyLooseThreshold = 0.8 // do not use this rung for edits without explicit confirmation


─── internal/tools/file_image.go:245-247 ───
[performance · medium] This decodes the complete image before any decoded-dimension or output-size
check. `Read` already loads arbitrary file bytes, and a small compressed image can expand to a very
large pixel buffer, allowing excessive memory/CPU use (or decoder allocation failure) despite the
bounded inline result. Apply a conservative input/decompression limit or dimension-aware decode
before accepting the image.

- im, err := decodeImage(out.data, out.mime)
+ im, err := decodeImageWithLimits(out.data, out.mime, lim)
  		if err != nil {
  			return inlineImage{}, &imageError{imageNotResizable}


─── internal/tools/coerce.go:196-199 ───
[bug · medium] Parsing a numeric string through `float64` loses integer precision and can change the
value before schema validation/execution. For example, a valid string representing an integer above
2^53 can round to a neighboring integer when `formatNumber` serializes it; the tool then receives a
different argument. Parse/format integer inputs exactly (and preserve decimal lexical precision for
numbers) rather than routing them through `float64`.

- f, ok := parseNumber(x)
- 			if ok && (typ == "number" || f == math.Trunc(f)) {
- 				return formatNumber(f), true
+ n, ok := parseNumberExact(x, typ)
+ 			if ok {
+ 				return n, true
  			}


─── internal/tools/file_read.go:123-126 ───
[performance · high] `io.ReadAll` materializes the entire file before `readText` can apply the
documented line/byte limits (and before deciding whether it is an image/binary). A model can
therefore request a multi-GB regular file and force an allocation/OOM even though the returned text
is capped. Detect/stream the relevant prefix and read only the bounded text window (while applying
an appropriate bounded strategy for images) rather than loading the whole file unconditionally.

- 	data, err := io.ReadAll(f)
+ 	data, err := readBoundedOrImage(f, info, in)
  	if err != nil {
  		return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
  	}


─── internal/tools/file_write.go:82-84 ───
[other · high] Unlike `Edit.Execute`, this overwrite path never calls `w.fs.Check(abs)` after
acquiring the path lock. If the session previously read or wrote the file and another process
changes it before this call, `write` unconditionally replaces that newer content, defeating the
per-session stale-file protection and causing data loss. Check the recorded state for existing files
(while allowing genuinely new files) before `WriteFileAtomic`.

- 	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
+ 	if _, err := os.Stat(abs); err == nil {
+ 		if err := w.fs.Check(abs); err != nil {
+ 			return protocol.ToolExecutionResult{}, failf("Cannot write %s: it changed on disk since you last read it. Read it again before you write.", in.Path)
+ 		}
+ 	} else if !os.IsNotExist(err) {
  		return protocol.ToolExecutionResult{}, failf("Cannot write %s: %s.", in.Path, reason(err))
  	}


─── internal/testsupport/oauth-ports.go:15-15 ───
[other · medium] `syscall.Flock` is not defined by the Windows syscall package. Because this file
has no build constraint (and `internal/testsupport` is imported by tests across the module), a
Windows build/test will fail at compile time with an undefined symbol. Put this implementation
behind an appropriate Unix build tag and provide a Windows-compatible implementation or skip helper,
matching the existing `*_unix.go` pattern.

+ //go:build unix
+ 
  func LockOAuthPorts(t *testing.T) {


─── internal/testsupport/fsfault/fsync-gate.c:80-80 ───
[other · low] `pread` and `pwrite` return `ssize_t`, but the cast to `int` happens before `result`
checks the error. On 64-bit systems a successful transfer larger than `INT_MAX` can wrap/truncate
and FUSE will receive an incorrect byte count (or a negative value interpreted as an error).
Preserve the `ssize_t` result through error handling and explicitly cap/reject values that cannot be
represented by the FUSE callback's `int` return type.

- return result((int)pread((int)fi->fh, buf, size, offset));
+ ssize_t n = pread((int)fi->fh, buf, size, offset);
+     if (n < 0) return -errno;
+     return n > INT_MAX ? -EOVERFLOW : (int)n;


─── internal/tools/file_state.go:66-67 ───
[other · medium] The version stamp is only `(mtime, size)`, so it is not a reliable change detector:
an external rewrite that preserves the length and restores/preserves the timestamp (or occurs within
a coarse timestamp bucket) produces the same stamp and `Check` returns nil. Since `Edit` relies on
this check before replacing the file, stale session content can overwrite a newer same-size file.
Include a stronger identity/content fingerprint (or otherwise revalidate the file contents) in the
recorded state.

- 	if info.ModTime().UnixNano() != want.mtime || info.Size() != want.size {
+ 	if info.ModTime().UnixNano() != want.mtime || info.Size() != want.size || contentFingerprint(key) != want.fingerprint {
  		return fmt.Errorf("%w: %s was modified at %s; read it again before you change it",


─── internal/tools/file_read.go:130-132 ───
[other · medium] `info` is obtained before `io.ReadAll`, but that same pre-read metadata is recorded
after the read. If the file is replaced or rewritten during the read, the session records a stamp
that may describe neither the bytes returned to the model nor the current file. The next edit can
then be rejected spuriously, or—if the file returns to the old stamp—proceed against content
different from what was read. Stat/revalidate after reading (and only record when the read's
snapshot is still valid), or use a read/version strategy that couples the bytes and recorded
version.

- 	r.fs.Record(abs, info)
+ 	latest, statErr := os.Stat(abs)
+ 	if statErr != nil {
+ 		return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(statErr))
+ 	}
+ 	if latest.ModTime() != info.ModTime() || latest.Size() != info.Size() {
+ 		return protocol.ToolExecutionResult{}, failf("Cannot read %s: it changed while being read; try again.", in.Path)
+ 	}
+ 	r.fs.Record(abs, latest)
  
  	if mime := detectImage(data[:min(len(data), imageSniffBytes)]); mime != "" {


─── internal/tools/file_read.go:188-191 ───
[bug · high] An out-of-range JSON number can make this request path panic. For example, `offset:
1e100` passes the `> total` check, then converting it to `int` can produce a negative/wrapped value;
the loop subsequently indexes `lines[i]` with that value. Validate that offset is finite, integral,
and within the representable/int-safe range before converting it (and return a model-facing
validation error).

  	start := 0
  	if in.Offset != nil && *in.Offset > 1 {
+ 		if math.IsNaN(*in.Offset) || math.IsInf(*in.Offset, 0) || math.Trunc(*in.Offset) != *in.Offset || *in.Offset > float64(int(^uint(0)>>1)) {
+ 			return protocol.ToolExecutionResult{}, failf("Offset must be a finite integer within the supported range.")
+ 		}
  		start = int(*in.Offset) - 1
  	}


─── internal/testsupport/fsfault/fsync-gate.c:33-35 ───
[bug · medium] `stopping` is read here without holding `mutex`, while `main` writes it under that
mutex after `fuse_main` returns. A callback/control wait can therefore observe this field
concurrently with the shutdown write, which is a C data race and undefined behavior (and can prevent
the wait from reliably noticing shutdown). Protect this read with `mutex`, or make the flag atomic
and use atomic accesses consistently.

  static int gate_stopping(struct fuse *fs) {
-     return stopping || (fs && fuse_session_exited(fuse_get_session(fs)));
+     int stopping_now;
+     pthread_mutex_lock(&mutex);
+     stopping_now = stopping;
+     pthread_mutex_unlock(&mutex);
+     return stopping_now || (fs && fuse_session_exited(fuse_get_session(fs)));
  }


─── internal/tasks/sink.go:104-104 ───
[other · high] This package has no platform build constraints, but `syscall.O_NOFOLLOW` is not
defined by the Windows syscall package (and `sweep.go` also uses Unix-only
`os.Getuid`/`syscall.Stat_t`). Consequently a normal `GOOS=windows go build ./...` cannot compile
this newly added package. Please isolate the Unix-specific implementation behind build tags and
provide a platform-neutral/Windows counterpart, or explicitly exclude the package from unsupported
targets.

- f, err := os.OpenFile(s.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
+ // Move the O_NOFOLLOW open into a platform-specific helper (with a Windows implementation),
+ // rather than referencing syscall.O_NOFOLLOW from this portable file.


─── internal/testsupport/fsfault/fsync-gate.c:99-100 ───
[bug · high] This waits for `RELEASE` while holding `mutex`, but the control thread must acquire the
same mutex to process the `RELEASE` command and set `released`. Consequently the FUSE callback waits
until its 30-second timeout and the test cannot release the gate (the analogous `sync_directory`
wait has the same deadlock with `RELEASE_EXIT`). Set/read the gate state under the mutex, unlock
before waiting, then re-lock to continue.

+         pthread_mutex_unlock(&mutex);
          int e = wait_gate(&released, 30, fuse_get_context()->fuse);
-         if (e) { pthread_mutex_unlock(&mutex); return -e; }
+         if (e) return -e;
+         pthread_mutex_lock(&mutex);


─── internal/tools/shell_bash.go:300-305 ───
[bug · medium] `sandbox.View` does not record whether truncation was caused by bytes or lines;
`ShownLines` is only the number of retained head/tail lines. A byte-limited output can retain
exactly `DefaultMaxLines` lines, causing this branch to report `lines` even though the byte limit
caused the cut. This makes `ToolResultDetails.Truncation.TruncatedBy` incorrect for consumers that
use it to decide how to fetch the remainder. Determine the limiting dimension from the view
limits/elided data (or add that field to `sandbox.Truncation`) rather than inferring it from the
retained line count.

  	by := "bytes"
- 	if tr.ShownLines >= sandbox.DefaultMaxLines {
+ 	// Preserve the actual limiting dimension from sandbox.View rather than
+ 	// inferring it from the number of retained lines.
+ 	if tr.Limit == "lines" {
  		by = "lines"
  	}
  	return &protocol.ToolTruncation{
  		TruncatedBy: by,


─── internal/testsupport/fsfault/fsync-gate.c:167-168 ───
[bug · high] The directory callback has the same mutex/condition-variable deadlock: it holds `mutex`
while waiting for `RELEASE_EXIT`, but the control thread can only process `RELEASE_EXIT` after
acquiring `mutex`. Thus the command cannot unblock this callback and the test will stall until the
timeout. Refactor `wait_gate`/its locking contract so the wait releases `mutex` while sleeping and
reacquires it for condition checks.

-         int e = wait_gate(&exit_released, 30, fuse_get_context()->fuse);
+         /* Wait without holding mutex so the control thread can process RELEASE_EXIT. */
+         int e = wait_gate_unlocked(&exit_released, 30, fuse_get_context()->fuse);
          if (e) rc = -e;


─── internal/tasks/sweep.go:106-109 ───
[other · high] This file is also compiled for Windows, but `os.Getuid` is unavailable there and
`syscall.Stat_t` does not provide this Unix ownership representation. Since `Sweep` is part of the
package imported by the agent, this prevents cross-compilation/building for Windows just like the
Unix-only `O_NOFOLLOW` use in `sink.go`. Move ownership checks into OS-specific files (or provide a
Windows implementation) and keep this portable sweep logic free of Unix-only symbols.

- func ownedByCurrentUser(info fs.FileInfo) bool {
- 	st, ok := info.Sys().(*syscall.Stat_t)
- 	return ok && int(st.Uid) == os.Getuid()
- }
+ // Put ownedByCurrentUser in OS-specific files, with an implementation for each
+ // supported target; do not reference os.Getuid/syscall.Stat_t here.


─── internal/tools/file_write.go:88-90 ───
[security · medium] The generated-file check and the content snapshot are performed before this
rename, so a concurrent writer can replace the file after `os.ReadFile` (or add a generator marker)
and this unconditional atomic rename will overwrite that newer content. Unlike the edit path, this
write has no last-moment identity/mtime check. Re-stat/revalidate immediately before replacing (or
otherwise coordinate with external writers) if the generated-file guard and overwrite semantics are
intended to be reliable.

+ 	// Revalidate the target immediately before replacing it to avoid overwriting
+ 	// changes made after the initial read/generated-file check.
  	if err := WriteFileAtomic(abs, []byte(in.Content), 0o644); err != nil {
  		return protocol.ToolExecutionResult{}, err
  	}


─── internal/tui/blocks.go:37-37 ───
[bug · low] `SpinnerFrames` is an exported mutable slice, but both `SpinnerFrame` and `runningState`
divide/modulo by its length without guarding against zero. Any in-package caller or external
consumer allowed to replace or clear this variable can make a running block panic during rendering
(`integer divide by zero`). Keep the frames private (and expose a copy if customization is needed),
or handle an empty slice before modulo/division.

- var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
+ var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}


─── internal/testsupport/fsfault/fsync-gate.c:33-35 ───
[bug · high] A mutex-based fix here must not lock `mutex`: `gate_stopping` is called by `wait_gate`
while that mutex is already held, so doing so would self-deadlock. Use an atomic flag (with atomic
stores/loads at all accesses), or redesign the wait/shutdown locking contract rather than locking
recursively.

+ static _Atomic int stopping;
  static int gate_stopping(struct fuse *fs) {
-     return stopping || (fs && fuse_session_exited(fuse_get_session(fs)));
+     return atomic_load(&stopping) || (fs && fuse_session_exited(fuse_get_session(fs)));
  }


─── internal/tools/shell_monitor.go:82-85 ───
[bug · medium] `math.IsNaN` and the `<= 0` check do not reject positive infinity. For `timeout_ms:
1e309` (decoded as `+Inf`), `math.Ceil(+Inf)` is converted to `int64`, which is
implementation-dependent/out-of-range; on typical Go targets this produces a negative duration.
`StartMonitor` then treats the non-positive timeout as the default 10-hour timeout while the
returned `TimeoutMs` is also nonsensical, rather than rejecting the invalid input or applying the
documented cap. Reject non-finite values (e.g. with `math.IsInf`) before converting to `int64`.

  	if in.TimeoutMs != nil {
- 		if math.IsNaN(*in.TimeoutMs) || *in.TimeoutMs <= 0 {
- 			return protocol.ToolExecutionResult{}, failf("timeout_ms must be more than 0.")
+ 		if math.IsNaN(*in.TimeoutMs) || math.IsInf(*in.TimeoutMs, 0) || *in.TimeoutMs <= 0 {
+ 			return protocol.ToolExecutionResult{}, failf("timeout_ms must be a finite number more than 0.")
  		}


─── internal/tui/editor_paste.go:25-32 ───
[performance · medium] Every multiline paste over the threshold is copied into `p.texts` and there
is no eviction or cleanup when the marker is submitted, undone, or removed from the draft. A user
can repeatedly paste large content and retain all of it for the entire TUI lifetime, causing
unbounded memory growth. Bound the store or delete entries that are no longer reachable (while
preserving any markers still present in undo/history).

  func (p *pasteStore) add(text string) int {
  	if p.texts == nil {
  		p.texts = make(map[int]string)
  	}
  	p.next++
  	p.texts[p.next] = text
  	return p.next
  }


─── internal/tui/editor_paste.go:49-52 ───
[security · medium] Marker provenance is inferred only from the numeric ID and line count. Once an
earlier paste has populated `e.pastes.texts[id]`, user-entered or recalled text matching `[paste
#id, N lines]` is treated as that paste and `submit` replaces it via `expand`, silently changing the
prompt sent to the agent. Track marker tokens/provenance separately from ordinary draft text (and
history), rather than recognizing markers by a regex over the current text.

  		full, ok := e.pastes.texts[id]
  		if ok && lineCount(full) == lines {
  			spans = append(spans, markerSpan{start: m[0], end: m[1], id: id})
  		}


─── internal/testsupport/fsfault/fsync-gate.c:8-9 ───
[bug · high] This translation unit uses `uint64_t`, `uintptr_t`, `struct timespec`, `clock_gettime`,
and `CLOCK_REALTIME`, but does not include `<stdint.h>` or `<time.h>`. The Dockerfile compiles with
`-Werror`, and these types/macros are not guaranteed to be provided transitively by the FUSE
headers, so the helper can fail to build on the stated image/toolchain. Include the headers
directly.

  #include <sys/statvfs.h>
+ #include <stdint.h>
+ #include <time.h>
  #include <pthread.h>


─── internal/tools/shell_monitor.go:89-93 ───
[bug · medium] When `persistent` is true, `timeout_ms` is documented as irrelevant and the value is
later replaced with zero, but it is converted to `int64` first. A valid finite JSON value such as
`1e308` therefore overflows during `int64(math.Ceil(...))` before the persistent branch, producing
an invalid timeout/metadata (and potentially an unintended timeout). Skip conversion for persistent
monitors, or validate/cap before converting.

+ 		if !persistent {
- 		timeoutMs = int64(math.Ceil(math.Min(*in.TimeoutMs, float64(monitorMaxTimeoutMs))))
+ 			timeoutMs = int64(math.Ceil(math.Min(*in.TimeoutMs, float64(monitorMaxTimeoutMs))))
+ 		}
  	}
  	if persistent {
  		timeoutMs = 0
  	}


─── internal/tui/markdown_table.go:91-98 ───
[bug · medium] The table body scan accepts every nonblank, non-heading line after the delimiter as a
data row. For example, `| a | b |\n|---|---|\nordinary prose` is consumed by `splitCells` as one
cell and disappears from `after`, so normal text immediately following a table is rendered inside
the table. Require each body line to be a valid table row (at least a pipe and the expected column
count, consistent with the header) and stop the scan when it is not.

  		end := i + 1
  		for end < len(lines) && strings.TrimSpace(lines[end]) != "" && !tableBreak.MatchString(lines[end]) {
+ 			cells := splitCells(lines[end])
+ 			if !hasPipe(lines[end]) || len(cells) != len(header) {
+ 				break
+ 			}
  			end++
  		}
  		t = textTable{header: inlineCells(header, len(header)), align: align}
  		for _, line := range lines[i+1 : end] {
  			t.rows = append(t.rows, inlineCells(splitCells(line), len(header)))
  		}


─── internal/tui/footer.go:62-62 ───
[bug · medium] `ctxTokens` and `window` are int64, but this multiplication can overflow before the
division. Usage values come from protocol messages and model/context metadata is configurable, so a
sufficiently large token count can produce a negative percentage (and consequently an incorrect
bar/role). Compute the rounded ratio without multiplying the full token count by 100, or guard the
multiplication against overflow.

- 	return int((s.ctxTokens*100 + window/2) / window)
+ 	return int(s.ctxTokens/window*100 + (s.ctxTokens%window)*100/window + ((s.ctxTokens%window)*100%window+window/2)/window)


─── internal/tui/markdown_split.go:95-99 ───
[bug · low] A closing fence is recognized here even when it is the final unterminated line, but
`ln.complete` is then passed as the chunk's closed flag. Thus a complete reply ending in ` ``` `
without a trailing newline remains marked open, so the markdown cache does not treat its code chunk
as stable and may redraw it as though more code could still arrive. Once `isFenceCloser` matches,
the fence has closed regardless of whether the source has a final newline; flush it as closed.

  			if isFenceCloser(text, cur.fence) {
  				cur.inFence = false
  				cur.end = lineEnd
  				if cur.code {
- 					flushed(cur, ln.complete)
+ 					flushed(cur, true)


─── internal/tui/model_cycle.go:100-102 ───
[bug · medium] If the session generation changes while `Models` is in flight (for example on
disconnect/reconnect or `useSession`), this response still matches `s.cycleFetch`, so `s.cycling` is
cleared, but the `msg.gen != s.gen` path returns before starting a replacement request. More
importantly, when the generation changes before the response arrives, the token check clears
`cycling` only if it matches and then the stale generation is discarded; if a newer cycle fetch has
reused/advanced the token, the stale response is discarded without clearing the state for the
current request, which is safe only if the current request is guaranteed to exist. The lifecycle
needs an explicit invalidation/reset of cycle state on generation changes (or a generation-aware
cancellation path), otherwise a stale fetch can leave model cycling locked or allow a reply to be
associated with the wrong session.

  	if msg.gen != s.gen {
  		return nil
  	}


─── internal/tui/update_providers.go:482-489 ───
[security · high] `take` converts the secret bytes to an immutable `string` and the returned value
is captured by the long-lived TEA command closure passed to `SaveAPIKey`/`SendLoginCode`. `clear`
can only wipe `d.secret.buf`; it cannot wipe this copy, so a slow, cancelled, or abandoned request
retains the credential in heap memory for up to the command timeout (90 seconds for key saves). This
contradicts the stated “lives only in this buffer” guarantee. Avoid representing the in-flight
secret as a Go string (for example, change the client boundary to accept owned bytes and explicitly
clear them after the call), or otherwise document and tightly bound this unavoidable copy.

  	secret := d.secret.take()
  	if d.isKey {
  		d.loginID = newLoginID()
  		id := d.loginID
  		return func() tea.Msg {
+ 			defer clearBytes(secret)
  			ctx, cancel := context.WithTimeout(context.Background(), keySaveTimeout)
  			defer cancel()
- 			out, err := c.SaveAPIKey(ctx, id, opt.ProviderID, secret)
+ 			out, err := c.SaveAPIKeyBytes(ctx, id, opt.ProviderID, secret)


─── internal/tui/selection.go:172-178 ───
[bug · medium] `softWraps` collapses every run of whitespace in both the wide and narrow rows with
`strings.Fields`, then `lineBuilder.add` reconstructs a wrapped boundary with exactly one space for
`joinSpace`. A logical line containing multiple spaces (or tabs, which are rendered as spaces)
therefore copies incorrectly: e.g. a wrap after `foo   bar` produces `foo bar`, even though
`cellSlice` preserves the source cells. This also makes matching ambiguous for rows whose only
distinction is spacing. Preserve the original cell/column text while determining the join type
instead of normalizing it before reconstruction.

- nextLine := func() {
- 		rest = ""
- 		for k < len(wide) && rest == "" {
- 			rest = strings.Join(strings.Fields(wide[k]), " ")
+ 			// Keep the original cells for copy reconstruction; normalize only a
+ 			// separate comparison string when classifying the wrap.
+ 			rest = wide[k]
  			k++
- 		}
- 	}


─── internal/tui/session.go:328-331 ───
[other · medium] All of the session commands created through `callContext` inherit no owner
cancellation: quitting only waits for the special login stop command, while in-flight
model/session/provider/queue calls continue against the old client until the 30-second timeout.
Generation checks prevent most stale UI updates, but they do not stop the underlying calls or
release client-side resources promptly, and a reconnect/quit can leave those operations running
after their modal/session owner is gone. Pass a session-owned cancellation context into these
commands and cancel it on generation change/quit (while retaining per-call deadlines).

- // callContext returns a context for one client call.
- func callContext() (context.Context, context.CancelFunc) {
- 	return context.WithTimeout(context.Background(), callTimeout)
+ // callContext returns a context for one client call owned by the current session.
+ func (s *sessionView) callContext() (context.Context, context.CancelFunc) {
+ 	return context.WithTimeout(s.ctx, callTimeout)
  }


─── internal/tui/login_box.go:378-384 ───
[bug · high] This loop can call `dropRow(-1)` and panic for a compact dialog. If the cursor has been
shifted to row 0 by the preceding intro-row removal while rows after the input still exceed the
budget (for example, a very short terminal showing a key refusal/error), the `i == 0` iteration
decrements `i` to -1 and passes it to `dropRow`. Keep the cursor row and drop another valid row, or
stop compacting when no non-cursor row remains.

  		for len(c.rows) > budget {
  			i := len(c.rows) - 1
  			if c.cursor != nil && c.cursor.Row == i {
+ 				if i == 0 {
+ 					break
+ 				}
  				i--
  			}
  			c.dropRow(i)
  		}


─── internal/tools/file_state.go:51-54 ───
[security · high] A symlink retarget is treated as an unrecorded path: `Record` stores the resolved
target (for example A), but after the symlink changes to B, `Check` resolves the name to B, finds no
entry, and returns nil. `Edit` can then apply the model's edits to B even though the session last
read A, bypassing the stale-file protection (and potentially modifying an unexpected file). Track
the original path/symlink identity as well, or make a retargeted/unrecorded resolution fail when a
prior record exists for that path.

  func (s *FileState) Check(path string) error {
  	key := canonicalPath(path)
+ 	// Also retain/check the original path's link identity so a retargeted
+ 	// symlink cannot turn a previously recorded file into an unrecorded one.
  	s.mu.Lock()
  	want, ok := s.files[key]


─── internal/tui/update_providers.go:577-579 ───
[performance · high] `WaitLogin` runs in a goroutine with a context that is independent of the
TUI/client lifetime. Closing the dialog or quitting only drops the UI state; it does not cancel this
wait (and `Client.WaitLogin` explicitly leaves the sign-in open when its context ends). As a result,
a quit/session change can retain the login/client operation for up to 12 minutes and its result will
arrive after the owner is gone. Use an owner-scoped cancelable context and cancel it when the
dialog/session is closed (while separately deciding whether to send `CancelLogin`).

- ctx, cancel := context.WithTimeout(context.Background(), loginWaitMax)
+ ctx, cancel := context.WithCancel(s.ctx)
  		defer cancel()
  		out, err := c.WaitLogin(ctx, id)


LLM retry report summary: 120 of 1423 requests affected -- 17 requests failed, 103 requests recovered after retry

Review planning (6 requests):
- internal/tuiclient/client.go,internal/tuiclient/events.go,internal/tuiclient/mailbox.go,internal/tuiclient/prompt.go,internal/tuiclient/providers.go,internal/tuiclient/rpc/conn.go,internal/tuiclient/rpc/doc.go,internal/tuiclient/rpc/transport.go,internal/tuiclient/trust.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- internal/workspace/path.go,internal/workspace/project.go,internal/workspace/trust.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- pkg/askui/doc.go,pkg/protocol/acp.go,pkg/protocol/acp_providers.go,pkg/protocol/acp_session.go,pkg/protocol/acp_skills.go,pkg/protocol/acp_trust.go,pkg/protocol/builder.go,pkg/protocol/codec.go,pkg/protocol/codec_fast.go,pkg/protocol/content.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- pkg/protocol/events.go,pkg/protocol/job_notice.go,pkg/protocol/leader.go,pkg/protocol/message.go,pkg/protocol/reminder.go,pkg/protocol/skill_input.go,pkg/protocol/stream_events.go,pkg/protocol/tool.go,pkg/protocol/usage.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- internal/leader/peer_darwin.go,internal/leader/peer_linux.go,internal/leader/peer_other.go,internal/leader/policy.go,internal/leader/proc_darwin.go,internal/leader/proc_linux.go,internal/leader/proc_other.go,internal/leader/queue.go,internal/leader/reverse.go,internal/leader/router.go: provider error (HTTP 503) -> succeeded
- ... and 1 more

Core review (110 requests):
- internal/tools/file_read.go,internal/tools/file_state.go,internal/tools/file_write.go,internal/tools/prompt.go,internal/tools/registry.go,internal/tools/search_find.go,internal/tools/search_grep.go,internal/tools/search_walk.go,internal/tools/selection.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- internal/tools/shell_bash.go,internal/tools/shell_jobs.go,internal/tools/shell_monitor.go,internal/tools/source.go,internal/tools/truncate.go,internal/tools/types.go,internal/tools/validate.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- internal/tui/completion.go,internal/tui/dialog.go,internal/tui/doc.go,internal/tui/editor.go,internal/tui/editor_history.go,internal/tui/editor_keys.go,internal/tui/editor_layout.go,internal/tui/editor_paste.go,internal/tui/focus.go,internal/tui/fold.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- internal/tui/footer.go,internal/tui/header.go,internal/tui/keys.go,internal/tui/layout.go,internal/tui/login_box.go,internal/tui/markdown.go,internal/tui/markdown_code.go,internal/tui/markdown_list.go,internal/tui/markdown_split.go,internal/tui/markdown_table.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- internal/tui/messages.go,internal/tui/model.go,internal/tui/model_cycle.go,internal/tui/mouse.go,internal/tui/pane.go,internal/tui/pending.go,internal/tui/providers_box.go,internal/tui/quit.go,internal/tui/reconnect.go,internal/tui/row_time.go: network error -> network error -> network error -> network error -> network error -> network error -> failed
- ... and 105 more

Comment filtering (4 requests):
- cmd/tui/acp.go,cmd/tui/args.go,cmd/tui/auth.go,cmd/tui/auth_args.go,cmd/tui/auth_input.go,cmd/tui/capture.go,cmd/tui/connect.go,cmd/tui/headless.go: provider error (HTTP 503) -> succeeded
- internal/providers/faux/faux.go,internal/providers/faux/script.go,internal/providers/faux/stream.go,internal/providers/faux/usage.go,internal/providers/generated_catalog.go,internal/providers/keys.go,internal/providers/model.go,internal/providers/model_config.go,internal/providers/model_pattern.go,internal/providers/model_set.go: provider error (HTTP 503) -> provider error (HTTP 503) -> succeeded
- internal/tui/block_list.go,internal/tui/blocks.go,internal/tui/blocks_diff.go,internal/tui/blocks_format.go,internal/tui/blocks_hook.go,internal/tui/blocks_mark.go,internal/tui/blocks_preview.go,internal/tui/blocks_step.go,internal/tui/client.go,internal/tui/clipboard.go: provider error (HTTP 503) -> provider error (HTTP 503) -> succeeded
- internal/tui/footer.go,internal/tui/header.go,internal/tui/keys.go,internal/tui/layout.go,internal/tui/login_box.go,internal/tui/markdown.go,internal/tui/markdown_code.go,internal/tui/markdown_list.go,internal/tui/markdown_split.go,internal/tui/markdown_table.go: network error -> network error -> network error -> succeeded

Per-attempt detail: --format json (retry_report).
