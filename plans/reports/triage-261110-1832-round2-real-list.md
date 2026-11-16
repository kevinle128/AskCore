N1 m1-providers.md  F3 | fantasykit/fold.go:207 | `blocks` counts text/tool blocks that only had a start event, so an empty reply passes as success | Medium 
N2 m1-providers.md  F5 | model_config.go:364 | `input: []` records a problem though the patch contract says empty is a no-op | Low 
N3 m1-providers.md  F6 | openai/profiles.go:154 | Completions guard fails with a base URL that ends in `/` | Low 
N4 m1-providers.md  F7 | openai/profiles.go:58 | Same trailing-slash failure for Responses guard | Low 
N5 m1-providers.md  F10 | anthropic/prepare.go:61 | `RuneCount/4` token estimate undercounts CJK, so `max_tokens` can overshoot the window | Low 
N6 m1-providers.md  F14 | anthropic/profile.go:62-66 | Wrapper replaces `req.Body` without closing the original | Low 
N7 m1-providers.md  F15 | openai/completions_body.go:100-102 | `SupportsReasoningEffort` is never read | Low 
N8 m1-providers.md  F18 | openai/completions.go:215-219 | `omitStreamOptionsRT` never closes original `req.Body` | Low 
N9 m1-providers.md  F19 | partialjson/partialjson.go:260-262 | Colon is skipped without a check | Low 
N10 m1-providers.md  F24 | normalize.go:74-79 | Same model id on a different native provider keeps the item id | Low 
N11 m1-providers.md  F25 | replay.go:57-63 | `DecodeThinkingSignature` accepts `null`/`{}` | Low 
N12 m2-leader-acp-app.md  F4 | internal/acp/stdio.go:278-279 | Pending-response set is keyed by ID; duplicate IDs release the barrier early. | Low 
N13 m2-leader-acp-app.md  F5 | internal/acp/host.go:500-507 | `mutationCommit` ignores the failure latch. | Low 
N14 m2-leader-acp-app.md  F10 | internal/app/module_leader.go:96-101 | Adapter-creation error path does not call `acpRT.Cleanup`. | Low 
N15 m2-leader-acp-app.md  F11 | internal/app/module_leader.go:121-125 | `NewServer` error path does not call `acpRT.Cleanup`. | Low 
N16 m2-leader-acp-app.md  F12 | internal/app/session_start.go:344-345 | Row budget ignores the `Warning: skills: ` prefix, so a line is over 256 bytes. | Low 
N17 m2-leader-acp-app.md  F13 | internal/app/session_start.go:363-368 | Prefix test labels a project context file below the Ask home as `user`. | Low 
N18 m3-agent-pipeline.md  F3 | agent/agent.go:446 | SetThinkingLevel lacks disposed check | Low 
N19 m3-agent-pipeline.md  F5 | agent/agent.go:367 | disposed check should precede validation/key work | Low 
N20 m3-agent-pipeline.md  F14 | tasks/registry.go:133 | NextID called before closed check burns a task number | Low 
N21 m4-tools-settings-auth.md  F1 | auth/broker.go:623 | `Logout` accepts an unknown method when no credential is saved | Low 
N22 m4-tools-settings-auth.md  F9 | settings/auth.go:549 | `CommitRefresh` does not check that the replacement is OAuth | Low 
N23 m4-tools-settings-auth.md  F14 | settings/host-id.go:16 | `HostID` on a reader store writes files | Low 
N24 m4-tools-settings-auth.md  F17 | skills/loader_listing.go:155 | `descBytes` ignores `Quote` escaping | Low 
N25 m4-tools-settings-auth.md  F21 | tools/file_image.go:245 | Image decoded with no dimension check (decompression bomb) | Low 
N26 m4-tools-settings-auth.md  F22 | tools/coerce.go:196 | Numeric string goes through `float64`, losing integers above 2^53 | Low 
N27 m4-tools-settings-auth.md  F25 | tools/shell_bash.go:300 | `TruncatedBy` inferred from retained line count | Low 
N28 m5m6-tui-cmd-e2e.md  B4 | e2e/tui/real_provider.py:176-181 | `save` is unused and ignores `tail` | Low 
N29 m5m6-tui-cmd-e2e.md  B5 | cmd/tui/connect.go:221, 568-570 | `initialize` and `session/new` use `context.Background()` and ignore signals | Low 
N30 m5m6-tui-cmd-e2e.md  B7 | cmd/tui/capture.go:31, headless.go:163 | `ASK_CAPTURE` is ignored when only `--model` selects the provider | Low 
N31 m5m6-tui-cmd-e2e.md  B11 | cmd/tui/leader.go:150-153 | `leader status --json` ignores marshal and write errors | Low 
N32 m5m6-tui-cmd-e2e.md  B12 | cmd/tui/leader.go:179-182 | Same for `leader list --json` | Low 
N33 m5m6-tui-cmd-e2e.md  B16 | cmd/tui/version.go:30 | `acpWireVersion` duplicates `internal/acp.wireVersion` | Low 
N34 m5m6-tui-cmd-e2e.md  B18 | cmd/tui/connect.go:636-642 | `finish` exits 0 when the leader closes after stdin EOF | Low 
N35 m5m6-tui-cmd-e2e.md  B19 | cmd/tui/headless.go:264 | `printStartModel` parses `ASK_FAUX_TPS` for real-provider runs | Low 
