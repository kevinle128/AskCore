# H7a final delta review

Status: DONE.
Score: 9.5/10 for the reviewed implementation and evidence.
Verdict: Approve; code, actor acceptance, live routes, and final quality gates are complete.

## Scope

This review supplements [the first independent review](./reviewer-261006-0823-h7a-implementation.md).
Read the [final CLI proof](./tester-261006-0835-h7a-final-proof.md), [live acceptance](./tester-261006-0835-h7a-live-acceptance.md), updated native readiness test, command restart/signal tests, settings/auth/app flow, and new model catalog row.
The final follow-up also reviews [the exact sync actor](./tester-261006-0855-h7a-fsync-actor.md), `cmd/tui/auth_fsync_test.go`, the external FUSE fixture, its container runner, and test owner documentation.
Reviewed source without changing code or plan status.
No live credential home or secret artifact was read.
No live provider request was made by this reviewer.

## Critical findings

None confirmed.
No new credential leak, automatic billed fallback, replay, tool argument loss, or public signature break was found in the delta.

## Exact acceptance gate closure

### Exact command signal proof during local durable commit

The accepted phase-2 and phase-6 criterion requires a signal while the validated replacement commit is blocked beyond two seconds.
The revised command test at `cmd/tui/auth_signal_test.go:148` holds the external token exchange for 2.2 seconds at line 208.
It now proves that all three signal paths and a second ordinary signal wait beyond the old inference grace.
It also proves that the real replacement commit completes before the expected signal exit code.
That earlier signal occurs during exchange, not during a local replacement write or sync.
The new FUSE actor below supplies the separate exact internal-commit proof.

Separate evidence supports the implementation.
`internal/settings/auth_test.go:625` holds replacement sync for 5.1 seconds through the existing private store seam and checks that the fence remains after the local budget expires.
`internal/auth/service.go:165` registers refresh before entering the store and defers unregistering until the call returns.
`internal/settings/auth.go:506` gives the replacement an independent five-second context.
`cmd/tui/headless.go:334` waits through AuthWait before consulting another signal or the inference grace timer.
These are distinct store, source, and command proofs.
Their combination alone did not close the accepted actor requirement.

The external Linux FUSE fixture and exact compiled command actor now exist.
The controller reports that the final clean Linux race/count-two invocation exited 0 and passed all five cases twice.
The literal acceptance row is now supported by source and complete executed actor evidence.

The fixture in `internal/testsupport/fsfault/fsync-gate.c` passes writes, locks, rename, file sync, and directory sync to the owned backing directory.
ARM follows the initial native login.
The first later regular-file sync publishes the pending fence; the second blocks the validated replacement sync.
At WAIT returning BLOCKED, the actor checks the old durable fence and replacement temporary bytes before sending the signal.
All CLI operations still use the actual mounted FUSE path.
The compiled observer reads only the corresponding backing bytes.
That read avoids Linux mounted-inode serialization behind fsync and does not replace a store operation or alter credential bytes.

The ordinary signal cases wait beyond two seconds after a second ordinary signal, release file sync, and wait for real backing directory sync.
The fixture performs backing fsync before reporting WAIT_SYNC as SYNCED.
It holds the directory-sync reply until RELEASE_EXIT, so the independent byte observation occurs before the CLI can exit.
The actor checks the complete saved replacement against the earlier temporary record, including generation, expiry, scopes, tokens, and absent fence.
The next assertion checks the expected signal exit code.
This is an actual internal-commit barrier at the compiled command boundary, with external HTTP and filesystem control only.
No production hook, endpoint override, identity bypass, or replacement store function is introduced.

The budget case holds replacement file sync beyond five seconds and checks the unchanged committed pending fence after release.
The forced-death case sends SIGKILL during blocked replacement sync.
Both start a new compiled prompt process and assert explicit recovery, byte-for-byte unchanged fence, and no additional auth or inference request.
These assertions passed in the selected Linux matrix and close the previously missing actor scenarios.

Cleanup releases both gates, stops and waits for owned CLI processes, unmounts the owned mount, stops and waits for the fixture, and removes owned temporary data.
The external-fixture mode removes only its case directory and does not stop an externally owned mount process.
The container runner selects /dev/fuse and SYS_ADMIN for mount operations, mounts the repository read-only, and removes its named test container on exit.
It does not request the broad privileged container option.
The dedicated cache volume is intentional and its removal command is documented.
The fixture and fsfault build tag are test-only and fail explicitly when the required Linux setup is absent.
No unresolved critical defect was found in these source paths.
The final corrected Linux race/count-two run passed ten of ten cases in 38.186 seconds.
Its two complete matrices took 18.55 and 18.60 seconds.
Ordinary signal cases held replacement sync for 2.362–2.375 seconds and observed the complete durable replacement before command exit.
The budget case held sync for 5.204 seconds and retained the exact fence; the next command sent no additional HTTP.
The actual SIGKILL case also retained the fence and prevented next-process grant reuse.

## Live route closure

The updated live report records successful Anthropic, ChatGPT, and xAI login, real echo tool turns, terminal settlement, usage, and local logout.
xAI device authorization returned code 0 and saved generation 1 with expiry and no pending fence.
Its ordinary production prompt executed one canonical echo tool and completed two assistant turns with toolUse and stop reasons.
The run had usage, exactly one agent_settled event, and no assistant error.
Local xAI logout returned code 0.
All three provider records were absent from the isolated home after the last logout.
The three live route gates are closed by the sanitized controller evidence.
The exact internal-commit proof is closed separately by the complete Linux matrix above.

## Closed concerns

| Earlier concern | New source and proof | Disposition |
| --- | --- | --- |
| Private input counted only retained bytes | `cmd/tui/auth_input.go:53` counts bytes before filtering; discarded-byte regression passes | Closed. |
| No command death/lost-response/write-failure restart proof | `cmd/tui/auth_signal_test.go:236` starts real native login, faults the rotating request/real filesystem, and runs a second prompt process | Closed for the stated fault cases. |
| Native discovery tested only at helper level | `internal/app/auth_readiness_test.go:24` calls app-composed Agent.SetModel with real native discovery and store replacement/deletion | Closed. |
| Failed readiness might change model/thinking | Readiness test checks prior provider and medium thinking for denied/replaced/deleted responses | Closed. |
| CLI signal wait did not cross old grace | Exchange test holds 2.2 seconds; exact FUSE actor holds actual replacement sync beyond two seconds with a second signal and before-exit durable observation | Closed by complete repeated Linux race run. |
| Refresh errors lost storage classification | `internal/auth/service.go:185` uses errors.Join with ErrRecovery and the store cause | Closed by source. |
| No live subscription acceptance | Updated live report records all three native routes, tool turns, settlement, usage, and local logout | Closed for all three providers. |
| OpenAI README example uses a model hidden from the live account | README now gives explicit Sol selection and states account availability separately from compiled metadata | Closed. |

The replacement-write-failure command case changes only the owned home mode and confirms an OS permission error before releasing the valid token response.
It uses real store I/O rather than an internal write replacement.
The second process checks recovery guidance, unchanged fence bytes, and no additional external request.
The process-killed case uses SIGKILL during an active fenced request.
The lost-response case closes only the external server connection.
The native readiness test replaces or deletes the record before the external discovery response returns.
It verifies that the resolver's post-discovery reread rejects stale state.

## Model catalog delta

`OpenAIGPT56Sol` at `internal/providers/catalog.go:83` uses the existing static catalog and native Responses path.
The row does not introduce a remote model catalog, endpoint override, alternate billing path, or account selector.
The earlier GPT-5.5 default remains unchanged.
The connected signed ChatGPT command test at `cmd/tui/auth_oauth_command_test.go:500` checks that hidden GPT-5.5 reaches no inference and explicitly selected listed Sol completes a real echo turn.

The row's context, output limit, text/image input, supported reasoning levels, ordinary prices, cache-write price, and long-context tier agree with the [official Sol model documentation](https://developers.openai.com/api/docs/models/gpt-5.6-sol).
The existing ThinkingLevelMap convention maps off to none, rejects minimal by clamping, and allows the documented higher levels.
The existing cost tier threshold remains 272,000 input/cache tokens.
No model metadata regression was found.

## Minor observations

The root README now shows `--model gpt-5.6-sol` and explains account availability.
Provider owner documentation distinguishes compiled metadata from account access.
The chosen GPT-5.5 default remains unchanged and no automatic model substitution is implied.
The documentation delegate reports 26 checked links and a passing command-help check.

The existing TestFuncSeesCallStateAndRequest now supplies ToolChoice echo and checks that the script Call receives it at `internal/providers/faux/faux_test.go:154`.
It changes a returned Record's choice and checks that a later Requests copy still has echo at line 170.
This closes the source-level copy and script proof gap without changing faux behavior.
The controller ran `go test -race ./internal/providers/faux -run TestFuncSeesCallStateAndRequest -count=1`; it passed in 1.561 seconds.

## Public contracts and quality

The store fence, same-record replacement, revision guard, actual-expiry policy, independent commit context, and post-commit discovery order remain intact.
Provider.Stream, StreamFn, Stream.Result, and agent event ownership remain unchanged.
Settings/provider/auth import boundaries remain as reviewed.
No protocol schema, database migration, or generated artifact change was introduced by the model delta.
The updated AGENTS and owner documents describe the real native auth package, OIDC dependency, composition, capture isolation, and local host limits.

The reviewer reran the focused native readiness, CLI restart, signal drain, signed ChatGPT model selection, private input, and native refresh-gate checks.
They passed for app, auth, and cmd/tui.
The reviewer separately ran provider catalog/lookup/thinking/cost checks; they passed.
The latest app readiness and provider catalog/thinking/cost checks also passed with the race detector.
The controller reports full go test, vet, and lint passes after the live callback listener closed.
The live report records the same checks and accurately identifies the earlier fixed-port scheduling conflict.
No new build or lint failure was found in the reviewed delta.
The reviewer independently passed `bash -n internal/testsupport/fsfault/run-tests.sh` and `go vet -tags fsfault ./cmd/tui` for the exact actor follow-up.
The first Linux actor run failed because the mounted observer read waited behind the blocked file sync; that result did not prove the matrix.
The corrected backing observer preserves real product I/O and fixes that test cause.
The controller's final corrected Linux `-race -count=2` run passed completely with exit code 0.
An earlier repeat failed when host disk space fell to 123 MiB, and concurrent lint reported no space.
The controller restored 5.9 GiB of free host space by removing reproducible build cache and owned artifacts.
It then removed the damaged owned Linux cache volume and rebuilt before the successful clean run.
That final pass supports closure without attributing the resource failures to a product defect.
The test container was removed and the owned VM was stopped.
The controller's final tagged lint rerun after cleanup exited 0 and reported zero issues.
The earlier disk-space failure is retained as a failed environment run, not counted as a lint pass.
This review started no background process.

## Completion decision

No additional product-scope decision or redesign is needed.
All reviewed actor acceptance gates, including the three live routes and exact internal-commit matrix, have evidence for closure.
Preserve the static Sol catalog decision, original GPT-5.5 default, and external filesystem fixture boundary.
Code review is complete with no unresolved critical defect.
Delivery is a go.
The controller reports completed plan status for all six phases and 22 of 22 criteria, valid root plan validation, 96 valid local document links, and a passing diff check.
The owned test VM was removed and no owned process remained; the original default VM and connection were preserved.
This report does not itself change plan or release status.
