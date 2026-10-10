#!/usr/bin/env python3
"""Render three visual directions for the T1a TUI on the same four states.

Run from the plan directory: python3 artifacts/style-gen.py
Output: artifacts/style-directions.html. Every line is checked to be exactly W columns.
"""
import html
import pathlib
import unicodedata

W = 80
here = pathlib.Path(__file__).resolve().parent

# Palette (truecolor). Lipgloss downsamples for 256-color terminals such as Terminal.app.
BG = "#13141c"
FG = "#d9dbe8"
MUTED = "#6f7390"
DIM = "#4c4f66"
PANEL = "#1d1f2b"
PANEL2 = "#252838"
VIOLET = "#a78bfa"
PINK = "#f472b6"
ORANGE = "#fb923c"
TEAL = "#2dd4bf"
GREEN = "#4ade80"
YELLOW = "#facc15"
RED = "#f87171"
BLUE = "#7aa2f7"


def cw(s):
    n = 0
    for c in s:
        if unicodedata.combining(c):
            continue
        n += 2 if unicodedata.east_asian_width(c) in "WF" else 1
    return n


def st(fg=None, bg=None, b=False, i=False, extra=""):
    parts = []
    if fg:
        parts.append(f"color:{fg}")
    if bg:
        parts.append(f"background:{bg}")
    if b:
        parts.append("font-weight:700")
    if i:
        parts.append("font-style:italic")
    if extra:
        parts.append(extra)
    return ";".join(parts)


def lerp(c1, c2, t):
    a = [int(c1[i:i + 2], 16) for i in (1, 3, 5)]
    b = [int(c2[i:i + 2], 16) for i in (1, 3, 5)]
    return "#" + "".join(f"{round(x + (y - x) * t):02x}" for x, y in zip(a, b))


def grad(text, stops=(VIOLET, PINK, ORANGE), b=True, bg=None):
    """One segment per character with interpolated color."""
    segs = []
    n = max(len(text) - 1, 1)
    for k, ch in enumerate(text):
        t = k / n
        if len(stops) == 3:
            col = lerp(stops[0], stops[1], t * 2) if t < .5 else lerp(stops[1], stops[2], (t - .5) * 2)
        else:
            col = lerp(stops[0], stops[1], t)
        segs.append((ch, st(col, bg, b)))
    return segs


def shimmer(text, color=PINK):
    return [(text, st(color, None, True, extra="") + ";", "shimmer")]


def L(*segs, fill=None, right=None):
    """A line of segments; optional right-aligned segments; padded to W with fill bg."""
    flat = []
    for s in segs:
        if isinstance(s, list):
            flat.extend(s)
        else:
            flat.append(s)
    rflat = []
    for s in (right or []):
        if isinstance(s, list):
            rflat.extend(s)
        else:
            rflat.append(s)
    used = sum(cw(s[0]) for s in flat) + sum(cw(s[0]) for s in rflat)
    if used > W:
        raise SystemExit(f"line too wide ({used}): {''.join(s[0] for s in flat + rflat)!r}")
    gap = (" " * (W - used), st(None, fill))
    return flat + [gap] + rflat


def boundary():
    return "BOUNDARY"


def wrap(text, width):
    lines, cur = [], ""
    for word in text.split():
        cand = word if not cur else cur + " " + word
        if cw(cand) > width:
            lines.append(cur)
            cur = word
        else:
            cur = cand
    lines.append(cur)
    return lines


Q1 = "why does TestLeaderStale fail on CI?"
A1 = ("The test waits for the stale socket check, but CI runs it with a 1 s deadline. "
      "The leader removes the stale socket after the lock probe, and the probe can take 1.2 s.")
Q2 = "show me the probe code"
A2 = "The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"
MODELS = [
    ("alibaba", "deepseek-v4.1-flash", "128k", "current"),
    ("alibaba", "qwen4-coder-plus", "256k", ""),
    ("anthropic", "claude-opus-5-5", "1M", "reasoning"),
    ("anthropic", "claude-sonnet-5-5", "1M", "reasoning"),
    ("openai", "gpt-5.5-codex", "400k", "reasoning"),
    ("xai", "grok-5-code", "256k", "reasoning"),
]
PCOL = {"alibaba": ORANGE, "anthropic": PINK, "openai": TEAL, "xai": BLUE}

# ------------------------------------------------------------------ direction A: Soft

def a_header():
    return [
        L((" ✦ ", st(PINK, None, True)), grad("ask"), ("  v0.1.0", st(MUTED))),
        L(("   leader 7c1e · protocol 3 · ~/src/askcore", st(MUTED))),
        L(),
    ]


def a_user(q):
    return [L(("  ", st(None, PANEL)), ("❯ ", st(PINK, PANEL, True)), (q, st(FG, PANEL, True)), fill=PANEL), L()]


def a_answer(text, width=76, done=True):
    out = []
    for k, l in enumerate(wrap(text, width)):
        out.append(L(("  ◆ " if k == 0 else "    ", st(VIOLET, None, True)), (l, st(FG))))
    return out


def a_code():
    return [
        L(),
        L(("    go", st(MUTED, PANEL2)), fill=PANEL2),
        L(("    deadline := 5 * time.Second", st(ORANGE, PANEL2)), fill=PANEL2),
        L(),
    ]


def a_history():
    return a_header() + a_user(Q1) + a_answer(A1) + a_code()


def a_editor(placeholder=True, text=None):
    inner = W - 2
    if text:
        body = [("❯ ", st(PINK, None, True)), (text, st(FG)), ("▌", st(PINK), "cursor")]
    else:
        body = [("❯ ", st(PINK, None, True)), ("▌", st(PINK), "cursor"),
                ("Ask anything  ·  / commands  ·  ctrl+l model", st(DIM, None, False, True))]
    used = 1 + sum(cw(s[0]) for s in body)
    return [
        L(("╭" + "─" * inner + "╮", st(DIM))),
        [("│ ", st(DIM))] + body + [(" " * (inner - used), ""), ("│", st(DIM))],
        L(("╰" + "─" * inner + "╯", st(DIM))),
    ]


def a_footer():
    return [L(("  ● ", st(GREEN)), ("leader 7c1e", st(MUTED)), ("   ⌂ ", st(MUTED)), ("~/src/askcore", st(MUTED)),
              right=[("deepseek-v4.1-flash", st(VIOLET, None, True)), ("  ", ""), ("▰▰▱▱▱▱▱▱", st(PINK)),
                     (" 18%", st(MUTED)), ("  $0.04 ", st(MUTED))])]


def a_status(word="Working…", tail=" 4s  ·  esc to interrupt"):
    return [L(("  ✻ ", st(PINK, None, True)), (word, st(PINK, None, True), "shimmer"), (tail, st(MUTED)))]


def a_tool(state="running"):
    out = [L(("  ◇ ", st(TEAL, None, True)), ("echo", st(TEAL, None, True)), ("  probe code request", st(FG)))]
    if state == "running":
        out.append(L(("    ⎿ ", st(DIM)), ("running…", st(MUTED, None, False, True))))
    else:
        out.append(L(("    ⎿ ", st(DIM)), ("✓ done", st(GREEN)), ("  0.1s", st(MUTED))))
    return out


def A():
    s3 = a_history() + [boundary()] + a_editor() + a_footer()
    s6 = a_history() + a_user(Q2) + [boundary()] + a_tool() + a_answer(A2) + [L()] + a_status() + a_editor() + a_footer()
    s7 = (a_history() + a_user(Q2) + a_tool("done") + [boundary()] + a_answer(A2) + [L()] + [
        L(("  ", ""), (" steer ", st(BG, VIOLET, True)), ("  use the fake clock from internal/testsupport", st(FG))),
        L(("  ", ""), (" next  ", st(BG, TEAL, True)), ("  then run the leader tests", st(FG))),
        L(("           alt+↑ edit queue", st(DIM, None, False, True))),
    ] + a_status(tail=" 9s  ·  esc to interrupt") + a_editor() + a_footer())
    inner = W - 2
    rows = []
    for k, (p, m, ctx, note) in enumerate(MODELS):
        sel = k == 2
        bg = PANEL2 if sel else None
        segs = [("│", st(DIM)), (" ❯ " if sel else "   ", st(PINK, bg, True)), ("● ", st(PCOL[p], bg)),
                (f"{p}/", st(MUTED, bg)), (m, st(FG, bg, sel))]
        right = [(f"{ctx:>5}  ", st(MUTED, bg)), (f"{note:<10}", st(VIOLET if note == "reasoning" else GREEN, bg)), (" ", st(None, bg)), ("│", st(DIM))]
        used = sum(cw(x[0]) for x in segs + right)
        rows.append(segs + [(" " * (W - used), st(None, bg))] + right)
    title = "╭─ Select model "
    s11 = a_history() + [boundary()] + [
        L((title, st(DIM)), (" " * 0, ""), ("─" * (inner - cw(title) + 1) + "╮", st(DIM))),
        [("│", st(DIM)), ("  ⌕ ", st(MUTED)), ("▌", st(PINK), "cursor"), ("type to filter", st(DIM, None, False, True)), (" " * (inner - 19), ""), ("│", st(DIM))],
    ] + rows + [L(("╰" + "─" * inner + "╯", st(DIM))),
                L(("   enter select  ·  esc close", st(DIM, None, False, True)))] + a_footer()
    return {"S3": s3, "S6": s6, "S7": s7, "S11": s11}


# ------------------------------------------------------------------ direction B: Rail

RAIL = DIM


def b_header():
    return [L(("◢◤ ", st(PINK, None, True)), grad("ask"), ("   v0.1.0 · leader 7c1e · ~/src/askcore", st(MUTED))), L()]


def b_turn(q, a, footer=True, code=True, time="14:02", dur="4.2s"):
    out = [
        L(("╭─ ", st(RAIL)), ("you", st(PINK, None, True)), (f"  {time}", st(MUTED))),
        L(("│  ", st(RAIL)), (q, st(FG, None, True))),
        L(("│", st(RAIL))),
        L(("├─ ", st(RAIL)), ("ask", st(VIOLET, None, True)), (f"  deepseek-v4.1-flash · {dur}", st(MUTED))),
    ]
    for l in wrap(a, 76):
        out.append(L(("│  ", st(RAIL)), (l, st(FG))))
    if code:
        out += [L(("│  ", st(RAIL)), ("┃ ", st(ORANGE)), ("deadline := 5 * time.Second", st(ORANGE)))]
    if footer:
        out += [L(("╰─ ", st(RAIL)), ("↑12.4k ↓1.8k · $0.004", st(DIM)))]
    out.append(L())
    return out


def b_live_turn(tool_done=False):
    out = [
        L(("╭─ ", st(RAIL)), ("you", st(PINK, None, True)), ("  14:03", st(MUTED))),
        L(("│  ", st(RAIL)), (Q2, st(FG, None, True))),
        L(("│", st(RAIL))),
        L(("├─ ", st(RAIL)), ("ask", st(VIOLET, None, True)), ("  deepseek-v4.1-flash", st(MUTED))),
    ]
    if tool_done:
        out.append(L(("│  ", st(RAIL)), ("◆ echo", st(TEAL, None, True)), ("  probe code request", st(FG)), right=[("✓ 0.1s", st(GREEN))]))
    return out


def b_live_tail(tool_running=True):
    out = []
    if tool_running:
        out.append(L(("│  ", st(RAIL)), ("◇ echo", st(TEAL, None, True)), ("  probe code request", st(FG)), right=[("running ⠋", st(YELLOW))]))
    for l in wrap(A2, 76):
        out.append(L(("│  ", st(RAIL)), (l, st(FG))))
    out.append(L(("┊  ", st(PINK)), ("◌ ", st(PINK, None, True)), ("working", st(PINK, None, True), "shimmer"), ("  4s · esc to interrupt", st(MUTED))))
    return out


def b_editor():
    return [
        L(("╭─ ", st(PINK)), ("next", st(PINK, None, True)), ("  enter send · shift+enter newline · / commands", st(DIM))),
        L(("╰❯ ", st(PINK, None, True)), ("▌", st(PINK), "cursor"), ("Ask anything", st(DIM, None, False, True))),
    ]


def b_footer():
    return [L((" ● leader 7c1e ", st(BG, GREEN, True)), (" ", ""), (" ~/src/askcore ", st(FG, PANEL2)),
              right=[(" deepseek-v4.1-flash ", st(BG, VIOLET, True)), (" ", ""), (" ctx 18% ", st(FG, PANEL2)), (" $0.04 ", st(FG, PANEL))])]


def B():
    hist = b_header() + b_turn(Q1, A1)
    s3 = hist + [boundary()] + b_editor() + b_footer()
    s6 = hist + b_live_turn() + [boundary()] + b_live_tail() + [L()] + b_editor() + b_footer()
    s7 = hist + b_live_turn(tool_done=True) + [boundary()] + b_live_tail(False) + [
        L(("┆  ", st(VIOLET)), ("⧗ steer", st(VIOLET, None, True)), ("   use the fake clock from internal/testsupport", st(FG))),
        L(("┆  ", st(TEAL)), ("⧗ next ", st(TEAL, None, True)), ("   then run the leader tests", st(FG))),
        L(("┆  ", st(DIM)), ("alt+↑ edit queue · dashed rail = not sent yet", st(DIM, None, False, True))),
        L(),
    ] + b_editor() + b_footer()
    rows = []
    for k, (p, m, ctx, note) in enumerate(MODELS):
        sel = k == 2
        bg = PANEL2 if sel else None
        rows.append(L(("│ ", st(PINK)), ("❯ " if sel else "  ", st(PINK, bg, True)), ("● ", st(PCOL[p], bg)),
                      (f"{p}/{m}", st(FG, bg, sel)), fill=bg,
                      right=[(f"{ctx:>5}  {note:<10} ", st(MUTED, bg))]))
    s11 = hist + [boundary()] + [
        L(("╭─ ", st(PINK)), ("model", st(PINK, None, True)), ("  ⌕ ", st(MUTED)), ("▌", st(PINK), "cursor"), ("type to filter", st(DIM, None, False, True))),
    ] + rows + [L(("╰─ ", st(PINK)), ("enter select · esc close", st(DIM, None, False, True)))] + b_footer()
    return {"S3": s3, "S6": s6, "S7": s7, "S11": s11}


# ------------------------------------------------------------------ direction B2: Rail without role labels

def b2_turn(q, a, footer=True, code=True, time="14:02", dur="4.2s"):
    out = [
        L(("╭❯ ", st(PINK, None, True)), (q, st(FG, None, True)), right=[(time, st(DIM))]),
        L(("│", st(RAIL))),
        L(("├─ ", st(RAIL)), ("◆ ", st(VIOLET, None, True)), (f"deepseek-v4.1-flash · {dur}", st(MUTED))),
    ]
    for l in wrap(a, 76):
        out.append(L(("│  ", st(RAIL)), (l, st(FG))))
    if code:
        out += [L(("│  ", st(RAIL)), ("┃ ", st(ORANGE)), ("deadline := 5 * time.Second", st(ORANGE)))]
    if footer:
        out += [L(("╰─ ", st(RAIL)), ("↑12.4k ↓1.8k · $0.004", st(DIM)))]
    out.append(L())
    return out


def b2_live_turn(tool_done=False):
    out = [
        L(("╭❯ ", st(PINK, None, True)), (Q2, st(FG, None, True)), right=[("14:03", st(DIM))]),
        L(("│", st(RAIL))),
        L(("├─ ", st(RAIL)), ("◆ ", st(VIOLET, None, True)), ("deepseek-v4.1-flash", st(MUTED))),
    ]
    if tool_done:
        out.append(L(("│  ", st(RAIL)), ("◆ echo", st(TEAL, None, True)), ("  probe code request", st(FG)), right=[("✓ 0.1s", st(GREEN))]))
    return out


def b2_editor():
    return [
        L(("╭─ ", st(PINK)), ("enter send · shift+enter newline · / commands", st(DIM))),
        L(("╰❯ ", st(PINK, None, True)), ("▌", st(PINK), "cursor"), ("Ask anything", st(DIM, None, False, True))),
    ]


def B2():
    hist = b_header() + b2_turn(Q1, A1)
    s3 = hist + [boundary()] + b2_editor() + b_footer()
    s6 = hist + b2_live_turn() + [boundary()] + b_live_tail() + [L()] + b2_editor() + b_footer()
    s7 = hist + b2_live_turn(tool_done=True) + [boundary()] + b_live_tail(False) + [
        L(("┆  ", st(VIOLET)), ("⧗ steer", st(VIOLET, None, True)), ("   use the fake clock from internal/testsupport", st(FG))),
        L(("┆  ", st(TEAL)), ("⧗ next ", st(TEAL, None, True)), ("   then run the leader tests", st(FG))),
        L(("┆  ", st(DIM)), ("alt+↑ edit queue · dashed rail = not sent yet", st(DIM, None, False, True))),
        L(),
    ] + b2_editor() + b_footer()
    s11 = B()["S11"]
    s11 = hist + s11[s11.index("BOUNDARY"):]
    return {"S3": s3, "S6": s6, "S7": s7, "S11": s11}


# ------------------------------------------------------------------ direction B3: Soft messages, rail only for live and pending parts

def b3_user(q, time):
    return [L(("  ", st(None, PANEL)), ("❯ ", st(PINK, PANEL, True)), (q, st(FG, PANEL, True)), fill=PANEL,
              right=[(time + "  ", st(DIM, PANEL))]), L()]


def b3_meta(dur="4.2s"):
    return [L(("    deepseek-v4.1-flash · " + dur + " · ↑12.4k ↓1.8k · $0.004", st(DIM))), L()]


def b3_history():
    return b_header() + b3_user(Q1, "14:02") + a_answer(A1) + a_code()[:-1] + b3_meta()


def b3_live_tail(tool_running=True):
    out = []
    if tool_running:
        out += [L(("  ◇ ", st(TEAL, None, True)), ("echo", st(TEAL, None, True)), ("  probe code request", st(FG)), right=[("running ⠋ ", st(YELLOW))])]
    out += a_answer(A2)
    out += [L(), L(("┊ ", st(PINK)), ("◌ ", st(PINK, None, True)), ("working", st(PINK, None, True), "shimmer"), ("  4s · esc to interrupt", st(MUTED)))]
    return out


def B3():
    hist = b3_history()
    s3 = hist + [boundary()] + b2_editor() + b_footer()
    s6 = hist + b3_user(Q2, "14:03") + [boundary()] + b3_live_tail() + [L()] + b2_editor() + b_footer()
    done = [L(("  ◆ ", st(TEAL, None, True)), ("echo", st(TEAL, None, True)), ("  probe code request", st(FG)), right=[("✓ 0.1s ", st(GREEN))])]
    s7 = hist + b3_user(Q2, "14:03") + done + [boundary()] + b3_live_tail(False) + [
        L(("┆ ", st(VIOLET)), ("⧗ steer", st(VIOLET, None, True)), ("   use the fake clock from internal/testsupport", st(FG))),
        L(("┆ ", st(TEAL)), ("⧗ next ", st(TEAL, None, True)), ("   then run the leader tests", st(FG))),
        L(("┆ ", st(DIM)), ("alt+↑ edit queue · dashed rail = not sent yet", st(DIM, None, False, True))),
        L(),
    ] + b2_editor() + b_footer()
    s11 = B()["S11"]
    s11 = hist + s11[s11.index("BOUNDARY"):]
    return {"S3": s3, "S6": s6, "S7": s7, "S11": s11}


# ------------------------------------------------------------------ direction B4: B3 with the Soft editor box and Soft footer

def B4():
    hist = b3_history()
    s3 = hist + [boundary()] + a_editor() + a_footer()
    s6 = hist + b3_user(Q2, "14:03") + [boundary()] + b3_live_tail() + a_editor() + a_footer()
    done = [L(("  ◆ ", st(TEAL, None, True)), ("echo", st(TEAL, None, True)), ("  probe code request", st(FG)), right=[("✓ 0.1s ", st(GREEN))])]
    s7 = hist + b3_user(Q2, "14:03") + done + [boundary()] + b3_live_tail(False) + [
        L(("┆ ", st(VIOLET)), ("⧗ steer", st(VIOLET, None, True)), ("   use the fake clock from internal/testsupport", st(FG))),
        L(("┆ ", st(TEAL)), ("⧗ next ", st(TEAL, None, True)), ("   then run the leader tests", st(FG))),
        L(("┆ ", st(DIM)), ("alt+↑ edit queue · dashed rail = not sent yet", st(DIM, None, False, True))),
    ] + a_editor() + a_footer()
    s11 = A()["S11"]
    s11 = hist + s11[s11.index("BOUNDARY"):]
    return {"S3": s3, "S6": s6, "S7": s7, "S11": s11}


# ------------------------------------------------------------------ direction C: Bold blocks

def c_header():
    return [
        L((" ", ""), grad("▄▀█ █▀ █▄▀", (VIOLET, PINK, ORANGE)), ("   v0.1.0", st(MUTED))),
        L((" ", ""), grad("█▀█ ▄█ █ █", (VIOLET, PINK, ORANGE)), ("   leader 7c1e · ~/src/askcore", st(MUTED))),
        L(),
    ]


def c_user(q):
    return [L((" YOU ", st(BG, PINK, True)), ("  " + q, st(FG, None, True))), L()]


def c_answer(text):
    out = []
    for k, l in enumerate(wrap(text, 73)):
        out.append(L((" ASK " if k == 0 else "     ", st(BG, VIOLET, True) if k == 0 else ""), ("  " + l, st(FG))))
    return out


def c_code():
    return [L(), L(("       ", ""), (" go ", st(BG, ORANGE, True)), ("  deadline := 5 * time.Second", st(ORANGE, PANEL)), fill=PANEL), L()]


def c_history():
    return c_header() + c_user(Q1) + c_answer(A1) + c_code()


def c_editor():
    return [
        L(("▌", st(PINK)), fill=PANEL),
        L(("▌", st(PINK)), ("  ", st(None, PANEL)), ("▌", st(PINK, PANEL), "cursor"), ("Ask anything", st(DIM, PANEL, False, True)), fill=PANEL,
          right=[("/ commands  ctrl+l model ", st(DIM, PANEL))]),
        L(("▌", st(PINK)), fill=PANEL),
    ]


def c_footer():
    return [L((" ◉ LEADER 7c1e ", st(BG, GREEN, True)), (" ~/src/askcore ", st(FG, "#2c2f44")), fill=PANEL,
              right=[(" DEEPSEEK-V4.1-FLASH ", st(BG, VIOLET, True)), (" 18% ", st(BG, PINK, True)), (" $0.04 ", st(BG, ORANGE, True))])]


def c_status(t="4s"):
    return [L((" ⣾ WORKING ", st(BG, PINK, True), "shimmerbg"), (f"  {t}", st(FG, PANEL)), ("  esc interrupt", st(MUTED, PANEL)), fill=PANEL)]


def C():
    s3 = c_history() + [boundary()] + c_editor() + c_footer()
    tool = [L((" ⚙ ECHO ", st(BG, TEAL, True)), ("  probe code request", st(FG)), right=[("● running", st(YELLOW, None, True))])]
    s6 = c_history() + c_user(Q2) + [boundary()] + tool + [L()] + c_answer(A2) + [L()] + c_status() + c_editor() + c_footer()
    tool_done = [L((" ⚙ ECHO ", st(BG, TEAL, True)), ("  probe code request", st(FG)), right=[("✓ 0.1s", st(GREEN, None, True))]), L()]
    s7 = c_history() + c_user(Q2) + tool_done + [boundary()] + c_answer(A2) + [L(),
        L((" STEER ", st(BG, VIOLET, True)), ("  use the fake clock from internal/testsupport", st(FG))),
        L((" NEXT  ", st(BG, TEAL, True)), ("  then run the leader tests", st(FG))),
        L(("        alt+↑ edit queue", st(DIM, None, False, True))),
    ] + c_status("9s") + c_editor() + c_footer()
    rows = []
    for k, (p, m, ctx, note) in enumerate(MODELS):
        sel = k == 2
        bg = PINK if sel else PANEL
        fgc = BG if sel else FG
        rows.append(L((" ", st(None, bg)), (f" {p[:3].upper()} ", st(BG, PCOL[p], True)), (f"  {m}", st(fgc, bg, True)), fill=bg,
                      right=[(f"{ctx:>5}  {note:<10} ", st(fgc if sel else MUTED, bg))]))
    s11 = c_history() + [boundary()] + [
        L((" MODEL ", st(BG, PINK, True)), ("  ⌕ ", st(MUTED, PANEL)), ("▌", st(PINK, PANEL), "cursor"), ("type to filter", st(DIM, PANEL, False, True)), fill=PANEL),
    ] + rows + [L(("  enter select · esc close", st(DIM, PANEL, False, True)), fill=PANEL)] + c_footer()
    return {"S3": s3, "S6": s6, "S7": s7, "S11": s11}


# ------------------------------------------------------------------ page

DIRECTIONS = [
    ("A", "Soft", "Mềm, gọn, quen thuộc",
     ["Editor là hộp bo góc ╭╮, có chữ gợi ý mờ.", "Prompt của bạn nằm trên dải nền riêng; câu trả lời mở đầu bằng ◆ tím.",
      "Status có chữ Working… lấp lánh (shimmer), tool có nhánh ⎿ như Claude Code.",
      "Footer 1 dòng: chấm xanh leader, model tím, thanh ngữ cảnh ▰▰▱▱."],
     "Gần Claude Code / Gemini CLI. An toàn, dễ đọc, ít rủi ro."),
    ("B", "Rail", "Dòng thời gian, sáng tạo nhất",
     ["Mỗi lượt hỏi đáp là một nhánh trên một đường ray dọc ╭─ │ ├─ ╰─.",
      "Ray liền = đã gửi; ray đứt ┊ ┆ = đang chạy hoặc đang xếp hàng (chưa gửi).",
      "Editor là nút cuối của ray: ╰❯. Footer là các huy hiệu có nền màu.",
      "Cuối mỗi lượt có token và chi phí của riêng lượt đó."],
     "Mới lạ, kể chuyện tốt. Copy văn bản sẽ dính ký tự │ ở đầu dòng."),
    ("B2", "Rail, no labels", "B đã chỉnh theo ý bạn",
     ["Bỏ chữ you / ask. Prompt của bạn nằm ngay trên nút mở ╭❯ (hồng, đậm), giờ gửi ở mép phải.",
      "Câu trả lời mở bằng nhánh ├─ ◆ kèm model và thời gian chạy.",
      "Ray liền = đã gửi; ray đứt ┊ ┆ = đang chạy hoặc đang xếp hàng.",
      "Mỗi lượt ngắn hơn bản B một dòng."],
     "Bản đề xuất để áp cho toàn bộ thiết kế."),
    ("B3", "Soft messages + rail", "B2 với hội thoại kiểu Soft",
     ["Hội thoại giống Soft: prompt trên dải nền có ❯ (giờ gửi ở mép phải), câu trả lời mở bằng ◆ tím, code trên nền riêng.",
      "Dưới mỗi câu trả lời có một dòng mờ: model · thời gian · token · chi phí.",
      "Ray chỉ còn ở phần đang diễn ra: ┊ working khi đang chạy, ┆ steer / next khi đang xếp hàng (chưa gửi).",
      "Editor ╭─ / ╰❯, header ◢◤ ask và footer huy hiệu giữ như B2."],
     "Bản đề xuất mới để áp cho toàn bộ thiết kế."),
    ("B4", "Soft + live rail", "B3 với ô nhập và footer kiểu Soft",
     ["Hội thoại, ô nhập (hộp bo góc ╭╮ có chữ gợi ý) và footer 1 dòng (● leader, model, ▰▰▱▱, chi phí) đều như Soft.",
      "Dưới mỗi câu trả lời có một dòng mờ: model · thời gian · token · chi phí.",
      "Ray đứt chỉ còn ở phần đang diễn ra: ┊ working khi đang chạy, ┆ steer / next khi đang xếp hàng (chưa gửi).",
      "Header ◢◤ ask giữ như B; selector model dùng hộp bo góc của Soft."],
     "Bản đề xuất mới nhất."),
    ("C", "Bold blocks", "Mạnh, trẻ, nhiều màu",
     ["Logo ASK chữ khối có màu chuyển sắc.", "Nhãn YOU / ASK / ECHO / STEER là các khối nền màu.",
      "Editor là tấm nền tối có thanh hồng ▌ bên trái (không viền hộp).",
      "Footer là thanh trạng thái kín bề ngang, các ô màu như powerline."],
     "Trẻ trung, nổi bật nhất. Nhiều màu nền nên cần terminal truecolor để đẹp."),
]
STATES = [("S3", "Chờ nhập (đã có một lượt hỏi đáp)"), ("S6", "Đang stream, tool đang chạy"),
          ("S7", "Đang chạy, có steer và follow-up trong hàng đợi"), ("S11", "Chọn model")]


def render(lines):
    out = []
    for ln in lines:
        if ln == "BOUNDARY":
            out.append('<div class="boundary"><span>live region</span></div>')
            continue
        total = sum(cw(s[0]) for s in ln)
        if total != W:
            raise SystemExit(f"width {total}: {''.join(s[0] for s in ln)!r}")
        spans = []
        for s in ln:
            cls = s[2] if len(s) > 2 else ""
            spans.append(f'<span class="{cls}" style="{s[1]}">{html.escape(s[0])}</span>')
        out.append('<div class="row">' + "".join(spans) + "</div>")
    return f'<div class="term">' + "\n".join(out) + "</div>"


def main():
    dirs = {"A": A(), "B": B(), "B2": B2(), "B3": B3(), "B4": B4(), "C": C()}
    tabs, panels = [], []
    for code, name, tag, points, note in DIRECTIONS:
        tabs.append(f'<button onclick="show(\'{code}\')" id="t{code}">{code} · {name}<small>{tag}</small></button>')
        body = [f'<section class="dir" id="d{code}"><h2>Hướng {code}: {name}</h2><ul>' +
                "".join(f"<li>{html.escape(p)}</li>" for p in points) + f'</ul><p class="note">{html.escape(note)}</p>']
        for sid, title in STATES:
            body.append(f'<h3>{sid} · {html.escape(title)}</h3>' + render(dirs[code][sid]))
        body.append("</section>")
        panels.append("".join(body))
    page = f"""<!doctype html><html lang="vi"><head><meta charset="utf-8"><title>T1a style directions</title>
<style>
body {{ margin:0; background:#0b0c11; color:#cfd2e3; font-family: system-ui, sans-serif; }}
header {{ position:sticky; top:0; background:#0b0c11ee; backdrop-filter: blur(6px); padding:14px 28px; border-bottom:1px solid #22243a; z-index:2; }}
header h1 {{ font-size:18px; margin:0 0 8px; }}
header p {{ margin:0 0 10px; font-size:13px; color:#8b8fa8; }}
button {{ background:#1a1c28; color:#cfd2e3; border:1px solid #2c2f44; border-radius:10px; padding:8px 14px; margin-right:8px; cursor:pointer; font-size:14px; }}
button small {{ display:block; font-size:11px; color:#8b8fa8; }}
button.on {{ border-color:{PINK}; box-shadow:0 0 0 1px {PINK} inset; }}
main {{ padding:8px 28px 60px; }}
.dir {{ display:none; }} .dir.on {{ display:block; }}
h2 {{ font-size:17px; margin:20px 0 6px; }} h3 {{ font-size:14px; margin:26px 0 8px; color:#aeb2c9; }}
ul {{ margin:0; padding-left:20px; font-size:14px; line-height:1.6; }} .note {{ color:#8b8fa8; font-size:13px; }}
.term {{ background:{BG}; color:{FG}; font:13.5px/1.42 "JetBrains Mono","SF Mono",Menlo,monospace; padding:14px 16px;
         border-radius:12px; width:80ch; box-shadow:0 10px 40px rgba(0,0,0,.5); border:1px solid #23253a; }}
.row {{ white-space:pre; min-height:1.42em; }}
.cursor {{ animation: blink 1s steps(1) infinite; }} @keyframes blink {{ 50% {{ opacity:0; }} }}
.shimmer {{ background: linear-gradient(90deg,{PINK},{ORANGE},#fff,{VIOLET},{PINK}); background-size:300% 100%;
            -webkit-background-clip:text; background-clip:text; color:transparent !important; animation: sh 2.2s linear infinite; }}
.shimmerbg {{ animation: pulse 1.6s ease-in-out infinite; }}
@keyframes sh {{ to {{ background-position: -300% 0; }} }} @keyframes pulse {{ 50% {{ filter: brightness(1.25); }} }}
.boundary {{ position:relative; border-top:1px dashed #3a3d55; margin:3px -16px; height:0; }}
.boundary span {{ position:absolute; right:10px; top:-9px; font:10px system-ui; color:#7d81a0; background:{BG}; padding:0 4px; }}
</style></head><body>
<header><h1>T1a · 3 hướng giao diện</h1>
<p>Cùng nội dung, cùng 4 trạng thái, khổ 80 cột. Màu động (shimmer, nhấp nháy) mô phỏng hiệu ứng trong terminal. Đường gạch đứt chỉ để chỉ ranh giới vùng live, không có trên màn hình.</p>
{"".join(tabs)}</header>
<main>{"".join(panels)}</main>
<script>
function show(c){{document.querySelectorAll('.dir').forEach(e=>e.classList.toggle('on',e.id=='d'+c));
document.querySelectorAll('header button').forEach(e=>e.classList.toggle('on',e.id=='t'+c));}}
show('B4');
</script></body></html>"""
    (here / "style-directions.html").write_text(page)
    print(here / "style-directions.html")


if __name__ == "__main__":
    main()
