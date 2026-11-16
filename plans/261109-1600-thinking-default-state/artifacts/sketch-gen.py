#!/usr/bin/env python3
"""Generate exact-width sketches for the thinking level default TUI design.

Screen model: the alternate screen as the current goldens draw it (header row, rule, block pane,
status row, rule, prompt row, rule, footer row, shortcut bar). The block pane rows copy the goldens
TestModelCycleKeyRefusedWhileRunning and TestModelCycleKeyMovesThroughScope.

Each screen row is (region, segments). A segment is (text, roles). design.md uses the plain
text; preview.html renders the roles as colors and the region names beside the terminal.

Usage: python3 sketch-gen.py > sketches.md
"""
import unicodedata


def cw(s):
    return sum(0 if unicodedata.combining(c) else (2 if unicodedata.east_asian_width(c) in "WF" else 1) for c in s)


def plain(row):
    return "".join(t for t, _ in row)


def L(w, *segs, right=None):
    right = list(right or [])
    used = sum(cw(t) for t, _ in segs) + sum(cw(t) for t, _ in right)
    if used > w:
        raise SystemExit(f"row too wide ({used}>{w}): {plain(list(segs) + right)!r}")
    return list(segs) + [(" " * (w - used), "")] + right


# ---------------------------------------------------------------- screen parts

def header(w):
    return ("header", L(w, (" ◢◤ ask", "logo")))


def rule(w):
    return ("rule", L(w, ("─" * w, "border")))


def prompt_row(w):
    if w >= 60:
        return ("prompt pane", L(w, (" ❯ ", "accent"), ("Ask anything  ·  / commands  ·  ctrl+l model", "muted")))
    return ("prompt pane", L(w, (" ❯ ", "accent"), ("Ask anything", "muted")))


def footer(w, model="openai/gpt-5.5", level="default", pct="4%", bar="▱▱▱▱▱▱▱▱"):
    """The footer row. level None means the row shows no level (a model with no thinking)."""
    if w < 60:
        # Under 60 columns: no directory and no level, as footer.go draws it today.
        return ("footer", L(w, right=[(model, "assistant"), (" ", ""), (pct, "accent"), (" ", "")]))
    right = [(model, "assistant")]
    if level is not None:
        right += [(" ◐ " + level, "assistant")]
    right += [("  ", ""), (bar + " " + pct, "accent"), (" ", "")]
    return ("footer", L(w, (" ⌂ ~/src/askcore", "muted"), right=right))


def bar(w):
    if w >= 60:
        return ("bar", L(w, (" Tab blocks · ctrl+t thinking · ctrl+l model · / commands", "dim")))
    return ("bar", L(w, (" Tab blocks · / commands", "dim")))


def blank(w):
    return ("block pane", L(w))


def user(w, q, time):
    return ("block pane", L(w, ("  ❯ ", "accent bold"), (q, "fg bold"), right=[(time, "dim"), ("  ", "")]))


def text(w, t, role=""):
    return ("block pane", L(w, ("  " + t, role)))


def status(w, *segs):
    return ("status", L(w, ("┊ ", "dim"), *segs))


def ok_status(w, t):
    return status(w, ("✓ ", "success"), (t, "dim"))


TURN = [
    ("user", "why does TestLeaderStale fail on CI?", "14:02"),
    ("blank",),
    ("text", "The test waits for the stale socket check, but CI runs it with a 1 s"),
    ("text", "deadline. The leader removes the stale socket after the lock probe, and the"),
    ("text", "probe can take 1.2 s."),
    ("blank",),
    ("meta", "gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.040"),
]

NARROW_TURN = [
    ("text", "check, but CI runs it with a 1 s"),
    ("text", "deadline. The leader removes the"),
    ("text", "stale socket after the lock probe,"),
    ("text", "and the probe can take 1.2 s."),
    ("blank",),
    ("meta", "gpt-5.5 · 4.2s · ↑12.4k ↓1.8k …"),
]

RUNNING = [
    ("blank",),
    ("user", "show me the probe code", "14:03"),
    ("blank",),
    ("step", "◆ Reading internal/leader/client.go", "⠋"),
]


def pane_rows(w, items):
    rows = []
    for it in items:
        kind = it[0]
        if kind == "user":
            rows.append(user(w, it[1], it[2]))
        elif kind == "blank":
            rows.append(blank(w))
        elif kind == "text":
            rows.append(text(w, it[1]))
        elif kind == "meta":
            rows.append(text(w, it[1], "muted"))
        elif kind == "step":
            rows.append(("block pane", L(w, ("  ◆ ", "warning"), (it[1][2:], "fg"), right=[(it[2], "warning"), ("  ", "")])))
    return rows


def screen(w, h, pane, stat=(), foot=None):
    top = [header(w), rule(w)]
    bottom = [rule(w), prompt_row(w), rule(w), foot or footer(w), bar(w)]
    stat = list(stat)
    room = h - len(top) - len(bottom) - len(stat)
    if len(pane) > room:
        pane = pane[len(pane) - room:]
    pane = pane + [blank(w)] * (room - len(pane))
    return top + pane + stat + bottom


# ---------------------------------------------------------------- states

def d1(w, h):
    return screen(w, h, [], foot=footer(w, level="default", pct="0%"))


def d2(w, h):
    return screen(w, h, pane_rows(w, TURN), foot=footer(w, level="off"))


def d3(w, h):
    return screen(w, h, pane_rows(w, TURN), foot=footer(w, level="default"))


def d4(w, h):
    model = "anthropic/claude-sonnet-4-6"
    return screen(w, h, pane_rows(w, TURN), [ok_status(w, f"Model: {model} · default · 2 of 3")],
                  foot=footer(w, model=model, level="default", pct="12%", bar="▰▱▱▱▱▱▱▱"))


def d5(w, h):
    return screen(w, h, pane_rows(w, TURN), [ok_status(w, "Model: local/alpha · 3 of 3")],
                  foot=footer(w, model="local/alpha", level=None))


def d6(w, h):
    return screen(w, h, pane_rows(w, TURN + RUNNING), [status(w, ("Thinking level can change when the run ends.", "dim"))],
                  foot=footer(w, level="default"))


def d7(w, h):
    return screen(w, h, pane_rows(w, TURN), [
        status(w, ("Could not change the thinking level: the leader does not know default.", "error")),
        status(w, ("Restart it with this build: ask leader stop", "error")),
    ], foot=footer(w, level="max"))


def d8(w, h):
    return screen(w, h, pane_rows(w, NARROW_TURN), [ok_status(w, "openai/gpt-5.5 · default")],
                  foot=footer(w, level=None))


STATES = [
    ("D1", "Session start, no level chosen",
     "The TUI started with no --thinking and no :level, on a model that has thinking. The request sends no level, "
     "so the default of the provider applies. The footer row says default, not off. No notice.",
     d1, [(80, 24)]),
    ("D2", "shift+tab from default goes to off",
     "One press of shift+tab while the run is idle. The level is now a choice: off. Only the footer row changes, as "
     "for every other level today; the status row stays empty.",
     d2, [(80, 24)]),
    ("D3", "shift+tab from the last level comes back to default",
     "From max (or from the highest level that the model has), the next press asks default. The footer row says "
     "default again, and the next request sends no level.",
     d3, [(80, 24)]),
    ("D4", "ctrl+p to a scope entry that states no level",
     "The level stays default after a model change. The cycle notice names it, as it names a chosen level today.",
     d4, [(80, 24)]),
    ("D5", "ctrl+p to a model that has no thinking",
     "A model with no thinking shows no level in the footer row and in the notice, as today. The session keeps "
     "default; the next model with thinking shows default again.",
     d5, [(80, 24)]),
    ("D6", "shift+tab while the run works",
     "The text is the text of today. The level does not change, and the footer row keeps default.",
     d6, [(80, 24)]),
    ("D7", "An older leader refuses default",
     "A leader that was started by hand from an older build answers invalid_params for the word default. The level "
     "stays max. The next press skips default and asks off, so the cycle does not stop.",
     d7, [(80, 24)]),
    ("D8", "Narrow terminal: ctrl+p notice and footer row",
     "Under 60 columns the footer row shows no level (unchanged). The short cycle notice names default.",
     d8, [(40, 14)]),
]


def regions(rows):
    out, start = [], 0
    for i in range(1, len(rows) + 1):
        if i == len(rows) or rows[i][0] != rows[start][0]:
            a, b = start + 1, i
            out.append((f"row {a}" if a == b else f"rows {a}-{b}") + " " + rows[start][0])
            start = i
    return "; ".join(out)


def main():
    for sid, title, caption, fn, sizes in STATES:
        print(f"### {sid}. {title}\n")
        print(caption + "\n")
        for (w, h) in sizes:
            rows = fn(w, h)
            for _, r in rows:
                if cw(plain(r)) != w:
                    raise SystemExit(f"{sid} {w}: {plain(r)!r}")
            if len(rows) != h:
                raise SystemExit(f"{sid} height {len(rows)} != {h}")
            print(f"Regions: {regions(rows)}.\n")
            print(f"{w}×{h}:\n")
            print("```text")
            for _, r in rows:
                print(plain(r))
            print("```\n")


if __name__ == "__main__":
    main()
