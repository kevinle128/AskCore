---
status: accepted
---

# Deliver minimal login with subscription access

Native subscription support must include a way for the user to acquire and remove credentials.
Deliver shared auth flows and minimal headless login/logout commands with the provider work, while keeping full TUI dialogs for later.
This changes the previous decision to defer all public login commands, so subscription access can be used when the provider work is delivered.
Only the supporting credential/settings work moves with it; broader settings and UI work retain their own scope.

Login runs on the inference host and saves credentials in that runtime's Ask home.
Remote use runs the login command on the remote host, with provider-native interaction or documented callback forwarding.
Local-client credential transfer to a remote leader is outside this phase.

Scheduling update (2026-10-06): the user requested that H4 stay limited to wire/replay work.
Deliver native auth and minimal headless login/logout in H7a after H7 catalog/credential persistence.
This changes the earlier H4 timing, not the decision to deliver login with subscription inference.
Full TUI dialogs remain later.

Priority update (2026-10-06): Alibaba Token Plan expires soon, so advance H7a after H3 alongside required Responses wire work.
Pull shared credential persistence into H7a; full catalog, builtin tools and project settings are not delivery gates.
Deliver each subscription as it becomes ready and complete the phase when all three supported flows work.
