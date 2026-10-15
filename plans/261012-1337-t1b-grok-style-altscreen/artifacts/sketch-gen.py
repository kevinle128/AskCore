#!/usr/bin/env python3
"""Generate exact-width sketches for the Grok-style alternate-screen TUI design.

Each sketch line is a list of segments (text, roles). design.md uses the plain text;
preview.html renders the roles as colors. Both come from this file, so they cannot disagree.
Helpers are copied from the T1b tool output generator (plans/261012-0226-t1b-tool-output).
The whole interactive TUI is on the alternate screen: there is no scrollback and no live-region line.

Usage: python3 sketch-gen.py > sketches.md
"""
import unicodedata

# A screen that is not on the alternate screen (the main screen after exit) starts with this mark.
MAIN = "-- main screen --"


def cw(s):
    n = 0
    for c in s:
        if unicodedata.combining(c):
            continue
        n += 2 if unicodedata.east_asian_width(c) in "WF" else 1
    return n


def wrap(text, w, first="", rest=""):
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


def trunc(s, n):
    if cw(s) <= n:
        return s
    out = ""
    for c in s:
        if cw(out + c) > n - 1:
            break
        out += c
    return out + "…"


def cut_cols(line, n):
    out, cur = [], ""
    for c in line:
        if cw(cur + c) > n:
            out.append(cur)
            cur = ""
        cur += c
    return out + [cur]


def _flat(items):
    out = []
    for it in items:
        out.extend(it) if isinstance(it, list) else out.append(it)
    return out


def L(w, *segs, right=None, fill=""):
    left, rgt = _flat(segs), _flat(right or [])
    used = sum(cw(t) for t, _ in left) + sum(cw(t) for t, _ in rgt)
    if used > w:
        raise SystemExit(f"row too wide ({used}>{w}): {''.join(t for t, _ in left + rgt)!r}")
    return left + [(" " * (w - used), fill)] + rgt


def plain(row):
    return "".join(t for t, _ in row)


def on(row, bg):
    """Put a background role on every segment of a row (selection)."""
    return [(t, (r + " " + bg).strip()) for t, r in row]


# ---------------------------------------------------------------- content

MODEL = "claude-sonnet-5-5"
TAB = 4

SEQ = [str(i) for i in range(1, 41)]
PARSER = """package parser

import "strings"

// Count returns the number of fields.
func Count(s string) int {
	fields := strings.Split(s, ",")
	n := 0
	for _, f := range fields {
		n++
	}
	return n
}""".expandtabs(TAB).split("\n")
DIFF = [(" ", "9", "	for _, f := range fields {"), ("-", "10", "		n++"), ("+", "10", "		if f != \"\" {"),
        ("+", "11", "			n++"), ("+", "12", "		}"), (" ", "13", "	}")]
TEST_OUT = """--- FAIL: TestCount (0.00s)
    parser_test.go:12: want 2, got 3
=== RUN   TestCountEmpty
--- PASS: TestCountEmpty (0.00s)
=== RUN   TestCountOne
--- PASS: TestCountOne (0.00s)
FAIL
FAIL	example.com/p/parser	0.012s
FAIL""".expandtabs(TAB).split("\n")
THINK = ["The user wants the command first, then a read of parser.go.",
         "The bug is probably in the count of empty fields.",
         "Split on \",\" gives an empty string for \"a,,b\".",
         "So the loop must skip f == \"\"."]
Q1 = "run seq 1 40, then read parser.go and fix the count of empty fields"
LONGQ = ("the parser counts empty fields. Read parser.go and parser_test.go, find why Count(\"a,,b\") "
         "returns 3, fix it so empty fields are skipped, keep the public API the same, run the tests, "
         "and then tell me which other callers of Count in the repo could change behavior after the fix "
         "and whether we need a changelog entry")
REPLY = ["Count now skips empty fields. The hook no-rm blocked the removal of the build",
         "directory. TestCount still fails because it expects the old value; shall I fix it?"]


# ---------------------------------------------------------------- frame parts

def header(w, pct=12, cost="$0.01"):
    if w < 60:
        return L(w, ("◢◤ ", "logo"), ("ask", "logo"), (" ● ", "success"), ("7c1e", "muted"),
                 right=[("sonnet-5-5", "assistant bold"), (f" {pct}% ", "muted")])
    return L(w, ("◢◤ ", "logo"), ("ask", "logo"), ("  ● ", "success"), ("leader 7c1e", "muted"),
             ("  ⌂ ~/src/p", "muted"),
             right=[(MODEL, "assistant bold"), ("  ", ""), ("▰" + "▱" * 7, "accent"), (f" {pct}%", "muted"),
                    (f"  {cost} ", "muted")])


def rule(w):
    return L(w, ("─" * w, "border"))


def bar(w, focus, mode=None):
    """Shortcut bar, the last row. Its text follows the focus."""
    if focus == "prompt":
        t = "Tab blocks · ctrl+t thinking · ctrl+l model · / commands" if w >= 60 else "Tab blocks · / commands"
    elif focus == "list":
        if w >= 60:
            t = "j/k select · e fold" + (f": {mode}" if mode else "") + " · E all · Enter open · Tab prompt"
        else:
            t = "j/k · e fold · Enter open"
    else:
        t = "↑↓ scroll · PgUp PgDn page · Home End · Esc back" if w >= 60 else "↑↓ PgUp PgDn · Esc back"
    return L(w, (" " + t, "dim"))


def prompt_rows(w, focus, draft=None, rows_max=1):
    """Prompt pane: `❯ ` and the draft; one row up to half the screen."""
    if draft is None:
        ph = "Ask anything  ·  / commands  ·  ctrl+l model" if w >= 60 else "Ask anything"
        segs = [(" ❯ ", "accent bold")]
        segs += [("▌", "cursor"), (ph, "dim italic")] if focus == "prompt" else [(ph, "dim italic")]
        return [L(w, *segs)]
    lines = []
    for k, para in enumerate(draft):
        lines += wrap(para, w - 4) if para else [""]
    out = []
    for k, ln in enumerate(lines):
        segs = [(" ❯ " if k == 0 else "   ", "accent bold"), (ln, "")]
        if k == len(lines) - 1 and focus == "prompt":
            segs.append(("▌", "cursor"))
        out.append(L(w, *segs))
    return out


def screen(w, h, blocks, focus="prompt", top=None, draft=None, status=None, mode=None, marker=False):
    """Whole alternate screen: header, rule, block pane, [status], rule, prompt pane, rule, bar."""
    prompt = prompt_rows(w, focus, draft)
    prompt = prompt[: max(1, h // 2)]
    st = [status] if status else []
    pane_h = h - 5 - len(prompt) - len(st)
    if top is None:  # follow the end
        top = max(0, len(blocks) - pane_h)
    win = blocks[top:top + pane_h]
    win += [L(w)] * (pane_h - len(win))
    if marker:  # follow is off: a clickable marker on the last pane row
        below = max(0, len(blocks) - (top + pane_h))  # pane rows below the window
        if marker == "new":
            txt = f"▼ {below} · End" if w < 30 else f"▼ {below} new lines · End"
        else:
            txt = "▼ End"
        pad = (w - cw(txt)) // 2  # centered on the last pane row, bold, no background
        win[-1] = L(w, (" " * pad, ""), (txt, "accent bold" if marker == "new" else "tool bold"))
    return [header(w), rule(w)] + win + st + [rule(w)] + prompt + [rule(w), bar(w, focus, mode)]


# ---------------------------------------------------------------- blocks

def user(w, q, time="15:20", fold=True):
    rows = wrap(q, w - 12)
    shown = rows[:3] if fold and len(rows) > 3 else rows
    out = [L(w, ("  ", "panel"), ("❯ ", "accent bold panel"), (shown[0], "user panel"),
             right=[(time + "  ", "dim panel")], fill="panel")]
    out += [L(w, ("    ", "panel"), (r, "user panel"), fill="panel") for r in shown[1:]]
    if len(shown) < len(rows):
        out.append(L(w, ("    " + trunc(f"… +{len(rows) - 3} lines · e to expand", w - 4), "muted panel"),
                     fill="panel"))
    return out + [L(w)]


def step(w, glyph, glyph_role, verb, desc, right=None, extra=None, sel=False, glyph_w=1):
    """`  ◆ <Verb> <description>  [extra]` with an optional right column. The description is cut first."""
    left = [("  ", ""), (glyph + " ", glyph_role), (verb, "bold")]
    rgt = list(right or []) + [(" ", "")]
    extra = extra or []
    room = w - sum(cw(t) for t, _ in left) - sum(cw(t) for t, _ in rgt) - sum(cw(t) for t, _ in extra) - 2
    if desc and room >= 2:
        left.append((" " + trunc(desc, room), ""))
    row = L(w, *left, extra, right=rgt)
    return on(row, "selected") if sel else row


def thought(w, secs="0.5s", sel=False):
    return step(w, "◆", "thinking", "Thought", f"for {secs}", sel=sel)


def thinking_live(w, secs):
    rows = [step(w, "◆", "thinking", "Thinking…", secs, right=[("⠋", "accent")])]
    rows += [L(w, ("  ┃ ", "thinking"), ("…", "thinking"))]
    rows += [L(w, ("  ┃ ", "thinking"), (trunc(t, w - 4), "thinking")) for t in THINK[-2:]]
    return rows


def ran(w, cmd, ok=True, sel=False, running=False, secs="4s"):
    if running:
        return step(w, "◆", "warning", "Running", cmd, right=[(f"⠋ {secs}", "warning")], sel=sel)
    if ok:
        return step(w, "◆", "tool", "Ran", cmd, sel=sel)
    return step(w, "◆", "error", "Ran", cmd, right=[("✗ exit 1", "error")], sel=sel)


def read(w, what="parser.go", sel=False):
    return step(w, "◆", "tool", "Read", what, sel=sel)


def edited(w, path="parser.go", add=3, rem=1, sel=False):
    stat = [("  +" + str(add), "success"), (" -" + str(rem), "error")]
    return step(w, "◆", "tool", "Edited", path, extra=stat, sel=sel)


def hook(w, event, scope, name, target, ok, secs, desc, sel=False):
    res = [((f"✓ allowed {secs}" if ok else f"✗ blocked {secs}"), "success" if ok else "error")]
    head = step(w, "⚓", "hook", f"{event} hook", f"({scope}/{name}) · {target}", right=res, sel=sel)
    return [head, L(w, ("    " + trunc(desc, w - 4), "muted"))]


def body(w, line, role=""):
    return L(w, ("    ", ""), (trunc(line, w - 4), role))


def full(w, line, role=""):
    return [L(w, ("    ", ""), (p, role)) for p in cut_cols(line, w - 4)]


def hint(w, n):
    text = f"⋮ {n} more lines · e to expand · Enter full screen" if w >= 60 else f"⋮ {n} more lines · e"
    return L(w, ("    " + trunc(text, w - 4), "muted"))


def grok_trunc(w, lines):
    """Truncated mode: first 3 lines, a hint, last 3 lines; 6 lines or fewer show all and no hint."""
    if len(lines) <= 6:
        return [body(w, l) for l in lines]
    return [body(w, l) for l in lines[:3]] + [hint(w, len(lines) - 6)] + [body(w, l) for l in lines[-3:]]


def diff_rows(w):
    out = []
    for sign, num, text in DIFF:
        role = {"+": "success", "-": "error", " ": "muted"}[sign]
        out.append(L(w, ("    ", ""), (f"{num:>4} ", "dim"), (sign + " ", role),
                     (trunc(text.expandtabs(TAB), w - 11), "" if sign == " " else role)))
    return out


def reply(w):
    rows = []
    for t in REPLY:
        rows += [L(w, ("  " + r, "")) for r in wrap(t, w - 2)]
    return rows + [L(w, ("  " + trunc(MODEL + " · 6.2s · ↑8.1k ↓320 · $0.006", w - 2), "dim")), L(w)]


HOOK_OK = ("PreToolUse", "project", "lint-guard", "edit parser.go", True, "0.1s",
           "Runs gofmt on the file before the agent edits it")
HOOK_NO = ("PreToolUse", "global", "no-rm", "bash rm -rf build", False, "0.0s",
           "Blocks recursive removal (rm -rf) inside the repo")


FOLDABLE = {1, 2, 3, 4, 5, 6, 7}  # thinking, tools, hooks of the session below


def indicate(row, glyph):
    """Replace the step glyph (`◆` or `⚓`) of a first row; `⚓` is 2 columns, so one space keeps the width."""
    out, done = [], False
    for t, r in row:
        if not done and t in ("◆ ", "⚓ "):
            t = glyph + " " + (" " if t == "⚓ " else "")
            r = "accent bold"
            done = True
        out.append((t, r))
    return out


def rail(row):
    """Draw `┃` in the gutter (column 1). The column is a space in every row, so nothing moves."""
    t, r = row[0]
    assert t[:1] == " ", row
    return [("┃", "accent")] + ([(t[1:], r)] if t[1:] else []) + row[1:]


def s_reply_selected(w, h):
    return screen(w, h, session(w, sel=8), focus="list")


def session(w, sel=None, fold=None, hover=None):
    """The compact session of the mockup. sel: selected block; hover: block under the pointer; fold: {index: rows}.
    A selected or hovered foldable block shows `›` when Collapsed and `⌄` when Expanded (open question 13)."""
    fold = fold or {}
    parts = [
        user(w, Q1),
        [thought(w)],
        [ran(w, "seq 1 40")],
        [read(w)],
        hook(w, *HOOK_OK),
        [edited(w)],
        hook(w, *HOOK_NO),
        [ran(w, "go test ./...", ok=False)],
        [L(w)] + reply(w),
    ]
    rows = []
    for i, p in enumerate(parts):
        p = list(p)
        if i in (sel, hover) and i in FOLDABLE:
            if i not in fold:
                p[0] = indicate(p[0], "›")
            elif i in EXPANDED:
                p[0] = indicate(p[0], "⌄")
        if i == sel and i in FOLDABLE and i not in fold:
            # Collapsed step: shade its rows (the header row; a hook also its description row).
            p = [on(r, "selected") for r in p]
            rows += p
        elif i == sel:
            # Any other selected block: no background; a `┃` rail in the gutter on every row of the block.
            body = p + fold.get(i, [])
            lo = 1 if i == 8 else 0                   # the reply part starts with a blank separator row
            hi = len(body) - 1 if i in (0, 8) else len(body)  # user and reply end with a blank row
            rows += [rail(r) if lo <= k < hi else r for k, r in enumerate(body)]
        else:
            rows += p + fold.get(i, [])
    return rows


# ---------------------------------------------------------------- states

def s_idle(w, h):
    return screen(w, h, session(w))


def s_list(w, h):
    return screen(w, h, session(w, sel=7), focus="list", mode="collapsed")


def s_truncated(w, h):
    tail = grok_trunc(w, TEST_OUT)
    return screen(w, h, session(w, sel=7, fold={7: tail}), focus="list", mode="truncated")


EXPANDED = set()


def s_expanded(w, h):
    EXPANDED.add(5)
    rows = session(w, sel=5, fold={5: diff_rows(w)})
    EXPANDED.discard(5)
    return screen(w, h, rows, focus="list", mode="expanded", top=0)


def s_streaming(w, h):
    rows = user(w, "run all tests and tell me what fails", "15:31")
    rows += thinking_live(w, "2s") + [ran(w, "go test ./... -count=1", running=True)]
    st = L(w, ("┊ ", "dim"), ("◌ ", "accent"), ("Working…", "accent bold shimmer"),
           (" 6s · esc to interrupt" if w >= 60 else " 6s · esc", "muted"))
    return screen(w, h, rows, status=st, top=0)


def s_long_prompt(w, h):
    rows = user(w, LONGQ, "15:44") + [thought(w, "1.2s"), read(w, "2 files")]
    return screen(w, h, rows, top=0)


def s_indicator(w, h):
    rows = session(w, sel=7, hover=4)
    return screen(w, h, rows, focus="list", mode="collapsed")


def s_hook_expanded(w, h):
    det = hook_detail(w, blocked=True)
    rows = session(w, sel=6, fold={6: det})
    top = next(i for i, r in enumerate(rows) if "no-rm" in plain(r)) - 1
    return screen(w, h, rows, focus="list", mode="expanded", top=top)


def hook_detail(w, blocked):
    f = [("Event", "PreToolUse"),
         ("Matcher", "tool bash" if blocked else "tool edit, write · file *.go"),
         ("Does", HOOK_NO[6] if blocked else HOOK_OK[6]),
         ("Source", "~/.ask/hooks.json (global)" if blocked else ".ask/hooks.json (project)"),
         ("Command", "ask-hook-no-rm · timeout 5s" if blocked else "./scripts/lint-guard.sh · timeout 5s"),
         ("Result", "blocked · exit 2 · 0.0s" if blocked else "allowed · exit 0 · 0.1s")]
    if blocked:
        f.append(("Reason", "recursive removal is not allowed in this repo"))
    rows = []
    for k, v in f:
        rows.append(L(w, ("    ", ""), (f"{k:<9}", "muted"), (trunc(v, w - 13), "error" if k == "Reason" else "")))
    rows.append(L(w, ("    ", ""), ("── output ──", "dim")))
    rows.append(body(w, 'no-rm: blocked "rm -rf build"' if blocked else "lint-guard: parser.go is formatted"))
    return rows


def viewer(w, h, title, right, content, top=0):
    """Full-screen viewer of one block: back row, rule, content, rule, key row."""
    pane = h - 4
    win = content[top:top + pane]
    win += [L(w)] * (pane - len(win))
    pos = f"{top + 1}–{min(top + pane, len(content))} of {len(content)} "
    back = [(" ← back", "accent bold"), ("  ", "")]
    rgt = list(right) + [(" ", "")]
    room = w - cw(" ← back  ") - sum(cw(t) for t, _ in rgt) - 1
    head = L(w, *back, (trunc(title, room), "bold"), right=rgt)
    keys = bar(w, "viewer")
    keys = L(w, (plain(keys).rstrip(), "dim"), right=[(pos, "dim")]) if w >= 60 else keys
    return [head, rule(w)] + win + [rule(w), keys]


def s_viewer_tool(w, h):
    content = [body(w, "$ seq 1 40", "dim"), L(w)] + [r for l in SEQ for r in full(w, l)]
    return viewer(w, h, "Ran seq 1 40", [("0.0s", "dim")], content)


def s_viewer_fail(w, h):
    content = [body(w, "$ go test ./...", "dim"), L(w)]
    content += [r for l in ["=== RUN   TestCount"] + TEST_OUT for r in full(w, l)]
    return viewer(w, h, "Ran go test ./...", [("✗ exit 1", "error")], content)


def s_viewer_hook(w, h):
    content = hook_detail(w, blocked=True)
    return viewer(w, h, "PreToolUse hook (global/no-rm) · bash rm -rf build", [("✗ blocked 0.0s", "error")], content)


def s_scrolled(w, h):
    rows = session(w) + user(w, "fix the test as well", "15:24") + [read(w, "parser_test.go"),
                                                                       ran(w, "go test ./...", running=True)]
    return screen(w, h, rows, focus="list", top=0, marker="new")


def s_scrolled_quiet(w, h):
    return screen(w, h, session(w), focus="list", top=0, marker="bottom")


def s_selection(w, h):
    rows = session(w)
    # Drag over the second reply row; the copied range uses the selected background.
    i = next(k for k, r in enumerate(rows) if plain(r).startswith("  directory."))
    text = plain(rows[i])
    a, b = 2, 2 + cw("directory. TestCount still fails")
    rows[i] = [(text[:a], ""), (text[a:b], "selected"), (text[b:], "")]
    st = L(w, ("┊ ", "dim"), ("✓ ", "success"), ("Copied", "dim"))
    return screen(w, h, rows, status=st)


def s_prompt_max(w, h):
    draft = ["Plan for the parser change:", "",
             "1. Skip empty fields in Count.", "2. Keep the public API.",
             "3. Update TestCount to expect 2.", "4. Add TestCountTrailingComma for \"a,b,\".",
             "5. Run go test ./... and go vet ./...", "6. List the callers of Count.",
             "7. Say if the changelog needs an entry.", "8. Do not touch the lexer.",
             "9. Keep the diff small.", "10. Report the test output."]
    return screen(w, h, session(w), draft=draft)


def s_exit(w, h):
    rows = [L(w, ("~/src/p $ ask", ""))]
    rows += [L(w, ("ask: session 3f2a9c1e", ""))]
    rows += [L(w, ("~/src/p $ ", ""), ("▌", "cursor"))]
    return [MAIN] + [L(w)] * 0 + rows + [L(w)] * (h - len(rows))


STATES = [
    ("A1", "Compact session, prompt focus",
     "The whole TUI is on the alternate screen. Every step is one row: thinking `◆ Thought`, tools `◆ <Verb> "
     "<what>`, hooks `⚓` with a description row. The reply is expanded. Without a model description the tool "
     "row shows the command or the path.", s_idle, [(80, 24), (40, 14)]),
    ("A2", "Block list focus, a block selected",
     "Tab moves the focus to the block list. The selected block has the `selected` background on its first row. "
     "The bar shows the list keys and the fold mode of the selected block.", s_list, [(80, 24)]),
    ("A2b", "Expand indicator on select and hover",
     "A foldable block at its minimum fold shows `›` instead of `◆` (or `⚓`) when it is selected (here the "
     "failed test run) or under the mouse pointer (here the lint-guard hook). `→`/`l` expands it to Expanded, "
     "`←`/`h` collapses it. The reply and the user prompt keep their rows: they are not foldable.",
     s_indicator, [(80, 24), (40, 14)]),
    ("A2c", "Assistant reply selected",
     "A block that is not a Collapsed step gets no background when selected: a `┃` rail in the `accent` role "
     "fills the gutter column on every row of the block, wrapped rows and the usage row included.",
     s_reply_selected, [(80, 24), (40, 14)]),
    ("A3", "Selected tool, Truncated (e once)",
     "Truncated (Grok form): the first 3 output lines, a muted `⋮ N more lines` hint, the last 3 lines. "
     "No command row; it shows only in Expanded and in the viewer.",
     s_truncated, [(80, 24), (40, 14)]),
    ("A4", "Edit, Expanded (e twice, or → once)",
     "The Expanded mode shows all output; for an edit, the unified diff of T1b. A selected Expanded block "
     "shows `⌄` (open question 13).", s_expanded, [(80, 24)]),
    ("A5", "A turn streams",
     "Thinking that streams is Truncated (title, `…`, the last 2 lines on a `┃` rail); it folds to Collapsed "
     "when it ends. A running tool has `◆` in the warning role and a spinner. The status row sits above the "
     "prompt pane.", s_streaming, [(80, 24), (40, 14)]),
    ("A6", "Long user prompt folds above 3 rows",
     "A prompt longer than 3 rows shows 3 rows and a muted count on the band. `e` on it expands it.",
     s_long_prompt, [(80, 24), (40, 14)]),
    ("A7", "Hook block, Expanded",
     "The blocked hook shows its detail fields and then its output.", s_hook_expanded, [(80, 24)]),
    ("V1", "Viewer: a tool block (Enter or click on the row)",
     "One block on the full screen, all output, wrapped. Esc or a click on `← back` returns to the same "
     "list position.", s_viewer_tool, [(80, 24), (40, 14)]),
    ("V2", "Viewer: a failed tool", "The result stays right in the title row.", s_viewer_fail, [(80, 24)]),
    ("V3", "Viewer: a hook", "Event, matcher, what the hook does, source and scope, command and timeout, "
     "result, reason, then the output.", s_viewer_hook, [(80, 24), (40, 14)]),
    ("S1", "Scrolled up while new output comes",
     "The wheel or j/k moved the window up, so follow is off. New output does not move it; the last pane row "
     "shows `▼ <N> new lines · End` (accent, bold, centered; N = pane rows below the window). A click on it or End jumps to the bottom and turns follow on.",
     s_scrolled, [(80, 24), (40, 14)]),
    ("S2", "Scrolled up, no new output",
     "Follow is off and nothing new came: the marker is `▼ End` in the tool role, bold, centered. Same click and keys.",
     s_scrolled_quiet, [(80, 24)]),
    ("M1", "Mouse drag selection, copied on release",
     "The dragged range has the `selected` background. On release the text goes to the clipboard through "
     "the /copy path, and the status row shows the result.", s_selection, [(80, 24)]),
    ("P1", "Prompt pane at its maximum height",
     "The prompt pane grows with the draft up to half the screen (12 rows at 24); the editor scrolls inside it. "
     "The block pane gets the rest.", s_prompt_max, [(80, 24)]),
    ("X1", "After exit, on the main screen",
     "The alternate screen closes; the terminal history is as before `ask` started, plus one line. "
     "Until H8 ships --resume, the line has only the session id.", s_exit, [(80, 24)]),
]


def block(rows):
    parts, cur = [], []
    for r in rows:
        if r == MAIN:
            parts.append("`-- main screen, after the alternate screen closed --`")
            continue
        cur.append(plain(r))
    parts.append("```text\n" + "\n".join(cur) + "\n```")
    return "\n\n".join(parts)


def main():
    out = []
    for sid, title, caption, fn, sizes in STATES:
        out.append(f"### {sid}. {title}\n")
        out.append(caption + "\n")
        for (w, h) in sizes:
            rows = fn(w, h)
            real = [r for r in rows if r != MAIN]
            if len(real) != h:
                raise SystemExit(f"{sid} {w}x{h}: {len(real)} rows")
            for r in real:
                if cw(plain(r)) != w:
                    raise SystemExit(f"{sid} {w}: width {cw(plain(r))}: {plain(r)!r}")
            out.append(f"{w}×{h}:\n")
            out.append(block(rows) + "\n")
    print("\n".join(out))


if __name__ == "__main__":
    main()
