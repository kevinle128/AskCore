#!/usr/bin/env python3
"""Generate exact-width sketches for the account model discovery TUI design.

Screen model: the alternate screen of plans/261012-1337-t1b-grok-style-altscreen/design.md, with the
proposed change of this design: no header row at the top; the status data is a footer row above the
shortcut bar. The footer row, selector rows, model line and fallback note copy the rules of the current
code: internal/tui/header.go (headerText, plus one space at the left), internal/tui/selector.go (itemRow,
slot, formatWindow),
internal/tui/model_line.go (fallbackReasonText; the start and change lines go) and internal/tui/width.go
(wrapLines).

Each screen row is (region, segments). A segment is (text, roles). design.md uses the plain
text; preview.html renders the roles as colors and the region names beside the terminal.
Both come from this file, so they cannot disagree.

Usage: python3 sketch-gen.py > sketches.md
"""
import unicodedata

# ---------------------------------------------------------------- width helpers


def cw(s):
    n = 0
    for c in s:
        if unicodedata.combining(c):
            continue
        n += 2 if unicodedata.east_asian_width(c) in "WF" else 1
    return n


def trunc(s, n):
    """truncateTo: cut to n columns with the marker "…"."""
    if n <= 0:
        return ""
    if cw(s) <= n:
        return s
    out = ""
    for c in s:
        if cw(out + c) > n - 1:
            break
        out += c
    return out + "…"


def wrap(text, w):
    """wrapLines for plain words: break at spaces; a word longer than a row breaks at the limit."""
    rows, cur = [], ""
    for word in text.split(" "):
        if cur == "":
            cand = word
        else:
            cand = cur + " " + word
        if cw(cand) <= w:
            cur = cand
            continue
        if cur:
            rows.append(cur)
        cur = word
        while cw(cur) > w:
            rows.append(cur[:w])
            cur = cur[w:]
    rows.append(cur)
    return rows


def L(w, *segs, right=None, fill=""):
    """One row: left segments, optional right-aligned segments, padded to w with the fill role."""
    left, rgt = list(segs), list(right or [])
    used = sum(cw(t) for t, _ in left) + sum(cw(t) for t, _ in rgt)
    if used > w:
        raise SystemExit(f"row too wide ({used}>{w}): {''.join(t for t, _ in left + rgt)!r}")
    return left + [(" " * (w - used), fill)] + rgt


def plain(row):
    return "".join(t for t, _ in row)


# ---------------------------------------------------------------- footer row (header.go today)

SHORT = 60  # shortWidth in internal/tui/status.go
CWD = "~/src/askcore"


def last_element(path):
    p = path.rstrip("/")
    root = "~/" if p.startswith("~/") else "/"
    i = p.rfind("/")
    if i < len(root):
        return path
    return root + "…" + p[i:]


def bar(pct):
    filled = min(8, max(0, (pct * 8 + 50) // 100))
    return "▰" * filled + "▱" * (8 - filled)


def usage_role(pct):
    return "error" if pct >= 90 else "warning" if pct >= 70 else "accent"


HEADER_RULE = True  # a rule under the slim header row (D16)


def header_row(w):
    """The slim header row: one space of padding, then the logo. Nothing else (D14, D15)."""
    return L(w, (" ", ""), ("◢◤ ask", "logo"))


def footer(w, model, level="", pct=None, no_model=False, cwd_path=CWD):
    """The footer row: directory (from 60 columns), "<provider>/<model>" with its thinking level, the
    context meter. No logo, no link state, no session cost. One space of padding at the left.

    Narrowing order: (a) the directory shortens to its last element; it is never removed from 60
    columns; (b) the end of the model id is cut with "…", the provider stays whole; (c) last resort:
    the model text is cut from its end, provider included.
    pct=None means the context window is unknown: the meter is "?".
    model="" is a session whose state is not known yet: the right side is the meter only."""
    short = w < SHORT
    left = [(" ", "")]
    if short:
        meter = [("?" if pct is None else f"{pct}%", "muted" if pct is None else usage_role(pct)), (" ", "")]
        sep = " "
    else:
        ctx = ("?", "muted") if pct is None else (bar(pct) + f" {pct}%", usage_role(pct))
        meter = [ctx, (" ", "")]
        sep = "  "
    lvl = "" if short or not level else " ◐ " + level

    def right(name):
        if no_model:
            return [("no model ", "muted")]
        if not name:
            return list(meter)
        r = [(name, "assistant")]
        if lvl:
            r.append((lvl, "assistant"))
        return r + [(sep, "")] + meter

    def width(segs):
        return sum(cw(t) for t, _ in segs)

    opts = [[]] if short else [[("⌂ " + cwd_path, "muted")], [("⌂ " + last_element(cwd_path), "muted")]]
    for cwd in opts:
        lft = left + cwd
        if width(lft) + 1 + width(right(model)) <= w:
            return L(w, *lft, right=right(model))
    lft = left + opts[-1]
    if no_model or not model:
        return L(w, *lft, right=right(model))
    room = w - width(lft) - 1 - (width(right("x")) - 1)
    return L(w, *lft, right=right(trunc(model, max(1, room))))


def rule(w):
    return L(w, ("─" * w, "border"))


# ---------------------------------------------------------------- shortcut bar and prompt pane

def bar_row(w):
    text = "Tab blocks · / commands" if w < SHORT else "Tab blocks · ctrl+t thinking · ctrl+l model · / commands"
    if cw(text + " · shift/option+drag select") < w:
        text += " · shift/option+drag select"
    return L(w, (" " + trunc(text, w - 1), "dim"))


def prompt_row(w):
    ph = "Ask anything" if w < SHORT else "Ask anything  ·  / commands  ·  ctrl+l model"
    return L(w, (" ", ""), ("❯ ", "accent bold"), ("▌", "cursor"), (ph, "dim italic"))


# ---------------------------------------------------------------- block pane content

def fallback_notice(w, text):
    """The fallback notice: its own block, no glyph, text in muted, indented 2 columns, wrapped at
    spaces (wrapLines), then one empty row. It replaces the reason row under the old start line."""
    rows = [L(w, ("  ", ""), (r, "muted")) for r in wrap(text, w - 2)]
    return rows + [L(w)]


def wrap_hanging(text, first, rest):
    """wrapHanging: the first row is narrower than the others."""
    rows = wrap(text, first)
    if len(rows) > 1:
        rows = rows[:1] + wrap(" ".join(rows[1:]), rest)
    return rows


def user(w, q, time):
    """userPromptBlock: indent 4, two spaces at the right, the send time on the first row."""
    avail = max(1, w - 4 - 2)
    rows = wrap_hanging(q, max(1, avail - cw(time) - 1), avail)
    out = [L(w, ("  ", "panel"), ("❯ ", "accent bold panel"), (rows[0], "user panel"),
             right=[(time + "  ", "dim panel")], fill="panel")]
    for r in rows[1:]:
        out.append(L(w, ("    ", "panel"), (r, "user panel"), fill="panel"))
    return out + [L(w)]


def reply(w, text, meta):
    rows = [L(w, ("  " + r, "")) for r in wrap(text, w - 2)]
    return rows + [L(w, (trunc("  " + meta, w), "dim")), L(w)]


def status(w, glyph, glyph_role, text):
    """noticeRow of a result: dim rail, the glyph, dim text; the layout cuts the row at the width."""
    return L(w, ("┊ ", "dim"), (glyph, glyph_role), (trunc(" " + text, w - 3), "dim"))


def step(w, verb, what, right="", right_role="", glyph_role="tool"):
    """Step row: "  ◆ <Verb> <what>" and the right column; the description is cut first."""
    lead = [("  ", ""), ("◆", glyph_role), (" ", ""), (verb, "bold")]
    rgt = [(right, right_role)] if right else []
    room = w - 4 - cw(verb) - 1 - (cw(right) + 1 if right else 0)
    return L(w, *lead, (" " + trunc(what, room), ""), right=rgt)


def thought(w, text):
    return L(w, ("  ◆ " + text, "thinking"))


def queue(w, kind, role, text):
    head = f"┆ ⧗ {kind:<7} "
    return L(w, ("┆ ", "dim"), ("⧗ " + f"{kind:<7} ", role), (trunc(text, w - cw(head)), role))


def queue_hint(w):
    return L(w, ("┆ ", "dim"), (trunc("alt+↑ edit queue · dashed rail = not sent yet", w - 2), "dim italic"))


def working(w, secs):
    tail = f" {secs}s · esc to interrupt" if w >= SHORT else f" {secs}s · esc"
    return L(w, ("┊ ", "dim"), ("◌ ", "accent"), ("Working…", "accent bold"), (tail, "muted"))


# ---------------------------------------------------------------- selector (selector.go)

WINDOW_COL, TAG_COL = 4, 9
HINT = "enter select  ·  esc close  ·  your draft is kept"


def fmt_window(n):
    """formatWindow with the change of this design: 0 (unknown) is "?"."""
    if n <= 0:
        return "?"
    if n >= 1_000_000 and n % 1_000_000 == 0:
        return f"{n // 1_000_000}M"
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    return f"{n // 1000}k"


def selector(w, h, groups, sel_id, current, default=None, not_in_account=()):
    """The /model selector in the prompt pane slot. groups: [(provider, role, [(id, window, reasoning)])].

    sel_id, current, default and not_in_account use "provider/id"."""
    inner = w - 4
    slot_h = max(1, h // 2)
    budget = min(8, max(3, min(12, slot_h) - 4))

    def boxed(*segs):
        used = sum(cw(t) for t, _ in segs)
        return [("│", "border"), (" ", "")] + list(segs) + [(" " * (inner - used), ""), (" ", ""), ("│", "border")]

    title = " Select model "
    rows = [[("╭─", "border"), (title, "accent bold"), ("─" * (w - 2 - cw(title) - 1) + "╮", "border")]]
    rows.append(boxed((" ⌕ ", ""), ("▌", "cursor"), ("type to filter", "dim italic")))

    disp = []  # (kind, data)
    for prov, role, models in groups:
        disp.append(("header", (prov, role)))
        for mid, window, reasoning in models:
            disp.append(("model", (prov, mid, window, reasoning)))
    def pos(full):
        return next(i for i, (k, d) in enumerate(disp) if k == "model" and d[0] + "/" + d[1] == full)

    sel_pos, cur_pos = pos(sel_id), pos(current)
    shown, hidden = len(disp), 0
    if shown > budget:
        shown = budget - 1
        hidden = len(disp) - shown
    # The window follows the selection from the row of the current model, as it does after
    # the arrow keys move from the start selection.
    top = 0
    if cur_pos >= top + shown:
        top = cur_pos - shown + 1
    step = -1 if sel_pos < cur_pos else 1
    for p in range(cur_pos, sel_pos + step, step):
        if disp[p][0] != "model":
            continue
        if p < top:
            top = p
        if p >= top + shown:
            top = p - shown + 1
    top = max(0, min(top, len(disp) - shown))

    for i in range(top, min(top + shown, len(disp))):
        kind, d = disp[i]
        if kind == "header":
            prov, role = d
            rows.append(boxed(("  ", ""), ("●", role), (" ", ""), (prov, "muted bold")))
            continue
        prov, mid, window, reasoning = d
        full = prov + "/" + mid
        if full == current:
            tag = "current"
        elif default and full == default:
            tag = "default"
        elif reasoning:
            tag = "reasoning"
        else:
            tag = ""
        right_text = fmt_window(window).rjust(WINDOW_COL) + "  " + tag.ljust(TAG_COL) + " "
        right_w = cw(right_text)
        selected = full == sel_id
        mark = "❯ " if selected else "  "
        left_room = max(1, inner - right_w - 1)
        label = trunc("  " + mid, max(1, left_room - cw(mark)))
        suffix = ""
        if full in not_in_account:
            s = " · not in account list"
            # From 60 columns the note shows when it fits after the id. Under 60 columns, or when it
            # does not fit, the tag "unlisted" replaces "current"; the id is never cut for the note.
            if w >= SHORT and cw(mark + label + s) <= left_room:
                suffix = s
            else:
                right_text = fmt_window(window).rjust(WINDOW_COL) + "  " + "unlisted".ljust(TAG_COL) + " "
        left_plain = mark + label + suffix
        gap = max(1, inner - cw(left_plain) - right_w)
        if selected:
            rows.append(boxed((left_plain + " " * gap + right_text, "selected")))
        else:
            segs = [(mark + label, "")]
            if suffix:
                segs.append((suffix, "muted"))
            segs += [(" " * gap, ""), (right_text, "muted")]
            rows.append(boxed(*segs))
    if hidden:
        rows.append(boxed((f"   … {hidden} more", "dim")))
    rows.append([("╰" + "─" * (w - 2) + "╯", "border")])
    hint = "   " + trunc(HINT, w - 3)
    rows.append(L(w, (hint, "dim italic")))
    return [L(w, *r) if cw(plain(r)) < w else r for r in rows]


# ---------------------------------------------------------------- screen composition (layout.go, proposed)

def screen(w, h, foot, pane, queue_rows=(), status_rows=(), slot=None):
    """Slim header row, rule, block pane, queue, status, rule, prompt pane or selector, rule, footer row,
    shortcut bar.
 The pane is top-anchored and keeps the end of its
    content when it is too long (follow mode)."""
    slot_rows = slot if slot is not None else [prompt_row(w)]
    top = [("header", header_row(w))] + ([("rule", rule(w))] if HEADER_RULE else [])
    fixed = len(top) + len(queue_rows) + len(status_rows) + 1 + len(slot_rows) + 1 + 1 + 1
    n = h - fixed
    body = list(pane)
    if len(body) > n:
        body = body[len(body) - n:]
    body += [L(w)] * (n - len(body))
    out = top + [("block pane", r) for r in body]
    out += [("queue", r) for r in queue_rows]
    out += [("status", r) for r in status_rows]
    out += [("rule", rule(w))]
    out += [("selector" if slot is not None else "prompt pane", r) for r in slot_rows]
    out += [("rule", rule(w)), ("footer", foot), ("bar", bar_row(w))]
    assert len(out) == h, (len(out), h)
    for _, r in out:
        if cw(plain(r)) != w:
            raise SystemExit(f"width {cw(plain(r))} != {w}: {plain(r)!r}")
    return out


# ---------------------------------------------------------------- example data

Q1, T1 = "why does TestLeaderStale fail on CI?", "14:02"
A1 = ("The test waits for the stale socket check, but CI runs it with a 1 s deadline. "
      "The leader removes the stale socket after the lock probe, and the probe can take 1.2 s.")
M1 = "gpt-5.4-mini · 4.2s · ↑12.4k ↓1.8k · $0.040"
Q3, T3 = "list the open TODOs in internal/leader", "09:41"
A3 = ("There are three: the stale socket probe, the lock timeout on Linux and the observer "
      "hand-off note. None of them blocks the release.")
M3 = "claude-opus-4-7 · 6.8s · ↑118k ↓2.4k"

ANTHROPIC = ("anthropic", "prov-1")
OPENAI = ("openai", "prov-2")
LIST_A = [("claude-opus-4-7", 1_000_000, True), ("claude-opus-4-9", 0, True), ("claude-sonnet-4-6", 1_000_000, True)]
LIST_O = [("gpt-5.4-mini", 400_000, True), ("gpt-5.5", 1_000_000, True)]
GROUPS = [(ANTHROPIC[0], ANTHROPIC[1], LIST_A), (OPENAI[0], OPENAI[1], LIST_O)]


def turn1(w):
    return user(w, Q1, T1) + reply(w, A1, M1)


# ---------------------------------------------------------------- states

Q4, T4 = "run the leader tests and fix what fails", "14:04"


def l1(w, h):
    foot = footer(w, "openai/gpt-5.4-mini", "medium", pct=4)
    return screen(w, h, foot, turn1(w))


def l2(w, h):
    foot = footer(w, "openai/gpt-5.4-mini", "medium", pct=6)
    pane = turn1(w) + user(w, Q4, T4) + [
        thought(w, "Thought for 1.2s"),
        step(w, "Ran", "go vet ./internal/leader"),
        step(w, "Running", "go test ./internal/leader -count=1", "⠋ 4s", "warning", "warning"),
    ]
    return screen(w, h, foot, pane,
                  queue_rows=[queue(w, "steer", "assistant", "use the fake clock from internal/testsupport"),
                              queue_hint(w)],
                  status_rows=[working(w, 4)])


def forms(w, h):
    rows = [
        footer(w, "openai/gpt-5.4-mini", "medium", pct=4),
        footer(w, "anthropic/claude-opus-4-9", "medium", pct=None),
        footer(w, "anthropic/claude-sonnet-4-6", "high", pct=93),
        footer(w, "", pct=0),
        footer(w, "", no_model=True),
        footer(w, "anthropic/claude-sonnet-4-6", "high", pct=93, cwd_path="~/src/github.com/kevinle128/askcore"),
        footer(w, "alibaba-token-plan/deepseek-v4.1-flash", "", pct=18,
               cwd_path="~/src/github.com/kevinle128/askcore"),
    ]
    if w == 80:
        # The longest examples must fit whole with the short directory (D13, D14).
        for k in (5, 6):
            row = plain(rows[k])
            assert "⌂ ~/…/askcore" in row and "…" not in row.replace("~/…/", ""), row
    assert len(rows) == h
    return [("footer", r) for r in rows]


def s1(w, h):
    foot = footer(w, "openai/gpt-5.4-mini", "medium", pct=4)
    return screen(w, h, foot, turn1(w),
                  slot=selector(w, h, GROUPS, "anthropic/claude-opus-4-9", "openai/gpt-5.4-mini",
                                default="openai/gpt-5.4-mini"))


def s2(w, h):
    foot = footer(w, "anthropic/claude-opus-4-9", "medium", pct=None)
    return screen(w, h, foot, turn1(w),
                  status_rows=[status(w, "✓", "success", "Default model: anthropic/claude-opus-4-9")])


def s4(w, h):
    foot = footer(w, "anthropic/claude-opus-4-7", "medium", pct=12)
    groups = [(ANTHROPIC[0], ANTHROPIC[1], LIST_A), (OPENAI[0], OPENAI[1], LIST_O)]
    pane = user(w, Q3, T3) + reply(w, A3, M3)
    return screen(w, h, foot, pane,
                  slot=selector(w, h, groups, "anthropic/claude-opus-4-7", "anthropic/claude-opus-4-7",
                                not_in_account=("anthropic/claude-opus-4-7",)))


def s5(w, h):
    foot = footer(w, "openai/gpt-5.5", "medium", pct=0)
    note = "Default model openai/gpt-5.4-mini is not usable (not in account list)."
    return screen(w, h, foot, fallback_notice(w, note))


S80, S40, S120 = (80, 24), (40, 14), (120, 40)
STATES = [
    ("L1", "Idle: the status data is in the footer row",
     "The decided layout. A slim header row holds only the logo, with a rule under it (D16). "
     "There is no model line, so the block pane starts with the first prompt band on row 3. "
     "Under the prompt pane and its rules, the footer row holds the directory, the model "
     "with its thinking level and the context use. It shows no logo, no link state and no session cost. The shortcut bar stays the last row.",
     l1, [S80, S120, S40]),
    ("L2", "Streaming: queue rows and the live rail above the prompt, the footer below it",
     "A tool runs and a steer waits. The queue rows and the status row stay directly above the prompt "
     "rules, as today. The footer row and the bar do not move while the pane streams.",
     l2, [S80, S40]),
    ("F", "Footer row forms",
     "One footer row for each case, top to bottom: a session model; unknown context window; context at 90% "
     "or more (error role); state not known yet (meter only); no model; a long directory with the longest "
     "Anthropic model; a long directory with the longest model id. The footer row has no logo and no link "
     "state in any state (D14). These rows are not a screen: each row is the footer row of its own state.",
     forms, [(80, 7), (40, 7)]),
    ("S1", "/model: a row with an unknown context window",
     "The account list has anthropic/claude-opus-4-9, which matches a family rule but has no metadata. "
     "Its window column shows ? in place of a blank. The user opened /model on gpt-5.4-mini and pressed "
     "up twice; the cursor skips the provider header. There is no cost column.",
     s1, [S80, S40]),
    ("S2", "Footer: the current model has an unknown context window",
     "The user chose claude-opus-4-9 in S1. The block pane gets no change line: only the footer row shows the "
     "new model, and the status row confirms the saved default. The footer row shows ? in place "
     "of the bar and the percent.",
     s2, [S80, S40]),
    ("S4", "/model: the current model is not in the account list",
     "A refresh of the anthropic account list no longer has claude-opus-4-7, the model of this session. "
     "The leader still sends its row, so the footer row keeps the bar and the thinking level. The row keeps "
     "the current tag and adds a muted note after the id. Under 60 columns the row has no room for the note, "
     "so the tag unlisted replaces current.",
     s4, [S80, S40]),
    ("S5", "Session start: the saved default is not in the account list",
     "The saved default openai/gpt-5.4-mini is in the catalog of Ask, but the signed-in account list does "
     "not have it. The session starts on openai/gpt-5.5, which the footer row names. The fallback note is its own "
     "notice block at the top of the pane, with no model line above it.",
     s5, [S80, S40]),
]


def regions(rows):
    """Collapse the region of each row into 'rows 1-19 block pane; row 20 rule; ...'."""
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
            print(f"Regions: {regions(rows)}.\n")
            print(f"{w}×{h}:\n")
            print("```text")
            for _, r in rows:
                print(plain(r))
            print("```\n")


if __name__ == "__main__":
    main()
