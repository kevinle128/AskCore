#!/usr/bin/env python3
"""Generate exact-width sketches for the T1a TUI design (visual direction B4).

Each sketch line is a list of segments (text, roles). design.md uses the plain text;
preview.html renders the roles as colors. Both come from this file, so they cannot disagree.

Usage: python3 sketch-gen.py > sketches.md
"""
import unicodedata

MARK = "-- live region starts --"


def cw(s):
    n = 0
    for c in s:
        if unicodedata.combining(c):
            continue
        n += 2 if unicodedata.east_asian_width(c) in "WF" else 1
    return n


def wrap(text, w, first="", rest=""):
    """Wrap by terminal columns, not by characters."""
    lines, cur = [], first
    for word in text.split():
        cand = word if cur in (first, rest) else " " + word
        if cw(cur + cand) > w and cur not in (first, rest):
            lines.append(cur)
            cur = rest + word
        else:
            cur += cand
    lines.append(cur)
    return lines


def _flat(items):
    out = []
    for it in items:
        if isinstance(it, list):
            out.extend(it)
        else:
            out.append(it)
    return out


def L(w, *segs, right=None, fill=""):
    """One row: left segments, optional right-aligned segments, padded to w with the fill role."""
    left, rgt = _flat(segs), _flat(right or [])
    used = sum(cw(t) for t, _ in left) + sum(cw(t) for t, _ in rgt)
    if used > w:
        raise SystemExit(f"row too wide ({used}>{w}): {''.join(t for t, _ in left + rgt)!r}")
    return left + [(" " * (w - used), fill)] + rgt


def plain(row):
    return "".join(t for t, _ in row)


# ---------------------------------------------------------------- content

Q1 = "why does TestLeaderStale fail on CI?"
A1 = ("The test waits for the stale socket check, but CI runs it with a 1 s deadline. "
      "The leader removes the stale socket after the lock probe, and the probe can take 1.2 s.")
Q2 = "show me the probe code"
A2 = ("The probe lives in internal/leader/client.go. It dials the socket, then sends an "
      "initialize frame and waits")
THINK = ("The stale check runs before the deadline starts, so the deadline is too short only "
         "when the probe is slow. I should look at the probe timing first.")
META = "deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004"

MODELS = [
    ("alibaba", "deepseek-v4.1-flash", "128k", "current"),
    ("alibaba", "qwen4-coder-plus", "256k", ""),
    ("anthropic", "claude-opus-5-5", "1M", "reasoning"),
    ("anthropic", "claude-sonnet-5-5", "1M", "reasoning"),
    ("openai", "gpt-5.5-codex", "400k", "reasoning"),
    ("xai", "grok-5-code", "256k", "reasoning"),
]
PROVIDER_ROLE = {"alibaba": "prov-1", "anthropic": "prov-2", "openai": "prov-3", "xai": "prov-4"}


def header(w, place="leader 7c1e"):
    return [L(w, ("◢◤ ", "logo"), ("ask", "logo"), (f"   v0.1.0 · {place} · ~/src/askcore", "muted")), L(w)]


def user(w, q, time):
    rows = wrap(q, w - 12, "", "")
    out = [L(w, ("  ", "panel"), ("❯ ", "accent bold panel"), (rows[0], "user panel"),
             right=[(time + "  ", "dim panel")], fill="panel")]
    for r in rows[1:]:
        out.append(L(w, ("    ", "panel"), (r, "user panel"), fill="panel"))
    return out + [L(w)]


def answer(w, text):
    rows = wrap(text, w - 4)
    return [L(w, ("  ◆ " if k == 0 else "    ", "assistant bold"), (r, "")) for k, r in enumerate(rows)]


def thinking(w, text):
    rows = wrap(text, w - 4)
    return [L(w, ("  ∴ " if k == 0 else "    ", "thinking"), (r, "thinking")) for k, r in enumerate(rows)] + [L(w)]


def code(w):
    return [L(w), L(w, ("    go", "dim code-panel"), fill="code-panel"),
            L(w, ("    deadline := 5 * time.Second", "code code-panel"), fill="code-panel")]


def meta(w, text=META):
    return [L(w, ("    " + text, "dim")), L(w)]


def tool_running(w):
    return [L(w, ("  ◇ ", "tool bold"), ("echo", "tool bold"), ("  probe code request", ""),
              right=[("running ⠋ ", "warning")])]


def tool_done(w):
    return [L(w, ("  ● ", "tool"), ("echo", "tool bold"), ("  probe code request", ""),
              right=[("✓ 0.1s ", "success")]), L(w)]


def history(w, place="leader 7c1e"):
    return header(w, place) + user(w, Q1, "14:02") + answer(w, A1) + code(w) + meta(w)


def status(w, mark, text, tail, role="accent"):
    return [L(w, ("┊ ", role), (mark + " ", role + " bold"), (text, role + " bold shimmer"), (tail, "muted"))]


def editor(w, lines=None, placeholder=None):
    """Rounded editor box. lines: list of row segment lists, or None for the empty prompt."""
    inner = w - 2
    if placeholder is None:
        placeholder = "Ask anything  ·  / commands  ·  ctrl+l model" if w >= 60 else "Ask anything"
    if lines is None:
        lines = [[("❯ ", "accent bold"), ("▌", "cursor"), (placeholder, "dim italic")]]
    out = [L(w, ("╭" + "─" * inner + "╮", "border"))]
    for segs in lines:
        used = 1 + sum(cw(t) for t, _ in segs)
        out.append([("│ ", "border")] + segs + [(" " * (inner - used), ""), ("│", "border")])
    out.append(L(w, ("╰" + "─" * inner + "╯", "border")))
    return out


def ctx_bar(pct, n=8):
    full = round(pct / 100 * n)
    role = "error" if pct >= 90 else "warning" if pct >= 70 else "accent"
    return ("▰" * full + "▱" * (n - full), role)


def footer(w, dot=("●", "success"), place="leader 7c1e", model="deepseek-v4.1-flash", think="",
           pct=18, cost="$0.04", place_role="muted"):
    if w < 60:
        return [L(w, (f"  {dot[0]} ", dot[1]), (place.split()[-1], place_role),
                  right=[(model, "assistant bold"), (f" {pct}% ", "muted")])]
    right = [(model, "assistant bold")]
    if think:
        right.append((f" ◐ {think}", "assistant"))
    right += [("  ", ""), ctx_bar(pct), (f" {pct}%", "muted"), (f"  {cost} ", "muted")]
    return [L(w, (f"  {dot[0]} ", dot[1]), (place, place_role), ("   ⌂ ", "muted"), ("~/src/askcore", "muted"),
              right=right)]


def frame(w, h, scroll, live, top_anchor=False):
    marker = [MARK] if live else []
    body = len(scroll) + len(live)
    if body >= h:
        keep = h - len(live)
        if keep < 0:
            raise SystemExit("live region taller than terminal")
        return scroll[len(scroll) - keep:] + marker + live
    filler = [L(w)] * (h - body)
    if top_anchor:
        return scroll + marker + live + filler
    return filler + scroll + marker + live


# ---------------------------------------------------------------- states

def state_start(w, h):
    live = status(w, "◌", "Starting leader…", "  first run can take a few seconds") + editor(w) + footer(
        w, dot=("◌", "muted"), place="leader starting", pct=0, cost="$0.00")
    return frame(w, h, [], live, top_anchor=True)


def state_in_process(w, h):
    scroll = header(w, "in-process agent")
    live = editor(w) + footer(w, dot=("○", "muted"), place="in-process", pct=0, cost="$0.00")
    return frame(w, h, scroll, live, top_anchor=True)


def state_idle(w, h):
    return frame(w, h, history(w), editor(w) + footer(w))


def state_completion(w, h):
    rows = [("/model", "Select a model"), ("/new", "Start a new session in this directory"),
            ("/sessions", "Watch another live session"), ("/take", "Take control of this session"),
            ("/quit", "Detach and quit")]
    comp = []
    for k, (cmd, desc) in enumerate(rows):
        sel = k == 0
        bg = " selected" if sel else ""
        comp.append(L(w, ("  ❯ " if sel else "    ", "accent bold" + bg), (f"{cmd:<11}", ("accent bold" if sel else "") + bg),
                      (desc, "muted" + bg), fill=bg.strip()))
    live = comp + editor(w, [[("❯ ", "accent bold"), ("/", ""), ("▌", "cursor")]]) + footer(w)
    return frame(w, h, history(w), live)


def state_multiline(w, h):
    lines = [[("❯ ", "accent bold"), ("rewrite the probe so that it:", "")],
             [("  ", ""), ("- uses an injected clock", "")],
             [("  ", ""), ("- returns ErrLeaderStarting after the deadline", ""), ("▌", "cursor")]]
    return frame(w, h, history(w), editor(w, lines) + footer(w))


def state_sending(w, h):
    live = status(w, "◌", "Sending…", "") + editor(w, [[("❯ ", "accent bold"), ("show me the probe code", ""), ("▌", "cursor")]]) + footer(w)
    return frame(w, h, history(w), live)


def state_send_failed(w, h):
    live = [L(w, ("┊ ", "error"), ("✗ ", "error bold"), ("Not sent: not_driver", "error bold"),
              (" · draft kept · /take to drive", "muted"))] + editor(
        w, [[("❯ ", "accent bold"), ("show me the probe code", ""), ("▌", "cursor")]]) + footer(w)
    return frame(w, h, history(w), live)


def state_stream(w, h):
    scroll = history(w) + user(w, Q2, "14:03")
    live = tool_running(w) + answer(w, A2) + [L(w)] + status(w, "◌", "Working…", " 4s · esc to interrupt") + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def state_queue(w, h):
    scroll = history(w) + user(w, Q2, "14:03") + tool_done(w)
    live = answer(w, A2) + [L(w)] + status(w, "◌", "Working…", " 9s · esc to interrupt") + [
        L(w, ("┆ ", "steer"), ("⧗ steer", "steer bold"), ("   use the fake clock from internal/testsupport", "")),
        L(w, ("┆ ", "tool"), ("⧗ next ", "tool bold"), ("   then run the leader tests", "")),
        L(w, ("┆ ", "dim"), ("alt+↑ edit queue · dashed rail = not sent yet", "dim italic")),
    ] + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def state_retry(w, h):
    scroll = history(w) + user(w, Q2, "14:03") + [
        L(w, ("  ↻ ", "warning"), ("Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 3 scheduled.", "warning")), L(w)]
    live = status(w, "◌", "Retrying (1/5) in 4s", " · rate limit · esc to cancel", "warning") + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def state_error(w, h):
    scroll = history(w) + user(w, Q2, "14:03") + answer(w, A2 + ".") + [L(w),
        L(w, ("  ✗ ", "error bold"), ("Error (auth): anthropic token expired and refresh failed.", "error")),
        L(w, ("    Run in a shell: ask auth login --provider anthropic. Then send again.", "muted")), L(w)]
    live = editor(w) + footer(w, model="claude-opus-5-5", think="medium", pct=21, cost="$0.11")
    return frame(w, h, scroll, live)


def state_cancel(w, h):
    scroll = history(w) + user(w, Q2, "14:03") + answer(w, A2) + [
        L(w, ("  ⊘ ", "muted"), ("Cancelled by you. Partial reply kept above.", "muted")), L(w)]
    return frame(w, h, scroll, editor(w) + footer(w))


def _box_title(w, title):
    head = "╭─ " + title + " "
    return L(w, ("╭─ ", "border"), (title, "accent bold"), (" " + "─" * (w - 2 - cw(head) + 1) + "╮", "border"))


def _box_row(w, segs, right=None, fill=""):
    inner = w - 2
    rgt = right or []
    used = sum(cw(t) for t, _ in segs) + sum(cw(t) for t, _ in rgt)
    return [("│", "border")] + segs + [(" " * (inner - used), fill)] + rgt + [("│", "border")]


def model_rows(w, rows, sel):
    out = []
    for k, (p, m, ctx, note) in enumerate(rows):
        s = k == sel
        bg = " selected" if s else ""
        out.append(_box_row(w, [(" ❯ " if s else "   ", "accent bold" + bg), ("● ", PROVIDER_ROLE[p] + bg),
                                (f"{p}/", "muted" + bg), (m, ("bold" if s else "") + bg)],
                            right=[(f"{ctx:>5}  ", "muted" + bg),
                                   (f"{note:<10} ", ("assistant" if note == "reasoning" else "success") + bg)],
                            fill=bg.strip()))
    return out


def _filter_row(w, typed=""):
    segs = [("  ⌕ ", "muted"), (typed, ""), ("▌", "cursor")]
    if not typed:
        segs.append(("type to filter", "dim italic"))
    return _box_row(w, segs)


def state_selector(w, h):
    live = [_box_title(w, "Select model"), _filter_row(w)] + model_rows(w, MODELS, 2) + [
        L(w, ("╰" + "─" * (w - 2) + "╯", "border")),
        L(w, ("   enter select  ·  esc close  ·  your draft is kept", "dim italic"))] + footer(w)
    return frame(w, h, history(w), live)


def state_selector_filter(w, h):
    rows = [m for m in MODELS if m[0] == "anthropic"]
    live = [_box_title(w, "Select model"), _filter_row(w, "anth")] + model_rows(w, rows, 0) + [
        _box_row(w, []),
        _box_row(w, [("  ✗ ", "error bold"), ("anthropic is not ready: not signed in. Model not changed.", "error")]),
        _box_row(w, [("    Run in a shell: ask auth login --provider anthropic", "muted")]),
        L(w, ("╰" + "─" * (w - 2) + "╯", "border")),
        L(w, ("   enter select  ·  esc close  ·  your draft is kept", "dim italic"))] + footer(w)
    return frame(w, h, history(w), live)


def state_sessions(w, h):
    rows = [("3f2a", "~/src/askcore", "this client drives"),
            ("8d07", "~/src/askcore", "driver busy"),
            ("c1e9", "~/src/pi-notes", "no driver")]
    out = []
    for k, (sid, cwd, drv) in enumerate(rows):
        s = k == 1
        bg = " selected" if s else ""
        out.append(_box_row(w, [(" ❯ " if s else "   ", "accent bold" + bg), (sid, ("bold" if s else "") + bg),
                                (f"  {cwd}", "muted" + bg)],
                            right=[(f"{drv} ", "muted" + bg)], fill=bg.strip()))
    live = [_box_title(w, "Live sessions"), _filter_row(w)] + out + [
        L(w, ("╰" + "─" * (w - 2) + "╯", "border")),
        L(w, ("   enter watch  ·  esc close", "dim italic"))] + footer(w)
    return frame(w, h, history(w), live)


DRAFT = [[("❯ ", "accent bold"), ("also check the lock timeout", ""), ("▌", "cursor")]]


def state_disconnected(w, h):
    live = [L(w, ("┊ ", "error"), ("✗ ", "error bold"), ("Disconnected from the leader", "error bold"),
              (" · reconnecting (attempt 2, next in 2s)", "muted")),
            L(w, ("┊ ", "dim"), ("draft kept · sending waits until the leader is back · nothing is resent", "dim italic"))
            ] + editor(w, DRAFT) + footer(w, dot=("●", "error"), place="leader lost", place_role="error")
    return frame(w, h, history(w), live)


def state_reconnected(w, h):
    scroll = history(w) + [L(w, ("  ↻ ", "success"), ("Reconnected. Session 3f2a restored.", "success")), L(w)]
    return frame(w, h, scroll, editor(w, DRAFT) + footer(w))


def state_session_gone(w, h):
    scroll = history(w) + [
        L(w, ("  ✗ ", "error bold"), ("Session 3f2a is gone: the leader restarted.", "error")),
        L(w, ("    The transcript above is kept. Your next prompt starts a new session.", "muted")), L(w)]
    return frame(w, h, scroll, editor(w, DRAFT) + footer(w, place="leader 91b0", pct=0, cost="$0.00"))


def state_observer(w, h):
    scroll = history(w) + user(w, Q2, "14:03")
    live = answer(w, A2) + [L(w)] + status(w, "◌", "Working…", " 6s · another client drives this session") + editor(
        w, [[("◉ ", "warning"), ("Watching. Type /take to take control when the run ends.", "dim italic")]]) + footer(
        w, dot=("◉", "warning"), place="observer 3f2a", place_role="warning")
    return frame(w, h, scroll, live)


def state_quit(w, h):
    scroll = history(w) + user(w, Q2, "14:03")
    live = answer(w, A2) + [L(w)] + [
        L(w, ("┊ ", "warning"), ("Press ctrl+c again to quit", "warning bold"), (" · the run continues in the leader", "muted"))
    ] + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def state_detached(w, h):
    scroll = history(w) + user(w, Q2, "14:03") + answer(w, A2) + [L(w),
        L(w, ("  ⏏ ", "muted"), ("Detached from session 3f2a. The run continues in leader 7c1e.", "muted")),
        L(w, ("    Run ask in this directory to see it again.", "dim")),
        L(w, ("$ ", ""), ("▌", "cursor"))]
    return frame(w, h, scroll, [])


def state_narrow(w, h):
    scroll = user(w, "giải thích lỗi 日本語のテスト", "14:05") + answer(
        w, "The 日本語 test fails because each wide character uses two columns.")
    live = [L(w)] + status(w, "◌", "Working…", " 3s · esc") + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def state_thinking_shown(w, h):
    scroll = history(w) + user(w, Q2, "14:03") + thinking(w, THINK) + answer(w, A2 + ".") + meta(
        w, "claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031")
    return frame(w, h, scroll, editor(w) + footer(w, model="claude-opus-5-5", think="medium", pct=21, cost="$0.11"))


def state_thinking_hidden(w, h):
    scroll = history(w) + user(w, Q2, "14:03") + [L(w, ("  ∴ ", "thinking"), ("Thinking...", "thinking")), L(w)] + answer(
        w, A2 + ".") + meta(w, "claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031")
    live = [L(w, ("┊ ", "dim"), ("Thinking blocks: hidden", "dim"), (" · ctrl+t to show", "dim italic"))] + editor(w) + footer(
        w, model="claude-opus-5-5", think="medium", pct=21, cost="$0.11")
    return frame(w, h, scroll, live)


SIZES = [(80, 24), (120, 40)]
STATES = [
    ("S1", "Starting: leader is spawned", state_start, SIZES),
    ("S2", "Leader unavailable: in-process agent", state_in_process, [(80, 24)]),
    ("S3", "Idle: empty editor", state_idle, SIZES),
    ("S4", "Slash command completion", state_completion, [(80, 24)]),
    ("S5", "Multi-line draft", state_multiline, [(80, 24)]),
    ("S5a", "Sending: prompt not accepted yet", state_sending, [(80, 24)]),
    ("S5b", "Send failed", state_send_failed, [(80, 24)]),
    ("S6", "Streaming reply with a tool call", state_stream, SIZES),
    ("S7", "Running with queued steer and follow-up", state_queue, SIZES),
    ("S8", "Retry wait", state_retry, [(80, 24)]),
    ("S9", "Run ended with an error", state_error, [(80, 24)]),
    ("S10", "Run cancelled", state_cancel, [(80, 24)]),
    ("S11", "Model selector open", state_selector, SIZES),
    ("S12", "Model selector: filter and readiness failure", state_selector_filter, [(80, 24)]),
    ("S12a", "Live session selector", state_sessions, [(80, 24)]),
    ("S13", "Disconnected from the leader", state_disconnected, [(80, 24)]),
    ("S13a", "Reconnected: same leader, session restored", state_reconnected, [(80, 24)]),
    ("S13b", "Reconnected: leader restarted, session gone", state_session_gone, [(80, 24)]),
    ("S14", "Observer: another client drives", state_observer, [(80, 24)]),
    ("S15", "Quit confirmation while a run is active", state_quit, [(80, 24)]),
    ("S16", "After quit: detached", state_detached, [(80, 24)]),
    ("S17", "Narrow terminal with wide characters", state_narrow, [(40, 14)]),
    ("S18", "Thinking shown (default)", state_thinking_shown, [(80, 24)]),
    ("S19", "Thinking hidden after ctrl+t", state_thinking_hidden, [(80, 24)]),
]


def block(rows):
    """Markdown for one sketch: plain text, live-region marker outside the fences, trailing empty rows counted."""
    parts, cur = [], []
    for r in rows:
        if r == MARK:
            if cur:
                parts.append("```text\n" + "\n".join(cur) + "\n```")
            parts.append("`-- live region starts --`")
            cur = []
        else:
            cur.append(plain(r))
    empty = 0
    while cur and not cur[-1].strip():
        cur.pop()
        empty += 1
    parts.append("```text\n" + "\n".join(cur) + "\n```")
    if empty:
        parts.append(f"({empty} empty rows below)")
    return "\n\n".join(parts)


def main():
    out = []
    for sid, title, fn, sizes in STATES:
        out.append(f"### {sid}. {title}\n")
        for (w, h) in sizes:
            rows = fn(w, h)
            for r in rows:
                if r != MARK and cw(plain(r)) != w:
                    raise SystemExit(f"{sid} {w}: width {cw(plain(r))}: {plain(r)!r}")
            out.append(f"{w}×{h}:\n")
            out.append(block(rows) + "\n")
    print("\n".join(out))


if __name__ == "__main__":
    main()
