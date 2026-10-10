#!/usr/bin/env python3
"""Real-provider scenarios for the built `ask` (see real_provider.py).

Usage: real_provider_scenarios.py <scenario> [cols rows]
Homes live under /private/tmp (a real directory; /tmp is a symlink on macOS and
the leader refuses it). The OAuth home is a copy of $HOME/.ask-t1a-oauth.
Credentials are never read: the copy keeps the file modes, the key stays in the
inherited environment.
"""
import json
import os
import shutil
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from real_provider import Session, KEYS, leader_stop, OUT  # noqa: E402

KEY_HOME = "/private/tmp/ask-rp-key"
OAUTH_HOME = "/private/tmp/ask-rp-oauth"
BAD_HOME = "/private/tmp/ask-rp-bad"
OK = "reply with the single word ok"
LONG = "write a story of about 300 words about a lighthouse keeper"
LOG = os.path.join(OUT, "steps.log")
CALLS = {"openai-key": 0, "openai-oauth": 0, "anthropic-oauth": 0, "openai-key-invalid": 0}


def log(msg):
    print(msg, flush=True)
    with open(LOG, "a") as f:
        f.write(msg + "\n")


def fresh_home(path, src=None):
    shutil.rmtree(path, ignore_errors=True)
    if src:
        shutil.copytree(src, path, copy_function=shutil.copy2)
        os.chmod(path, 0o700)
        for n in os.listdir(path):
            os.chmod(os.path.join(path, n), 0o600)
        for sock in ("leader.sock", "leader.pid", "leader.lock"):
            try:
                os.remove(os.path.join(path, sock))
            except OSError:
                pass
    else:
        os.makedirs(path, mode=0o700)


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


def start(home, cols, rows, env=None, drop=()):
    e = dict(env or {})
    s = Session(home, cols, rows, env=e)
    if not s.wait("leader [0-9a-f]+", 15):
        log("WARN: no leader id in footer after 15 s")
    return s


def finish(s, home):
    s.send(KEYS["ctrl+d"])
    time.sleep(1.0)
    s.close()
    r = leader_stop(home)
    log(f"leader stop rc={r.returncode}")


def repeated(cols, rows):
    fresh_home(KEY_HOME)
    s = start(KEY_HOME, cols, rows)
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
    s = start(KEY_HOME, cols, rows)
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
    s = start(KEY_HOME, 80, 24)
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


def invalid():
    fresh_home(BAD_HOME)
    s = start(BAD_HOME, 80, 24, env={"OPENAI_API_KEY": "dummy-invalid-key-0000"})
    pick_model(s, "gpt-5.5")
    s.type(OK)
    s.send("\r")
    CALLS["openai-key-invalid"] += 1
    s.wait(r"✗|rror", 60)
    s.settle(1.5, 30)
    shot("invalid-error-block", s)
    log("history+screen:\n" + s.text())
    finish(s, BAD_HOME)
    # new session, same home, good key from the inherited environment
    s = start(BAD_HOME, 80, 24)
    pick_model(s, "gpt-5.5")
    ok = prompt(s, OK, "openai-key")
    shot("invalid-then-good-key", s, f"done={ok}")
    finish(s, BAD_HOME)


def oauth_switch(cols, rows):
    if not os.path.isdir(OAUTH_HOME):  # keep the copy: a refresh rotates the stored tokens
        fresh_home(OAUTH_HOME, os.path.expanduser("~/.ask-t1a-oauth"))
    os.environ.pop("OPENAI_API_KEY", None)
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
    if not os.path.isdir(OAUTH_HOME):  # keep the copy: a refresh rotates the stored tokens
        fresh_home(OAUTH_HOME, os.path.expanduser("~/.ask-t1a-oauth"))
    os.environ.pop("OPENAI_API_KEY", None)
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
    s = start(KEY_HOME, 80, 24)
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


SCEN = {
    "repeat": lambda a: repeated(*a),
    "cancel-esc": lambda a: cancel(*a, "esc"),
    "cancel-ctrl-c": lambda a: cancel(*a, "ctrl+c"),
    "ctrlc-running": lambda a: ctrlc_running(),
    "invalid": lambda a: invalid(),
    "switch": lambda a: oauth_switch(*a),
    "thinking": lambda a: thinking(),
    "detach": lambda a: detach(),
}

if __name__ == "__main__":
    name = sys.argv[1]
    size = (int(sys.argv[2]), int(sys.argv[3])) if len(sys.argv) > 3 else (80, 24)
    log(f"=== scenario {name} {size}")
    try:
        SCEN[name](size)
    finally:
        log("calls " + json.dumps(CALLS))
