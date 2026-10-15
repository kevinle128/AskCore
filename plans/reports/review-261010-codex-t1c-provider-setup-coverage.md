# Provider setup coverage and docs audit

This is a code and assertion audit of all 60 approved criteria.
The source authority is [the approved design](../261010-1424-t1c-provider-setup-design/design.md).
No real-provider script was executed.
The short report is [the review report](review-261010-codex-t1c-provider-setup.md).
Adequate means that the assertions cover the criterion at the proper unit or product seam; it does not mean that this reviewer ran the Go suite.
All original named tests and literal subtests existed before the excluded glossary renames.
Two mapped names have since moved; the rows identify their equivalent tests without changing the rename work.
The original map has 46 adequate rows and 14 partial rows.
The current assertions cover 54 rows; rows 5, 12, 18, 34, 39 and 51 have the limits below.

| # | Original map | Current assertions | Evidence and limits |
|---|---|---|---|
| 1 | Exists; Adequate | Adequate | Golden asserts exact rail, footer suffix and no context bar; built-PTY start confirms no provider request; see `TestFirstRunGoldens/no_model_80x24` (internal/tui/first_run_test.go:26); `TestInteractiveFirstRunNoModel` (cmd/tui/providers_flow_test.go:357). |
| 2 | Exists; Adequate | Adequate | 40x14 golden asserts one rail row and footer through real Bubble Tea rendering with controlled client; see `TestFirstRunGoldens/no_model_40x14` (internal/tui/first_run_test.go:26). |
| 3 | Exists; Adequate | Adequate | Actual text/enter preserves draft; zero prompt call and golden excludes committed prompt band; real fixture count stays zero; see `TestNoModelSendRefusedKeepsDraft` (internal/tui/first_run_test.go:45); `TestInteractiveFirstPromptNotSent` (cmd/tui/providers_flow_test.go:370). |
| 4 | Exists; Adequate | Adequate | Actual /p completion and uppercase /PROVIDERS enter through input handler; see `TestCompletionListsProviders` (internal/tui/providers_box_test.go:13); `TestProvidersCommandCaseInsensitive` (internal/tui/providers_box_test.go:20). |
| 5 | Exists; Partial | Partial | Exact seven fixture rows, suffixes, last search row and hint; esc restores only empty editor, not a nonempty public-entry draft; see `TestProviderBoxGoldens/list_80x24` (internal/tui/providers_box_test.go:29); `TestInteractiveProvidersList` (cmd/tui/providers_flow_test.go:406). |
| 6 | Exists; Adequate | Adequate | Actual search input; four matching key names, first cursor and search row; see `TestProviderBoxSearchFilters` (internal/tui/providers_box_test.go:75). |
| 7 | Exists; Adequate | Adequate | Exact narrow rows, ellipsis, more row and whole env/signed-in suffixes; see `TestProviderBoxGoldens/narrow_40x14` (internal/tui/providers_box_test.go:29). |
| 8 | Exists; Adequate | Adequate | Actual signed-in row menu and escape; unsigned key entry opens directly without a login-start call; see `TestProviderRowMenu` (internal/tui/providers_box_test.go:101); `TestProviderEnterStartsFlowAtOnce` (internal/tui/providers_box_test.go:113). |
| 9 | Exists; Adequate | Adequate | Bracketed paste and raw/grid fake-key absence; fixed (hidden) text appears; see `TestKeyBoxPasteShowsHidden` (internal/tui/secret_input_test.go:46). |
| 10 | Exists; Adequate | Adequate | Controlled key check stalls; exact rail, typing lock and one key call; see `TestKeyBoxCheckingLocksInput` (internal/tui/secret_input_test.go:60). |
| 11 | Exists; Adequate | Adequate | Actual fixture refusal and broker transport; status/code only, empty input and raw/grid secrecy; see `TestKeyBoxRefusedClearsInput` (internal/tui/secret_input_test.go:85); `TestBrokerKeyRefusalCarriesCodeOnly` (internal/auth/broker_test.go:186); `TestInteractiveAPIKeyRefused` (cmd/tui/providers_flow_test.go:458). |
| 12 | Exists; Partial | Partial | Actual paste/submit and fixture store; exact save note, rail and close, but draft assertion is only empty editor; see `TestKeyBoxSavedNoteAndRail` (internal/tui/secret_input_test.go:111); `TestInteractiveAPIKeySaved` (cmd/tui/providers_flow_test.go:491). |
| 13 | Exists; Adequate | Adequate | Actual browser row entry; whole wrapped URL, waiting rail and copy/cancel hint; see `TestBrowserBoxGoldens/waiting_80x24` (internal/tui/login_box_test.go:46). |
| 14 | Exists; Adequate | Adequate | Exact four capped URL rows and full-URL copy hint at 40x14; golden owns spacing; see `TestBrowserBoxGoldens/narrow_40x14` (internal/tui/login_box_test.go:46). |
| 15 | Exists; Adequate | Adequate | Browser denial and enter retry increment start count; broker runs production OAuth flow against fixture; see `TestLoginFailedTryAgain` (internal/tui/login_box_test.go:108); `TestBrokerBrowserDenied` (internal/auth/broker_test.go:340). |
| 16 | Exists; Adequate | Adequate | Two real clients compete; broker and UI assert provider busy; see `TestBrokerOneLoginPerProvider` (internal/auth/broker_test.go:223); `TestLoginCannotStartBusy` (internal/tui/login_box_test.go:140); `TestInteractiveProviderBusyTwoClients` (cmd/tui/providers_flow_test.go:668). |
| 17 | Exists; Adequate | Adequate | Real leader loss and client release; reconnect does not restart sign-in and start count stays one; see `TestLoginBoxLeaderLost` (internal/tui/login_box_test.go:160); `TestRouterReleasesLoginsOfGoneClient` (internal/leader/router_provider_setup_test.go:80); `TestInteractiveLoginLeaderLost` (cmd/tui/providers_flow_test.go:690). |
| 18 | Exists; Partial | Partial | Actual escape/cancel and late-success suppression, plus broker commit ordering; restored draft is only empty; see `TestLoginCancelNoteIgnoresLateResult` (internal/tui/login_box_test.go:181); `TestEscDuringKeyCheckNoteFollowsCancelAnswer` (internal/tui/login_box_test.go:201); `TestBrokerCancelDropsLateResult` (internal/auth/broker_test.go:273); `TestBrokerKeyCancelBeforeCommitSavesNothing` (internal/auth/broker_test.go:609). |
| 19 | Exists; Adequate | Adequate | Actual ctrl+c cancels box; second press opens quit confirmation and Bubble Tea remains alive; see `TestLoginCtrlCCancelsAndDoesNotQuit` (internal/tui/login_box_test.go:239). |
| 20 | Exists; Adequate | Adequate | OAuth fixture account response reaches note with account name and ready-model count; see `TestLoginSuccessNamesAccount` (internal/tui/login_box_test.go:259); `TestInteractiveOAuthSignedInNamesAccount` (cmd/tui/providers_flow_test.go:628). |
| 21 | Exists; Adequate | Adequate | Actual masked callback-address paste/submit plus browser-address broker fixture; checking text and raw absence; see `TestAnthropicAddressAndCodeBoxes` (internal/tui/login_box_test.go:273); `TestAddressBoxPasteShowsHidden` (internal/tui/login_box_test.go:319); `TestBrokerAnthropicBrowserAcceptsPastedAddress` (internal/auth/broker_test.go:358). |
| 22 | Exists; Adequate | Adequate | Busy callback fixture selects copy-code; masked paste and broker code input; no visible code; see `TestAnthropicAddressAndCodeBoxes` (internal/tui/login_box_test.go:273); `TestCodeBoxPasteShowsHidden` (internal/tui/login_box_test.go:333); `TestBrokerAnthropicFallsBackToCopyCode` (internal/auth/broker_test.go:425); `TestBrokerCopyCodeInput` (internal/auth/broker_test.go:442). |
| 23 | Exists; Adequate | Adequate | Real two-client removal; broker reason, host/client result and exact cancelled note; see `TestLoginCancelledByRemovalShowsReason` (internal/tui/login_box_test.go:347); `TestBrokerLogoutCancelsPendingSignIn` (internal/auth/broker_test.go:706); `TestHostLogoutCancelsOtherClientSignIn` (internal/app/leader_routing_test.go:726); `TestClientWaitLoginCancelledByRemoval` (cmd/tui/tui_client_e2e_test.go:770); `TestInteractiveRemovalCancelsOtherClientSignIn` (cmd/tui/providers_flow_test.go:828). |
| 24 | Exists; Adequate | Adequate | Device box golden page/code/expiry; expired broker outcome and enter starts another flow; see `TestDeviceBoxGoldens` (internal/tui/login_box_test.go:374); `TestBrokerDeviceExpired` (internal/auth/broker_test.go:474). |
| 25 | Exists; Adequate | Adequate | Delete confirmation; esc makes zero logout calls; enter removes saved credential in built PTY; see `TestRemoveConfirmFlow` (internal/tui/providers_box_test.go:148); `TestInteractiveRemoveProvider` (cmd/tui/providers_flow_test.go:750). |
| 26 | Exists; Adequate | Adequate; mapped name stale | UI note plus real Accounts service env usability; named test moved during excluded glossary work; see `TestRemoveNoteEnvStillSet` (internal/tui/providers_box_test.go:180); `TestAccountsLogoutReportsEnv` -> TestProviderSetupLogoutReportsEnv (internal/app/provider_setup_test.go:72). |
| 27 | Exists; Adequate | Adequate | Actual env-only delete leaves list open and sends no logout call; see `TestRemoveEnvOnlyRowSendsNothing` (internal/tui/providers_box_test.go:203). |
| 28 | Exists; Partial | Adequate | Actual leader/agent clear and prompt refusal; Original all-providers-gone test expected Add a provider; repaired test now asserts the exact P22/P23 /model rail and refusal; see `TestCurrentModelRemovedShowsNoModel` (internal/tui/first_run_test.go:126); `TestHostLogoutClearsUnusableModels` (internal/acp/provider_methods_test.go:423); `TestLogoutReportsClearedModelOfCallerSession` (internal/acp/provider_methods_test.go:374); `TestInteractiveRemoveCurrentProvider` (cmd/tui/providers_flow_test.go:787). |
| 29 | Exists; Adequate | Adequate | Actual in-process /providers shows shell hint; client account calls return ErrNeedsLeader without a pipe request; see `TestProvidersNeedsLeaderInProcess` (internal/tui/first_run_test.go:110); `TestClientProvidersNeedLeaderInProcess` (internal/tuiclient/providers_test.go:225). |
| 30 | Exists; Adequate | Adequate | Production host usableOnly filter and built-PTY sign-in/model list; golden asserts groups/hidden unusable rows; see `TestModelSelectorGroupsUsableProviders` (internal/tui/login_box_test.go:587); `TestModelsUsableOnly` (internal/acp/provider_methods_test.go:297); `TestInteractiveModelListAfterSignIn` (cmd/tui/providers_flow_test.go:646). |
| 31 | Exists; Adequate | Adequate | Actual model shortcut, empty hint and no filter row; see `TestModelSelectorEmptyHint` (internal/tui/login_box_test.go:624). |
| 32 | Exists; Adequate | Adequate | Real two-client broadcast and UI open-box update preserve filter/selection and change suffix/rail; see `TestProvidersChangedUpdatesOpenBox` (internal/tui/providers_box_test.go:214); `TestRouterBroadcastsProvidersChanged` (internal/leader/router_provider_setup_test.go:49); `TestInteractiveProvidersChangedSecondClient` (cmd/tui/providers_flow_test.go:950). |
| 33 | Exists; Adequate | Adequate | UI stale-result handlers are called after stopping renderer; dialog/list/transcript invariants and newer-list positive control pass; Client script checks stale generation; no delayed-wire E2E; see `TestStaleProviderResultsIgnored` (internal/tui/providers_box_test.go:325); `TestClientDropsStaleProvidersChanged` (internal/tuiclient/providers_test.go:286). |
| 34 | Exists; Partial | Partial | Live box history keys do not recall earlier prompt; esc restores only empty editor, not nonempty command-entry draft; see `TestKeyBoxIgnoresHistoryKeepsDraft` (internal/tui/secret_input_test.go:146). |
| 35 | Exists; Partial | Adequate | Original map lacks pasted hidden input; Added address/key resize tests send unchanged secret once after resize; original URL/rail/one-start proof remains; see `TestLoginBoxResizeKeepsLogin` (internal/tui/login_box_test.go:402); `TestAddressBufferSurvivesResizeAndIsSentOnce` (internal/tui/login_box_test.go:447); `TestKeyBufferSurvivesResizeAndIsSentOnce` (internal/tui/secret_input_test.go:335); `TestInteractiveLoginResizeKeepsLogin` (cmd/tui/providers_flow_test.go:718). |
| 36 | Exists; Adequate | Adequate | Raw/grid scans cover checking/refused/saved/resize; built-PTY secret excluded from output recording; see `TestSecretNeverOnScreenGrid` (internal/tui/secret_input_test.go:170); `TestInteractiveSecretNeverOnScreen` (cmd/tui/providers_security_test.go:97). |
| 37 | Exists; Adequate | Adequate | Actual refusal/save then committed/tail blocks, notices and PTY transcript scans; see `TestSecretNotInTranscriptBlocks` (internal/tui/secret_input_test.go:224); `TestInteractiveSecretNotInTranscript` (cmd/tui/providers_security_test.go:136). |
| 38 | Exists; Partial | Adequate | Original unit test calls history.older after stopping UI; The mapped built-PTY test also sends actual Up after save, /new and resize, then scans screen and transcript for the key; see `TestSecretNotInPromptHistory` (internal/tui/secret_input_test.go:242); `TestInteractiveSecretNotInTranscript` (cmd/tui/providers_security_test.go:136). |
| 39 | Exists; Partial | Partial | Original map scans rings only; New helper seeds draft/undo state and calls box handlers plus ctrl+- directly; public nonempty-draft entry remains unproved; see `TestSecretNotInUndoOrKillRing` (internal/tui/secret_input_test.go:253); `TestSecretFlowKeepsDraftAndUndo` (internal/tui/secret_input_test.go:355). |
| 40 | Exists; Partial | Adequate | Original test has standalone zeroing, esc/ctrl+c/clear/refusal/signal; nil-dialog signal can pass; New handler test checks former byte slices after close/save/refuse/disconnect; see `TestSecretBufferClearedOnEveryExit` (internal/tui/secret_input_test.go:269); `TestSecretExitOverwritesFormerBuffer` (internal/tui/secret_input_test.go:383). |
| 41 | Exists; Partial | Adequate | Original host code test uses unknown login id; Key E2E scans frames/logs/store and counts once; successful code-flow secrecy/count was originally missing and is now covered by TestInteractiveCopyCodeNotInLogsOrEvents (cmd/tui/providers_security_test.go:233), which checks one input frame, one exchange and code/token absence in output, frames, watcher and files; see `TestHostNeverLogsSecrets` (internal/app/leader_routing_test.go:760); `TestInteractiveSecretNotInLogsOrEvents` (cmd/tui/providers_security_test.go:170). |
| 42 | Exists; Partial | Adequate | Original map covers clipboard only; Existing raw-output test now mapped: no title OSC0/1/2 and no bell; output leaks remain scan-visible; see `TestClipboardCopiesOnlyURLOrCode` (internal/tui/secret_input_test.go:432); `TestNoTitleOrBellInOutput` (internal/tui/secret_input_test.go:493). |
| 43 | Exists; Adequate | Adequate | Live multiline/interior-space/over-limit pastes are refused without paste text or key call; see `TestSecretPasteRefusedWithoutEcho` (internal/tui/secret_input_test.go:409). |
| 44 | Exists; Adequate | Adequate | Golden asserts start line directly after header and bare footer; real leader PTY start also covered; see `TestModelLineGoldens/start_80x24` (internal/tui/model_line_test.go:19); `TestInteractiveStartLineShowsPair` (cmd/tui/providers_flow_test.go:589). |
| 45 | Exists; Adequate | Adequate | 120x40 golden asserts same fixed start-line position; see `TestModelLineGoldens/start_120x40` (internal/tui/model_line_test.go:19). |
| 46 | Exists; Adequate | Adequate | 40x14 golden asserts uncut pair without label; see `TestModelLineGoldens/start_40x14` (internal/tui/model_line_test.go:19). |
| 47 | Exists; Adequate | Adequate | Allowed D13 fixtures: suffix same row at 80, own row at 40; see `TestModelLineGoldens/two_formats_80x24` (internal/tui/model_line_test.go:19). |
| 48 | Exists; Adequate | Adequate | Actual selection, agent production event and PTY switch; change line and bare footer; see `TestModelChangeLine` (internal/tui/model_line_test.go:60); `TestAgentSetModelPublishesModelUpdate` (internal/agent/model_update_test.go:44); `TestInteractiveModelSwitch` (cmd/tui/interactive_flow_test.go:365). |
| 49 | Exists; Adequate | Adequate | Actual selector/attach/follow; divider then line then history, plus built-PTY observer attach; see `TestObserverAttachStartLine` (internal/tui/model_line_test.go:99); `TestInteractiveSessionsAttach` (cmd/tui/interactive_flow_test.go:724). |
| 50 | Exists; Adequate | Adequate | Actual connection/restore messages through Bubble Tea; golden model line follows note; Additional restore tests prove once-only line/replay suppression; see `TestReconnectStatesGoldens/restored` (internal/tui/session_test.go:567). |
| 51 | Exists; Partial | Partial against literal approved text | Original map proves first-run absence only; Added live clear test proves no new model line, no change suffix, the no-model footer and persistent notice through resize and typing, but requires the prior start-line count to stay 1; The approved P22 sketch (design.md:1281) keeps that prior line; criterion 51 (design.md:2095) requires no model line on screen after removal; The code keeps the approved sketch; the criterion remains in conflict; see `TestNoModelHasNoModelLine` (internal/tui/model_line_test.go:165); `TestLiveModelClearKeepsRemovalNoticeWithoutLine` (internal/tui/model_line_test.go:173). |
| 52 | Exists; Adequate | Adequate | Old epoch and sequence-at-cut enter live render; screen equality proves no row; Other-session event test also exists; see `TestStaleModelEventAddsNoRow` (internal/tui/model_line_test.go:196). |
| 53 | Exists; Partial | Adequate | Original PTY test restarted ask; now /new produces second start line and one extra live leader session; Store/router tests check saved choice and restart; see `TestDefaultModelSavedOnSelect` (internal/app/leader_routing_test.go:815); `TestModelSelectShowsDefaultSavedStatus` (internal/tui/first_run_test.go:281); `TestSessionStartUsesSavedDefault` (internal/app/default_model_test.go:47); `TestInteractiveDefaultModelSurvivesNewSession` (cmd/tui/providers_flow_test.go:876); `TestLeaderDefaultModelSurvivesRestart` (cmd/tui/leader_provider_setup_e2e_test.go:159). |
| 54 | Exists; Adequate | Adequate; mapped name stale | Host fallback reason plus 80/40 goldens and real PTY removed-default fallback; Provider-default test moved during excluded glossary work; see `TestSessionStartFallsBackWhenDefaultUnusable` (internal/app/default_model_test.go:54); `TestSessionStartUsesProviderDefault` -> TestSessionStartUsesRecommendedModel (internal/app/default_model_test.go:70); `TestFallbackNoticeNamesModelInUse` (internal/tui/first_run_test.go:257); `TestInteractiveFallbackNoticeWhenDefaultRemoved` (cmd/tui/providers_flow_test.go:922). |
| 55 | Exists; Adequate | Adequate | Broker sign-in keeps stored default; live UI makes no model selection and current line stays; see `TestDefaultNotSetBySignIn` (internal/app/leader_routing_test.go:867); `TestProvidersSignInDoesNotChangeDefault` (internal/tui/first_run_test.go:349). |
| 56 | Exists; Adequate | Adequate | Host reports saved default; live selector tags default/current and shows one tag when equal; see `TestModelsReportsSavedDefault` (internal/app/leader_routing_test.go:879); `TestModelSelectorTagsDefault` (internal/tui/first_run_test.go:315). |
| 57 | Exists; Adequate | Adequate | Exact start/key/code warning rows and file name; built-PTY TEA_TRACE; Accepted trace exception unchanged; see `TestTraceWarningOnceAtStart` (internal/tui/first_run_test.go:184); `TestTraceWarningOnKeyBox` (internal/tui/first_run_test.go:206); `TestTraceWarningOnCodeInput` (internal/tui/first_run_test.go:225); `TestInteractiveTraceWarningShown` (cmd/tui/providers_security_test.go:306). |
| 58 | Exists; Adequate | Adequate | Unset variable live render and PTY contain no TEA_TRACE row; see `TestNoTraceWarningWhenUnset` (internal/tui/first_run_test.go:247); `TestInteractiveNoTraceWarningWhenUnset` (cmd/tui/providers_security_test.go:336). |
| 59 | Exists; Adequate | Adequate | Actual unchecked save; fixture gets zero requests, no Checking row, exact note and api-key suffix after re-open; see `TestAlibabaKeyBoxSaysNotChecked` (internal/tui/login_box_test.go:539); `TestKeySavedNotCheckedNote` (internal/tui/login_box_test.go:554); `TestInteractiveTokenPlanKeySavedUnchecked` (cmd/tui/providers_flow_test.go:524). |
| 60 | Exists; Partial | Adequate | Original PTY test preloaded key via CLI; now real /providers save, model choice and first prompt prove zero save requests and 401 auth block/hint; see `TestUncheckedKeyRefusedAtFirstPrompt` (internal/tui/first_run_test.go:364); `TestInteractiveTokenPlanBadKeyAtFirstPrompt` (cmd/tui/providers_flow_test.go:559). |

## Findings

- Medium: criteria 5, 12, 18, 34 and 39 do not prove a nonempty draft through public /providers entry.
  The new undo helper proves internal box preservation by assigning editor state directly.
- Fixed mapping: criterion 38 now includes the existing built-PTY test that sends Up in the live empty editor.
- Medium, fixed: criterion 41 originally submitted code to an unknown login id; the successful copy-code E2E now covers scan and one delivery/exchange.
  The controller reports 20 focused runs passed.
  A separate combined run had failures in other tests, so this is focused evidence only.
- Medium: criterion 51 has a literal acceptance gap.
  The new live-clear test proves no new line and a stable notice, but preserves the prior start line.
  The approved P22 sketch at design.md:1281 keeps that line; criterion 51 at design.md:2095 says no model line on screen after removal.
  The code preserves the approved sketch, and the conflict remains open.
- Medium, fixed: criterion 28 all-providers-gone rail differed from the approved text; the test now asserts the P22/P23 /model hint.
  The approved design stayed unchanged.
- Excluded glossary inconsistencies: criterion 26 names TestAccountsLogoutReportsEnv; criterion 54 names TestSessionStartUsesProviderDefault.
  Their current equivalent names are listed in the table.
- Medium, fixed: Session.call stored typed fake keys in recording stdout in commands.json.
  Failing control reproduced it before the fix; input events are hidden and output leaks stay visible to scan_secrets.
- Medium, fixed: ask_providers inspected the loading screen.
  Real Ghostty failed with an empty provider row list; wait now requires all names.
- High, fixed: the T1a template rebuild removed accepted decisions beyond section 12.
  Restored not_driver, retry 1/5, no watcher count, login hint, entitlement error and case-insensitive commands in the source, then regenerated design/preview through build-design.py.
- Medium, fixed: phase 8, plan and task said complete while a new OAuth sign-in was not run.
  The plan/phase now say in_progress, and the phase 8 task checkbox is open.
- Fixed docs: client host release, provider file owner, terminal provider scenarios, input-channel lifetime, criterion count, generator and scan references.

## Real-provider runner risks (read only)

The controller fixed the reviewed script risks with fake safety controls; no real-provider scenario was run.

- Fixed: each run now owns unique homes and refuses resets outside them.
- Fixed: cleanup closes sessions, reaps child processes and stops owned leaders before scanning and deleting homes.
- Fixed: the OAuth source copies only auth.json/settings.json; current token fields are reread for the scan.
- Fixed: every scenario entry point calls cleanup and the artifact scan.
- Fixed: unreadable current stores record a failed check but do not skip scanning registered values.
- Acceptance gap remains: the recorded run uses existing OAuth credentials; a new manual OAuth sign-in is not run.

The controller reports 3 fake safety controls passed; this reviewer read those controls and the script changes without running a real script.

## Verification

- Failing-first recording control failed before fix.
- python3 e2e/tui/test-assertions.py: 17 tests pass after recording and loading-wait fixes.
- Initial full terminal run: sandbox exit 1; tui-test lifecycle lock under ~/.tui-test and ps were denied.
- Approved unrestricted full run before loading fix: 18 T0 cases pass; 7 of 8 Ask cases pass; Ghostty provider case fails on loading state.
  Boundary error also records concurrent product edits.
  This run does not certify a stable final tree.
- Earlier stable full terminal run: exit 0; all 26 cases passed (18 T0 and 8 Ask), including provider setup on Alacritty and Ghostty.
  Artifacts: /private/tmp/ask-provider-review-e2e-final; archive/baseline/product hashes are unchanged, no secret or boundary error, and all 8 leader stops are clean.
- Final terminal rerun through the environment-stripping wrapper: exit 0; all 26 cases pass on Alacritty and Ghostty.
  Artifacts: /private/tmp/ask-provider-review-e2e-complete; archive/baseline/product hashes stay unchanged, no secret or boundary error, and all eight leader stops are clean.
  Its sandbox attempt failed on lifecycle locks and process inspection before sessions started; those artifacts are in /private/tmp/ask-provider-review-e2e-complete-sandbox.
  The assertion runner through the same wrapper passes 17 tests.
  No owned exec session remains active.
- Local Markdown targets: 52 files checked; zero missing targets.
  Local anchors: 29 checked; zero unresolved anchors.
  Scoped whitespace check passed for the edited docs, T1c plans, task record and terminal runner.
  The full whitespace check reports 23 fixed-width padding rows in the generated T1a sketch; no generated padding was changed manually.
- T1a generator ran with PYTHONDONTWRITEBYTECODE=1; width check reported zero bad lines.

No approved design, Go production/test, go.mod/sum, tracked pyc, commit, push or real-provider call was changed or executed by this reviewer.
No process from another owner was stopped.
