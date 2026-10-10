#!/usr/bin/env python3
"""Real-provider scenarios for the built `ask` (see real_provider.py).

Usage: real_provider_scenarios.py <scenario> [cols rows]

Scenarios `providers`, `oauth` and `acceptance` (both in order, then the
artifact scan) are the provider-setup acceptance run. The others are the older
single-purpose runs; they start in an empty home, so they first save the key
through the same paste path (`sign_in_key`).

Homes live under /private/tmp (a real directory; /tmp is a symlink on macOS and
the leader refuses it). The OAuth home is a copy of $HOME/.ask-t1a-oauth and is
deleted at the end of every run, passed or failed.

Secret handling, in this order: (1) read the credential values of the
environment, (2) build the scan set from them, (3) the child gets none of the
credential variables (`real_provider.child_env`), (4) start `ask`, (5) paste the
key through the PTY descriptor, (6) scan the output directory and the leader
logs. A value is never printed, logged or written; only names and counts are.
"""
import json
import os
import re
import shutil
import sys
import tempfile
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from real_provider import Session, KEYS, leader_stop, OUT, ensure_ask, runner  # noqa: E402

RUN_HOME = tempfile.mkdtemp(prefix="ask-provider-run-", dir="/private/tmp")
KEY_HOME = os.path.join(RUN_HOME, "key")
OAUTH_HOME = os.path.join(RUN_HOME, "oauth")
OAUTH_SOURCE = os.path.expanduser("~/.ask-t1a-oauth")
# Read once, before any child starts: the child environment has no credential.
REAL_KEY = os.environ.get("OPENAI_API_KEY", "")
FAKE_KEY = "sk-made-up-" + "0123456789abcdef" * 2
SCAN_SET = {**runner.secret_values(os.environ), "RP_FAKE_KEY": FAKE_KEY}
PASTE_BEGIN, PASTE_END = "\x1b[200~", "\x1b[201~"
DELETE = "\x1b[3~"
RESULTS = []
OK = "reply with the single word ok"
LONG = "write a story of about 300 words about a lighthouse keeper"
LOG = os.path.join(OUT, "steps.log")
CALLS = {"openai-key": 0, "openai-oauth": 0, "anthropic-oauth": 0}
SESSIONS = []
STARTED_HOMES = set()


def log(msg):
    print(msg, flush=True)
    with open(LOG, "a") as f:
        f.write(msg + "\n")


def fresh_home(path, src=None):
    if path not in (KEY_HOME, OAUTH_HOME):
        raise ValueError("cannot reset a home outside this run")
    stop_home(path)
    shutil.rmtree(path, ignore_errors=True)
    os.makedirs(path, mode=0o700)
    if src:
        for name in ("auth.json", "settings.json"):
            source = os.path.join(src, name)
            if os.path.isfile(source):
                target = os.path.join(path, name)
                shutil.copyfile(source, target)
                os.chmod(target, 0o600)


def shot(name, s, note=""):
    t = s.view()
    p = os.path.join(OUT, name + ".txt")
    with open(p, "w") as f:
        f.write(t + "\n")
    log(f"--- {name} {s.cols}x{s.rows} {note}\n{t}\n")
    return t


def pick_model(s, flt):
    s.send(KEYS["ctrl+l"])
    s.wait("Select model", 5)
    time.sleep(0.3)
    s.type(flt)
    time.sleep(0.5)
    s.send("\r")
    time.sleep(1.5)


def prompt(s, text, bucket, timeout=90, wait_done=r"· \d+(\.\d+)?s · ↑"):
    s.type(text)
    s.send("\r")
    CALLS[bucket] += 1
    ok = s.wait(wait_done, timeout, view=False)
    s.settle(1.0, 15)
    return ok


def start(home, cols, rows, env=None):
    if home not in (KEY_HOME, OAUTH_HOME):
        raise ValueError("cannot start a session outside this run")
    s = Session(home, cols, rows, env=dict(env or {}))
    SESSIONS.append(s)
    STARTED_HOMES.add(home)
    if not s.wait("leader [0-9a-f]+", 15):
        log("WARN: no leader id in footer after 15 s")
    return s


def oauth_home():
    """Copy the OAuth home once per run; a token refresh rewrites auth.json, so
    the original is never used. cleanup() deletes the copy."""
    if not os.path.isdir(OAUTH_HOME):
        if not os.path.isdir(OAUTH_SOURCE):
            raise SystemExit("OAuth source home is missing")
        fresh_home(OAUTH_HOME, OAUTH_SOURCE)
        # Stored token strings join the scan set (values stay in memory).
        try:
            with open(os.path.join(OAUTH_HOME, "auth.json")) as f:
                SCAN_SET.update(stored_strings(json.load(f)))
        except (OSError, ValueError):
            pass


def stored_strings(node, prefix="auth"):
    """Credential fields of the current store, keyed by field path."""
    out = {}
    if isinstance(node, dict):
        for k, v in node.items():
            out.update(stored_strings(v, f"{prefix}.{k}"))
    elif isinstance(node, list):
        for i, v in enumerate(node):
            out.update(stored_strings(v, f"{prefix}[{i}]"))
    elif isinstance(node, str) and node and prefix.rsplit(".", 1)[-1] in {
            "apiKey", "accessToken", "refreshToken", "idToken", "key", "access", "refresh"}:
        out[prefix] = node
    return out


def stop_home(home):
    failed = False
    for s in list(SESSIONS):
        if s.home == home:
            try:
                s.close()
                SESSIONS.remove(s)
            except Exception:
                failed = True
    if home in STARTED_HOMES:
        try:
            r = leader_stop(home)
            if r.returncode != 0:
                failed = True
            else:
                STARTED_HOMES.remove(home)
        except Exception:
            failed = True
    if failed:
        raise RuntimeError("could not stop the session or leader of this run")


def cleanup():
    failed = False
    for home in (OAUTH_HOME, KEY_HOME):
        try:
            stop_home(home)
        except (OSError, RuntimeError):
            failed = True
    try:
        clean = artifact_scan()
    finally:
        if not failed:
            shutil.rmtree(RUN_HOME, ignore_errors=True)
    if failed:
        raise RuntimeError("run cleanup failed; the owned homes were kept")
    return clean


def paste(s, value):
    """Send a bracketed paste through the PTY descriptor. The value is never
    an argument, never logged and never written to a file."""
    s.send((PASTE_BEGIN + value + PASTE_END).encode())


def check(name, ok, detail=""):
    RESULTS.append({"step": name, "result": "pass" if ok else "fail", "detail": detail})
    log(f"[{'pass' if ok else 'FAIL'}] {name} {detail}".rstrip())
    return ok


def has_line(s, pattern, view=True):
    t = s.view() if view else s.text()
    return re.search(pattern, t, re.M) is not None


def open_providers(s):
    s.type("/providers")
    s.send("\r")
    return s.wait(r"╭─ Providers", 10) and s.wait("Type to search", 5)


def pick_provider(s, flt):
    s.type(flt)
    s.wait("⌕ " + flt, 5)
    s.send("\r")


def sign_in_key(s):
    """Save the real key through /providers (paste, enter). Returns True when saved."""
    assert REAL_KEY, "OPENAI_API_KEY is not set"
    open_providers(s)
    pick_provider(s, "openai api")
    s.wait("Paste your API key.", 10)
    paste(s, REAL_KEY)
    s.wait(r"\(hidden\)", 5)
    s.send("\r")
    ok = s.wait(r"openai: API key saved\.", 40, view=False)
    s.settle(1.0, 10)
    return ok


def start_keyed(home, cols, rows):
    fresh_home(home)
    s = start(home, cols, rows)
    sign_in_key(s)
    return s


def finish(s, home):
    try:
        s.send(KEYS["ctrl+d"])
        time.sleep(1.0)
    finally:
        stop_home(home)


def repeated(cols, rows):
    fresh_home(KEY_HOME)
    s = start_keyed(KEY_HOME, cols, rows)
    pick_model(s, "gpt-5.5")
    shot(f"repeat-{cols}-model", s)
    res = []
    for i in range(3):
        ok = prompt(s, OK, "openai-key")
        res.append(ok)
        shot(f"repeat-{cols}-p{i+1}", s, f"done={ok}")
    log(f"repeated {cols}x{rows} replies_done={res}")
    shot(f"repeat-{cols}-final-with-history", s)
    log("history+screen:\n" + s.text())
    finish(s, KEY_HOME)


def cancel(cols, rows, key):
    fresh_home(KEY_HOME)
    s = start_keyed(KEY_HOME, cols, rows)
    pick_model(s, "gpt-5.5")
    s.type(LONG)
    s.send("\r")
    CALLS["openai-key"] += 1
    s.wait(r"◆", 60)
    time.sleep(1.5)
    shot(f"cancel-{key}-streaming", s)
    t0 = time.time()
    s.send(KEYS[key])
    seen = {}
    while time.time() - t0 < 3:
        v = s.view()
        for tag, pat in (("cancelling", "Cancelling"), ("marker", "⊘"), ("working", "Working")):
            if pat in v and tag not in seen:
                seen[tag] = round(time.time() - t0, 3)
            if tag == "working" and pat not in v and "working" in seen and "gone" not in seen:
                seen["working_gone"] = round(time.time() - t0, 3)
        time.sleep(0.02)
    log(f"cancel-{key} first-seen seconds after key: {seen}")
    shot(f"cancel-{key}-immediate", s)
    s.settle(1.5, 30)
    shot(f"cancel-{key}-settled", s)
    ok = prompt(s, OK, "openai-key")
    shot(f"cancel-{key}-next-prompt", s, f"done={ok}")
    log("history+screen:\n" + s.text())
    finish(s, KEY_HOME)


def ctrlc_running():
    fresh_home(KEY_HOME)
    s = start_keyed(KEY_HOME, 80, 24)
    pick_model(s, "gpt-5.5")
    s.type(LONG)
    s.send("\r")
    CALLS["openai-key"] += 1
    s.wait(r"◆", 60)
    time.sleep(1)
    s.send(KEYS["ctrl+c"])
    time.sleep(0.4)
    shot("ctrlc-running-notice", s)
    s.send(KEYS["esc"])
    s.settle(1.5, 30)
    shot("ctrlc-running-then-esc", s)
    time.sleep(2.5)
    log("history+screen:\n" + s.text())
    finish(s, KEY_HOME)


def oauth_switch(cols, rows):
    oauth_home()
    s = start(OAUTH_HOME, cols, rows)
    pick_model(s, "gpt-5.6-sol")
    shot("switch-1-select-openai", s)
    ok1 = prompt(s, OK, "openai-oauth")
    shot("switch-2-openai-prompt", s, f"done={ok1}")
    pick_model(s, "claude-sonnet")
    shot("switch-3-select-anthropic", s)
    ok2 = prompt(s, OK, "anthropic-oauth")
    shot("switch-4-anthropic-prompt", s, f"done={ok2}")
    pick_model(s, "gpt-5.6-sol")
    shot("switch-5-back-openai", s)
    ok3 = prompt(s, OK, "openai-oauth")
    shot("switch-6-openai-prompt", s, f"done={ok3}")
    log("history+screen:\n" + s.text())
    finish(s, OAUTH_HOME)


def thinking():
    oauth_home()
    s = start(OAUTH_HOME, 80, 24)
    pick_model(s, "claude-sonnet")
    for _ in range(2):
        s.send("\x1b[Z")  # shift+tab cycles the thinking level
        time.sleep(0.5)
    shot("thinking-level", s)
    ok = prompt(s, "what is 17 times 23? answer with the number only", "anthropic-oauth")
    shot("thinking-shown", s, f"done={ok}")
    s.send(KEYS["ctrl+t"])
    time.sleep(1.5)
    shot("thinking-hidden", s)
    s.send(KEYS["ctrl+t"])
    time.sleep(1.5)
    shot("thinking-visible-again", s)
    log("history+screen:\n" + s.text())
    finish(s, OAUTH_HOME)


def detach():
    fresh_home(KEY_HOME)
    s = start_keyed(KEY_HOME, 80, 24)
    pick_model(s, "gpt-5.5")
    s.type(LONG)
    s.send("\r")
    CALLS["openai-key"] += 1
    s.wait(r"◆", 60)
    time.sleep(0.5)
    shot("detach-before", s)
    s.send(KEYS["ctrl+d"])
    time.sleep(1.5)
    log(f"first ask alive after ctrl+d: {s.proc.poll() is None} rc={s.proc.poll()}")
    shot("detach-after-ctrl-d", s)
    i = s.raw.rfind(b"Detached")
    log("raw bytes before 'Detached': " + repr(s.raw[max(0, i - 40):i]))
    s.close()
    o = start(KEY_HOME, 80, 24)
    shot("detach-second-start", o)
    o.type("/sessions")
    time.sleep(0.5)
    o.send("\r")
    time.sleep(1.5)
    shot("detach-sessions-list", o)
    o.send(KEYS["down"])
    time.sleep(0.5)
    shot("detach-sessions-list-down", o)
    o.send("\r")
    time.sleep(2.0)
    shot("detach-observer-attached", o)
    o.settle(2, 60)
    shot("detach-observer-settled", o)
    log("history+screen:\n" + o.text())
    finish(o, KEY_HOME)


def no_model_footer(s):
    return has_line(s, r"leader [0-9a-f]+.*no model")


def providers(cols=80, rows=24):
    """Scenarios 1-4 and the default-model checks in one ordered run of one home.

    providers_first_run, key_refused, key_saved_then_model, remove_then_no_model,
    default_model_persists.
    """
    assert REAL_KEY, "OPENAI_API_KEY is not set in the environment"
    fresh_home(KEY_HOME)

    # providers_first_run: empty home, no credential variable in the child.
    s = start(KEY_HOME, cols, rows)
    s.wait("no model", 15)
    s.settle(1.0, 10)
    shot("p1-first-run", s)
    check("providers_first_run.footer_no_model", no_model_footer(s))
    check("providers_first_run.rail_notice", has_line(s, "Add a provider with /providers"))
    check("providers_first_run.no_model_line", not has_line(s, r"^  model  \S+/\S+"))
    s.send(KEYS["ctrl+l"])
    ok = s.wait("Use /providers to add providers.", 10)
    shot("p1-model-empty-hint", s)
    check("providers_first_run.model_empty_hint", ok)
    s.send(KEYS["esc"])
    s.wait(r"Ask anything", 5)

    # key_refused: a made-up key; the refusal names status and provider code.
    check("key_refused.opened", open_providers(s))
    pick_provider(s, "openai api")
    check("key_refused.key_box", s.wait("Paste your API key.", 10))
    paste(s, FAKE_KEY)
    check("key_refused.hidden", s.wait(r"\(hidden\)", 5))
    s.send("\r")
    ok = s.wait(r"openai refused the key: .*\(HTTP \d{3}\)", 40, view=False)
    s.settle(1.0, 10)
    shot("p2-refused", s)
    check("key_refused.refusal_shape", ok)
    check("key_refused.nothing_saved", s.wait("Nothing was saved.", 5, view=False))
    check("key_refused.no_checking_row", not has_line(s, "Checking the key"))
    s.send(KEYS["esc"])
    s.wait(r"Sign-in to openai cancelled", 5, view=False)
    s.send(KEYS["esc"])  # close the provider box

    # key_saved_then_model: the real key; /model lists openai; one short prompt.
    s.settle(0.8, 5)
    check("key_saved_then_model.opened", open_providers(s))
    pick_provider(s, "openai api")
    s.wait("Paste your API key.", 10)
    paste(s, REAL_KEY)
    s.wait(r"\(hidden\)", 5)
    s.send("\r")
    ok = s.wait(r"openai: API key saved\.", 40, view=False)
    s.settle(1.0, 10)
    shot("p3-saved", s)
    check("key_saved_then_model.saved", ok)
    check("key_saved_then_model.choose_hint", s.wait(r"Choose a model with /model\.", 5, view=False))
    s.wait(r"Ask anything", 5)
    check("key_saved_then_model.still_no_model", no_model_footer(s))
    s.send(KEYS["ctrl+l"])
    check("key_saved_then_model.model_selector", s.wait("Select model", 10))
    check("key_saved_then_model.openai_group", s.wait(r"● openai", 5))
    check("key_saved_then_model.no_other_group", not has_line(s, r"● (anthropic|xai|alibaba)"))
    s.type("gpt-5.5")
    time.sleep(0.5)
    shot("p3-model-list", s)
    s.send("\r")
    ok = s.wait(r"leader [0-9a-f]+.*gpt-5\.5", 15)
    s.settle(1.0, 10)
    shot("p3-model-chosen", s)
    check("key_saved_then_model.footer_model", ok)
    check("key_saved_then_model.change_line", has_line(s, r"model\s+openai/gpt-5\.5", view=False))
    check("key_saved_then_model.default_saved", s.wait(r"Default model: openai/gpt-5\.5", 5, view=False))
    ok = prompt(s, OK, "openai-key")
    shot("p3-prompt", s, f"done={ok}")
    check("key_saved_then_model.prompt_answers", ok)
    finish(s, KEY_HOME)

    # default_model_persists (saved): a new leader and session in the same home.
    s = start(KEY_HOME, cols, rows)
    ok = s.wait(r"model  openai/gpt-5\.5", 20, view=False)
    s.settle(1.0, 10)
    shot("p5-restart-saved-default", s)
    check("default_model_persists.start_line", ok)
    check("default_model_persists.footer", has_line(s, r"leader [0-9a-f]+.*gpt-5\.5"))

    # remove_then_no_model: remove the only provider; prompt refused, no call.
    check("remove_then_no_model.opened", open_providers(s))
    s.type("openai api")
    s.wait("⌕ openai api", 5)
    s.send(DELETE)
    check("remove_then_no_model.confirm", s.wait(r"Remove OpenAI API key\?", 10))
    s.send("\r")
    check("remove_then_no_model.removed", s.wait(r"Removed the saved credential for openai\.", 20, view=False))
    s.send(KEYS["esc"])
    s.settle(1.0, 10)
    check("remove_then_no_model.footer_no_model", s.wait(r"leader [0-9a-f]+.*no model", 15))
    s.type("hello after removal")
    s.send("\r")
    ok = s.wait(r"Not sent: no model", 10, view=False)
    shot("p4-refused", s)
    check("remove_then_no_model.prompt_refused", ok)
    s.send(KEYS["ctrl+c"])  # clears the draft
    time.sleep(0.5)
    finish(s, KEY_HOME)

    # default_model_persists (removed): a new session has no model.
    s = start(KEY_HOME, cols, rows)
    s.wait("no model", 15)
    s.settle(1.0, 10)
    shot("p5-restart-after-removal", s)
    check("default_model_persists.no_model_after_removal", no_model_footer(s))
    finish(s, KEY_HOME)


def oauth(cols=80, rows=24):
    """oauth_rows_and_switch: signed-in rows, a switch across providers, no prompt."""
    oauth_home()
    s = start(OAUTH_HOME, cols, rows)
    s.settle(1.5, 15)
    shot("o1-start", s)
    check("oauth_rows_and_switch.start_line", has_line(s, r"^  model  \S+/\S+", view=False))
    check("oauth_rows_and_switch.footer_model", not no_model_footer(s))
    check("oauth_rows_and_switch.providers_open", open_providers(s))
    s.settle(0.8, 5)
    shot("o2-providers", s)
    signed = len(re.findall(r"signed in", s.view()))
    check("oauth_rows_and_switch.signed_in_rows", signed >= 2, f"count={signed}")
    s.send(KEYS["esc"])
    s.settle(0.8, 5)
    s.send(KEYS["ctrl+l"])
    s.wait("Select model", 10)
    time.sleep(0.5)
    shot("o3-model-list", s)
    check("oauth_rows_and_switch.groups", len(re.findall(r"● ", s.view())) >= 2)
    s.send(KEYS["esc"])
    s.settle(0.8, 5)
    for n, (flt, prov, model, prev) in enumerate((("gpt-5.6-sol", "openai", "gpt-5.6-sol", "openai/gpt-5.5"),
                                                  ("claude-sonnet", "anthropic", "claude-sonnet-4-6", "openai/gpt-5.6-sol"),
                                                  ("gpt-5.6-sol", "openai", "gpt-5.6-sol", "anthropic/claude-sonnet-4-6")), 1):
        s.send(KEYS["ctrl+l"])
        s.wait("Select model", 10)
        s.wait(r"⌕ type to filter", 5)
        time.sleep(0.3)
        s.type(flt)
        time.sleep(0.6)
        shot(f"o4-{n}-filter-{prov}", s)
        s.send("\r")
        ok = s.wait(rf"leader [0-9a-f]+\s+.*{re.escape(model)}", 15)
        s.settle(1.0, 10)
        shot(f"o4-{n}-switched-{prov}", s)
        check(f"oauth_rows_and_switch.footer_{n}_{prov}", ok)
        pat = rf"model\s+{prov}/{re.escape(model)} · was {re.escape(prev)}"
        check(f"oauth_rows_and_switch.change_line_{n}_{prov}", has_line(s, pat, view=False), f"was={prev}")
    finish(s, OAUTH_HOME)


def artifact_scan():
    """artifact_scan: no credential value in the output directory or a leader log."""
    logs = os.path.join(OUT, "leader-logs")
    os.makedirs(logs, exist_ok=True)
    for tag, home in (("key", KEY_HOME), ("oauth", OAUTH_HOME)):
        try:
            with open(os.path.join(home, "auth.json")) as f:
                SCAN_SET.update(stored_strings(json.load(f)))
        except FileNotFoundError:
            pass
        except (OSError, ValueError):
            check("artifact_scan.store_read", False, "could not read a current credential store")
        for name in ("leader.log", "leader.log.1"):
            src = os.path.join(home, name)
            if os.path.isfile(src):
                shutil.copyfile(src, os.path.join(logs, f"{tag}-{name}"))
    with open(os.path.join(OUT, "results.json"), "w") as f:
        json.dump({"steps": RESULTS, "calls": CALLS}, f, indent=2)
    hits = runner.scan_secrets(OUT, SCAN_SET)
    names = sorted({n for v in hits.values() for n in v})
    check("artifact_scan.clean", not hits, f"files={len(hits)} variables={names} scanned_values={len(SCAN_SET)} files_scanned_dir={OUT}")
    return not hits


def acceptance(cols=80, rows=24):
    providers(cols, rows)
    oauth(cols, rows)
    failed = [r["step"] for r in RESULTS if r["result"] != "pass"]
    log(f"acceptance steps={len(RESULTS)} failed={failed}")
    return 0 if not failed else 1


SCEN = {
    "providers": lambda a: providers(*a),
    "oauth": lambda a: oauth(*a),
    "acceptance": lambda a: acceptance(*a),
    "repeat": lambda a: repeated(*a),
    "cancel-esc": lambda a: cancel(*a, "esc"),
    "cancel-ctrl-c": lambda a: cancel(*a, "ctrl+c"),
    "ctrlc-running": lambda a: ctrlc_running(),
    "switch": lambda a: oauth_switch(*a),
    "thinking": lambda a: thinking(),
    "detach": lambda a: detach(),
}

if __name__ == "__main__":
    name = sys.argv[1]
    size = (int(sys.argv[2]), int(sys.argv[3])) if len(sys.argv) > 3 else (80, 24)
    log(f"=== scenario {name} {size}")
    code = 0
    try:
        ensure_ask()
        code = SCEN[name](size) or 0
    finally:
        log("calls " + json.dumps(CALLS))
        if not cleanup():
            code = 1
    if any(result["result"] != "pass" for result in RESULTS):
        code = 1
    sys.exit(code)
