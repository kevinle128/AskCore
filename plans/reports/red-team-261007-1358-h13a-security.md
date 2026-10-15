# H13a security review

Status: DONE_WITH_CONCERNS.
I authored the reviewed draft, so this is an adversarial self-review, not a fully independent review.
A separate live reviewer is preferable for final approval; I did not spawn one.
I read all five plan files and relevant Agent, auth, provider, app, CLI and settings source.
No implementation, SDK conformance, or runtime test was executed.

## Threat model

The ACP editor is a local process with the user's authority over the child.
It can request sessions and cwd values and invoke exposed controls, but must not obtain private native credential material through protocol projections.
There is no listener or remote-client authorization boundary in H13a.
Shared process credentials are host authority; separate Agents isolate session execution and logs, not separate operating-system identities.
Do not invent a cwd sandbox or require a login prompt after the user explicitly chose existing credentials and CLI login.

## Finding

Medium: define the optional `authMethodId` mismatch rule before implementation.
The proposed set-model request accepts optional `authMethodId` in [Phase 1](../261007-0700-h13a-acp-stdio/phase-01-start.md:45).
The plan also promises configured-method validation, no billed fallback, and no interactive login.
Current `auth.Service.Resolve` selects an explicit key override, otherwise the saved provider credential, otherwise the environment key; it does not accept a requested method ID (`internal/auth/service.go:114–145`).
`app.AuthRunner` invokes that resolver for readiness and streaming (`internal/app/module_auth.go:14–33`).
`Agent.SetModel` receives a model only and calls readiness before committing the new model (`internal/agent/agent.go:265–305`).
Thus merely passing the model through those owners does not prove that a requested subscription method matches the actual saved API-key method.

Repair the plan by defining this field as a constraint on existing configured host selection, not permission to create or replace credentials.
Reject a differing or unsupported method explicitly before SetModel and preserve the prior model/thinking state.
Add a built-process case with an API-key credential and a subscription `authMethodId`, plus the reverse mismatch, proving no billed inference request and no credential mutation.
This is a missing adapter contract/test, not a demonstrated source vulnerability or a reason to add interactive auth.
No High or Critical finding was established.

## Forty-claim source verification

SUPPORTED means current source supports an existing prerequisite or the plan clearly assigns new work.
GATE means a deliberately unexecuted SDK/protocol check; it is not a conformance PASS.
The table samples ten claims per phase.

### Phase 1

| # | Claim | Source or plan evidence | Result |
|---|---|---|---|
| 1 | ACP server belongs in internal/acp | internal/acp/README.md:3–11 | SUPPORTED |
| 2 | Wire DTO owner is pkg/protocol | internal/acp/README.md:17 | SUPPORTED |
| 3 | Configuration is constructor-injected | internal/acp/README.md:32–38 | SUPPORTED |
| 4 | Agent API is not exposed as public wire types | phase-01-start.md:59–64 | SUPPORTED planned boundary |
| 5 | AuthSnapshot contains credential material | internal/providers/auth.go:10–19 | SUPPORTED |
| 6 | Snapshot diagnostics omit access/account | internal/providers/auth.go:22–35 | SUPPORTED |
| 7 | Supported SDK/schema identity remains unresolved | researcher-261007-1358-h13a-sdk.md:7–16 | GATE |
| 8 | Metadata precision must be tested | phase-01-start.md:107–108, 121, 132 | GATE |
| 9 | First ingress and writer errors have tests | phase-01-start.md:113–115, 129–131 | SUPPORTED planned tests |
| 10 | Optional authMethodId needs validation rule | phase-01-start.md:45; internal/auth/service.go:114–145 | GAP, Medium above |

### Phase 2

| # | Claim | Source or plan evidence | Result |
|---|---|---|---|
| 1 | Agent.New owns a new memory log by default | internal/agent/agent.go:104–122 | SUPPORTED |
| 2 | Existing headless constructor chooses process cwd | cmd/tui/headless.go:230–233 | SUPPORTED; extraction must change seam |
| 3 | Existing headless constructor creates its own ID | cmd/tui/headless.go:241–257 | SUPPORTED; extraction must inject ID |
| 4 | Busy and disposed Prompt are current contracts | internal/agent/agent.go:126–134 | SUPPORTED |
| 5 | SetModel validates readiness before mutation | internal/agent/agent.go:265–305 | SUPPORTED |
| 6 | Qualified API disambiguates catalog choice | internal/providers/catalog.go:99–122 | SUPPORTED |
| 7 | Reset replaces only that Agent's writer/epoch | internal/agent/agent.go:326–359 | SUPPORTED |
| 8 | New session must create another Agent, not Reset | phase-02-session-adapter.md:63, 74–77 | SUPPORTED planned ownership |
| 9 | Faux starts without native service today | cmd/tui/headless.go:139–147 | SUPPORTED |
| 10 | Real wire registration is initially conditional | cmd/tui/headless.go:234–240 | SUPPORTED; plan calls out faux-to-real repair |

### Phase 3

| # | Claim | Source or plan evidence | Result |
|---|---|---|---|
| 1 | Follow cursor carries epoch and sequence | internal/agent/follow.go:51–56 | SUPPORTED |
| 2 | Baseline carries incomplete raw open blocks | internal/agent/follow.go:58–66 | SUPPORTED |
| 3 | Resumed cursor omits full log snapshot | internal/agent/follow.go:69–84, 137–141 | SUPPORTED |
| 4 | Oversized event can require explicit resync | internal/agent/follow.go:124–129 | SUPPORTED |
| 5 | Snapshot and event cut share publication state | internal/agent/follow.go:131–158 | SUPPORTED |
| 6 | Queue admission returns IDs | internal/agent/queue.go:159–166, 190–200 | SUPPORTED |
| 7 | Remove returns existing bool policy | internal/agent/queue.go:202–212 | SUPPORTED |
| 8 | Incoming watermark is not outbound delivery proof | researcher-261007-1358-h13a-sdk.md:30–38; phase-03-ordered-events-and-control.md:64–67 | SUPPORTED contract distinction |
| 9 | Mandatory prompt observer survives optional unfollow | phase-03-ordered-events-and-control.md:85–92 | SUPPORTED planned ownership |
| 10 | Missing completion sequence fails connection | phase-03-ordered-events-and-control.md:73–81 | SUPPORTED planned failure path |

### Phase 4

| # | Claim | Source or plan evidence | Result |
|---|---|---|---|
| 1 | Auth command is dispatched before parseArgs | cmd/tui/headless.go:70–108 | SUPPORTED registration pattern |
| 2 | Auth command consumes supplied raw stdin | cmd/tui/headless.go:91 | SUPPORTED; forbidden inside ACP |
| 3 | Login requires private callbacks | internal/auth/login.go:12–19, 33–58 | SUPPORTED |
| 4 | NewNativeAuth owns credential store and methods | internal/app/auth_native.go:11–30 | SUPPORTED |
| 5 | Native auth HTTP is separate from inference | internal/app/auth_native.go:9–10; cmd/tui/headless.go:132–141 | SUPPORTED |
| 6 | AuthWait has independent 20-second bound | internal/app/module_auth.go:52–57 | SUPPORTED |
| 7 | BindAuth installs readiness and stream together | internal/app/module_auth.go:69–75 | SUPPORTED |
| 8 | Saved credential read validates private file mode | internal/settings/auth.go:138–152, 313–327 | SUPPORTED |
| 9 | Existing headless signal grace is separate | cmd/tui/headless.go:329–357; phase-04-stdio-auth-and-e2e.md shutdown contract | SUPPORTED; plan does not copy it for session/cancel |
| 10 | Built process owns stdin/stdout and no listener | phase-04-stdio-auth-and-e2e.md dependency/scenario sections | SUPPORTED planned E2E requirement |

## Evidence limits

SDK metadata precision, bounded reader behavior, concurrent control handling, writer-error propagation, and blocked-I/O shutdown remain executable Phase 1 gates.
The plan correctly keeps D17 open and does not claim blanket ACP conformance.
The review found no reason to reverse ACP v1, per-session Agent ownership, or host-side CLI login.
Unresolved questions: none beyond the adapter contract repair above and the stated executable gates.
