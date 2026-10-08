# H13a SDK conformance report (D17)

Date: 2026-10-07. Outcome: D17 closed for coder SDK v0.13.5 with two small owned guards. lx-wnk v1.21.0 and Caelis v1.4.0 stay unverified (not tested, not failed).
Scope: required Ask contracts only, not blanket ACP certification.

## Pins

| Item | Value |
|---|---|
| Module | github.com/coder/acp-go-sdk v0.13.5 (Go 1.21, no requires) |
| Module sum (h1) | LI9jq5xon7xslaYlnoktvTVyDlE37yIk2daT7N9ASYk= |
| go.mod sum | yKzM/3R9uELp4+nBAwwtkS0aN1FOFjo11CNPy37yFko= |
| SDK tag commit | 0845a3bb9eddda5bfc22a94dd3598c90cb842451 (refs/tags/v0.13.5) |
| Schema tag | agentclientprotocol/agent-client-protocol v0.13.5, tag object 3e7721a31dd094ea43315be37cd101cf4f794af1, commit e22561876c59e378ea4e25a6f2d1d63313517aba |
| ACP wire version | 1 (schema/meta.json) |
| Stable schema.json SHA-256 | 0da9fe718746ccc2e8ef789efa6687e64a252dea3a8789faae9a1acbe60e0d3b |
| meta.json SHA-256 | 9a33a0049faec80db5e1fb11524e38ebbccb0811c60682272d4aefcf6d2af029 |
| schema.unstable.json SHA-256 | 9e1a31a28775e2a4194a60912278d6b34a03f3ab7e95d63aa4bab6e5618390e2 |
| meta.unstable.json SHA-256 | 6e17db561428dda78dc1812a737f8ab529544da282a8302dba876a9908630278 |
| License | Apache-2.0 |

The four schema files in the SDK were compared with the upstream release assets (curl of releases/download/v0.13.5/*): all four hashes are equal. The generator inputs are the stable and the unstable files; the Makefile derives both from schema/version (0.13.5). Ask uses stable only.

## Commands and results

1. Resolve: GOWORK=off go mod download -json github.com/coder/acp-go-sdk@v0.13.5 in a temporary module (exit 0).
2. RED (bare SDK, temporary module, saved test red_test.go): GOWORK=off GOFLAGS=-mod=mod go test ./acp -run TestRED -count=1 -> FAIL, exit 1:

```
--- FAIL: TestREDWriteError (2.00s)
    red_test.go:44: RED: connection still open after lost response write
--- FAIL: TestREDOversizeLine (2.00s)
    red_test.go:53: RED: connection still open after 2 MiB line without newline (cap 1 MiB)
--- FAIL: TestREDMetaPrecision (0.00s)
    red_test.go:62: RED: typed meta lost precision: {"_meta":{"seq":9007199254740992}}
FAIL
FAIL	tmpmod/acp	4.386s
FAIL
```

3. GREEN, same scenarios with the guards in internal/acp/wire.go (temporary module, race): ok, exit 0.
4. In tree after the pin: go test -race ./internal/acp ./pkg/protocol -count=1 -> ok, exit 0. golangci-lint on both packages: 0 issues.
5. Pin: go get github.com/coder/acp-go-sdk@v0.13.5. go.mod gains one require line; go.sum gains two lines. go mod tidy was not run.

## Findings from SDK source (v0.13.5)

| Topic | Evidence | Result |
|---|---|---|
| Extension hook | extensions.go: names that start with "_" go to HandleExtensionMethod(ctx, method, raw params). Unknown notification with -32601 is ignored. | PASS: requests, results, errors, notifications and escaped "\/" names reach the handler (TestACPConformanceAskDispatch) |
| Error data leak | errors.go toReqErr: an unmapped error becomes -32603 with err.Error() in data | Limit: host must map every error (test asserts the leak so the rule stays visible) |
| Write errors | connection.go handleInbound: _ = c.sendMessage(res); sendMessage ignores the byte count | RED, fixed by CheckedWriter (error, short and zero-byte write become a latched failure and stop the connection) |
| Scanner cap | receive(): 1 MiB initial buffer, 10 MiB cap, read side only | RED for an Ask cap below 10 MiB, fixed by LineLimitReader (counted per byte, split-byte test, exact-cap line passes) |
| Request cancel | $/cancel_request handled in the reader, not queued. Numeric 7 and string "abc" cancel with -32800. Number 1 and string "1" differ. Unknown ID, early cancel and late cancel do nothing. | PASS (TestACPConformanceRequestCancellation). Cancelling a request is not session/cancel. |
| session/cancel | agent_gen.go: a notification, so it is queued in order. It also cancels the prompt context of the session. | PASS and documented: a blocked notification handler delays it |
| Same-session prompt | agent_gen.go session/prompt: a second prompt on one session cancels the first prompt context (prev()). | Limit for the host: do not take abort from the prompt context; reject a busy prompt before the SDK context is used |
| Queue pressure | Ordered queue holds 1024; overflow closes the connection with an error | PASS: bounded failure within 5 s |
| Duplicate request IDs | inflight map is overwritten; the first finisher removes the entry of the second | PASS for responses (two frames, no panic). Limit: cancel of the second can be lost |
| Malformed JSON | logged with the raw line, skipped, no -32700 frame | PASS (connection stays up). Limit: set a logger that drops the raw attribute (SetLogger) |
| Usage notification | usage_update appears only in schema.unstable.json | Correction of earlier research: stable schema rejects it. Ask reports usage with _ask/session/usage |
| Typed _meta | map[string]any decodes numbers as float64: 9007199254740993 -> 9007199254740992 | RED. Mitigation: Ask counters are decimal strings (ACPCursor, seq). Extension params and results are raw and exact (tests) |
| Unsupported | session/load, unstable session/set_model and _ask compact/fork/tree answer -32601 | PASS |
| Stable config | session/set_config_option and config_option_update are in the stable schema | PASS (config update validates against schema.json) |
| Agent interface | generated Agent interface includes Unstable* methods | Note: host implements them or embeds a stub that returns method-not-found; none is advertised |

Test map (internal/acp/conformance_test.go): VersionSchema, MetaPrecision, AskDispatch, ConcurrentReverseTraffic, RequestCancellation, InboundPressure, OutboundFault (error, zero-byte, short, latch, blocked), IngressFault (EOF, partial frame, over-cap split bytes, exact cap, malformed, duplicate IDs), StableSchema, NoGoroutineLeak (goleak after EOF and after a released blocked writer).

## Mitigations (owned)

- internal/acp/wire.go: CheckedWriter and LineLimitReader, stdlib only. The caller sets onFail to close the SDK input (io.Pipe CloseWithError in tests). The cap value is the host choice.
- Not done on purpose: no SDK patch, no second JSON-RPC engine, no unstable interface.

## Wire DTO additions (pkg/protocol/acp.go)

compact/fork/tree constants; ACPContentBlock (text and image) with UserBlocks validation; state, models, set-model, thinking, continue, reset and usage results; ACPErrorKind with one code table and ACPErrorData (kind only); CycleID and AttemptID on the event notification. No turn ID exists in the events, so none was invented.

## Unresolved

- A blocked write that never returns cannot be interrupted by CheckedWriter; the host must close the stdout file on shutdown.
- lx-wnk and Caelis not evaluated.
- The final no-result-after-lost-output rule for prompt responses is Phase 3 (sender barrier); this report covers the response-write path only.
