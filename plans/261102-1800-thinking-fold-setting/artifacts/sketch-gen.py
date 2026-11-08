#!/usr/bin/env python3
"""Generate exact-width sketches for the thinking and reply look TUI design (style option A).

Screen model: the alternate screen as the current goldens draw it (header row, rule, block pane,
status row, rule, prompt row, rule, footer row, shortcut bar).

Each screen row is (region, segments). A segment is (text, roles). design.md uses the plain
text; preview.html renders the roles as colors and the region names beside the terminal.

Usage: python3 sketch-gen.py > sketches.md
"""
import re
import unicodedata


def cw(s):
    return sum(0 if unicodedata.combining(c) else (2 if unicodedata.east_asian_width(c) in "WF" else 1) for c in s)


def plain(row):
    return "".join(t for t, _ in row)


def fit(segs, w, fill=""):
    used = sum(cw(t) for t, _ in segs)
    if used > w:
        raise SystemExit(f"row too wide ({used}>{w}): {plain(segs)!r}")
    return segs + [(" " * (w - used), fill)]


def L(w, *segs, right=None):
    right = list(right or [])
    used = sum(cw(t) for t, _ in segs) + sum(cw(t) for t, _ in right)
    if used > w:
        raise SystemExit(f"row too wide ({used}>{w}): {plain(list(segs) + right)!r}")
    return list(segs) + [(" " * (w - used), "")] + right


def wrap_segs(segs, first, rest):
    """Wrap a segment list by words; the first row has `first` columns, the others `rest`."""
    rows, cur, used, limit = [], [], 0, first
    for text, role in segs:
        for word in re.split(r"( )", text):
            if word == "" or (word == " " and used == 0):
                continue
            if word != " " and used + cw(word) > limit:
                while cur and cur[-1][0] == " ":
                    cur.pop()
                rows.append(cur)
                cur, used, limit = [], 0, rest
            cur.append((word, role))
            used += cw(word)
    rows.append(cur)
    return rows


# ---------------------------------------------------------------- style A

HEAD = {1: "h1 bold", 2: "h2 bold", 3: "h3 bold"}


def inline(text):
    """`code` is blue bold with no backticks; **bold** is bold; the rest is fg."""
    out = []
    for part in re.split(r"(`[^`]+`|\*\*[^*]+\*\*)", text):
        if not part:
            continue
        if part.startswith("`"):
            out.append((part[1:-1], "h2 bold"))
        elif part.startswith("**"):
            out.append((part[2:-2], "fg bold"))
        else:
            out.append((part, ""))
    return out


def md_rows(blocks, inner, first_room):
    """Markdown blocks -> rows of segments, each at most inner columns (first row: first_room)."""
    rows = []
    for kind, val in blocks:
        room = first_room if not rows else inner
        if kind == "p":
            rows += wrap_segs(inline(val), room, inner)
        elif kind == "blank":
            rows.append([])
        elif kind in ("h1", "h2", "h3"):
            # One blank row under each heading (user decision of 2026-11-03).
            rows += [[(val, HEAD[int(kind[1])])], []]
        elif kind == "ol":
            for i, item in enumerate(val, 1):
                rows += [[(f"{i}. ", "muted")] + r if j == 0 else [("   ", "")] + r
                         for j, r in enumerate(wrap_segs(inline(item), inner - 3, inner - 3))]
        elif kind == "ul":
            for item in val:
                rows += [[("• ", "muted")] + r if j == 0 else [("  ", "")] + r
                         for j, r in enumerate(wrap_segs(inline(item), inner - 2, inner - 2))]
        elif kind == "table":
            widths = [max(cw(r[i]) for r in val) for i in range(len(val[0]))]

            def line(l, m, r):
                return [(l + m.join("─" * (x + 2) for x in widths) + r, "dim")]

            def cells(r, role):
                segs = [("│", "dim")]
                for i, c in enumerate(r):
                    segs += [(" " + c + " " * (widths[i] - cw(c)) + " ", role), ("│", "dim")]
                return segs
            rows += [line("┌", "┬", "┐"), cells(val[0], "fg bold"), line("├", "┼", "┤")]
            rows += [cells(r, "") for r in val[1:]] + [line("└", "┴", "┘")]
        elif kind == "code":
            for c in val:
                rows.append([("  " + c, "code code-panel"), (" " * (inner - cw(c) - 2), "code-panel")])
    return rows


# ---------------------------------------------------------------- example turn

PROMPT = "run the tests and fix what fails"
THOUGHT = [
    "The test fails on an empty field.",
    "parser.go splits on commas and counts every part.",
    "An empty part still adds one to the count.",
    'So the loop must skip f == "".',
    "Then TestCount gets 2, not 3.",
    "I will edit Count and run the tests again.",
]
REPLY = [("p", "Count now skips empty fields, and `go test ./...` passes.")]
META = "claude-sonnet-5-5 · 9.4s · ↑8.1k ↓410 · $0.007"
PROMPT2 = "now add a test for a trailing comma"
THOUGHT2 = ["A trailing comma gives one empty part at the end.", 'The new test wants 2 for "a,b,".']
REPLY2 = [("p", "Added `TestCountTrailingComma`; it passes.")]
META2 = "claude-sonnet-5-5 · 5.1s · ↑9.0k ↓220 · $0.004"

MD_PROMPT = "explain tool_coordinator.go"
MD_THOUGHT = ["The user wants a summary of tool_coordinator.go.",
              "I will group it: the types, the batch loop, and the safety parts."]
MD_REPLY = [
    ("p", "`tool_coordinator.go` runs **one batch of tool calls**: it prepares them, runs them in parallel "
          "with a limit, and commits them in order."),
    ("blank", ""), ("h2", "Types"),
    ("table", [("Type", "Job"), ("toolSlot", "State of one call"), ("toolDone", "A worker is done"),
               ("updater", "Progress that never blocks")]),
    ("blank", ""), ("h2", "The batch loop"),
    ("ol", ["Find the last assistant entry in the log.",
            "`MaxParallelTools` (default 10) limits the workers.",
            "Commit in order: `progress`, `afterTool`, `emitToolEnd`."]),
    ("blank", ""), ("h3", "Safety"),
    ("ul", ["`toolGuarded` turns a panic into an error.", "`cloneToolResult` deep-copies before emit."]),
    ("blank", ""),
    ("code", ["body, e = p.tool.Execute(bodyCtx, tc, args)"]),
]
MD_META = "claude-sonnet-5-5 · 14s · ↑10.2k ↓1.8k · $0.012"


# ---------------------------------------------------------------- screen parts

def header(w):
    return ("header", L(w, (" ◢◤ ask", "logo")))


def rule(w):
    return ("rule", L(w, ("─" * w, "border")))


def prompt_row(w):
    hint = "Ask anything  ·  / commands  ·  ctrl+l model"
    if cw(hint) + 3 > w:
        hint = hint[:w - 4] + "…"
    return ("prompt pane", L(w, (" ❯ ", "accent"), (hint, "muted")))


def footer(w):
    if w >= 60:
        return ("footer", L(w, (" ⌂ ~/src/p", "muted"),
                            right=[("anthropic/claude-sonnet-5-5", "assistant"), ("  ", ""),
                                   ("▰▱▱▱▱▱▱▱", "success"), (" 12%", "muted")]))
    return ("footer", L(w, (" ⌂ ~/src/p ", "muted"), ("anthropic/claude-sonnet-5-5 …", "assistant")))


def bar(w, focus="prompt"):
    if focus == "list":
        text = " j/k select · e fold · Enter open · Tab prompt"
    else:
        text = " Tab blocks · ctrl+t thinking · ctrl+l model · / commands"
        if cw(text) > w:
            text = " Tab blocks · / commands"
    return ("bar", L(w, (text, "muted")))


def blank(w):
    return ("block pane", L(w))


def user(w, q, time):
    segs = [("  ❯ ", "accent panel bold"), (q, "fg panel bold")]
    used = sum(cw(t) for t, _ in segs)
    if used + cw(time) + 2 > w:
        return ("block pane", fit(segs, w, "panel"))
    return ("block pane", segs + [(" " * (w - used - cw(time) - 2), "panel"), (time, "dim panel"), ("  ", "panel")])


def thought_header(w, secs, streaming=False, glyph="◆"):
    """› when the step is open or selected; ◆ only when it is collapsed and not selected."""
    g = f"  {glyph} "
    if streaming:
        return ("block pane", L(w, (g, "thinking"), ("Thinking…", "fg bold"), (f" {secs}", "muted"),
                                right=[("⠋", "accent")]))
    return ("block pane", L(w, (g, "thinking"), ("Thought", "fg bold"), (f" for {secs}", "muted")))


def thought_body(w, lines, mode):
    """One blank row, then body rows at column 4 in the thinking role (not italic), no rail.
    Truncated: … then the last 3 rows. Collapsed: no rows."""
    body = []
    for line in lines:
        body += [plain(r) for r in wrap_segs([(line, "")], w - 6, w - 6)]
    if mode == "collapsed":
        return []
    if mode == "truncated" and len(body) > 3:
        body = ["…"] + body[-3:]
    if not body:
        return []
    return [blank(w)] + [("block pane", L(w, ("    " + t, "thinking"))) for t in body]


def tool_row(w, verb, what, glyph="◆"):
    return ("block pane", L(w, (f"  {glyph} ", "tool"), (verb, "fg bold"), (" " + what, "muted")))


TEXT_KINDS = ("p", "h1", "h2", "h3", "ol", "ul")


def reply(w, blocks, meta, time):
    """Reply rows: text at column 2, the 24h time at the right of the first row, the meta line.
    The time shows only when the first row is a text row: a reply that starts with a table or
    a code block shows no time."""
    inner = w - 4
    timed = bool(blocks) and blocks[0][0] in TEXT_KINDS
    rows = md_rows(blocks, inner, inner - cw(time) - 2 if timed else inner)
    out = []
    for i, r in enumerate(rows):
        segs = [("  ", "")] + r
        if i == 0 and timed:
            used = sum(cw(t) for t, _ in segs)
            segs += [(" " * (w - used - cw(time) - 2), ""), (time, "dim"), ("  ", "")]
            out.append(("block pane", segs))
        else:
            out.append(("block pane", fit(segs, w)))
    m = meta if cw(meta) + 2 <= w else meta.split(" · ")[0]
    out.append(("block pane", L(w, ("  " + m, "dim"))))
    return out


def turn(w, mode, prompt=PROMPT, time="15:31", lines=THOUGHT, secs="4.2s", rep=REPLY, meta=META, tool=None,
         sel_thought=False):
    """One blank row between any two blocks, and one after the last block."""
    glyph = "›" if (mode != "collapsed" or sel_thought) else "◆"
    rows = [user(w, prompt, time), blank(w), thought_header(w, secs, glyph=glyph)] + thought_body(w, lines, mode)
    if tool:
        rows += [blank(w), tool_row(w, *tool)]
    rows += [blank(w)] + reply(w, rep, meta, time) + [blank(w)]
    return rows


def boxed(rows, start, end):
    """Thin selection box on rows[start:end], as Grok's SelectionBox: │ in column 0 and the last column of each
    block row, ┌ ┐ on the row above the block and └ ┘ on the row below it. Those rows are the blank gap rows, so
    the box never adds a row."""
    out = list(rows)

    def frame(row, l, r):
        region, segs = row
        text = plain(segs)
        if text[0] != " " or text[-1] != " ":
            raise SystemExit(f"no free edge column: {text!r}")
        segs = [list(x) for x in segs]
        segs[0][0] = segs[0][0][1:]
        segs[-1][0] = segs[-1][0][:-1]
        return (region, [(l, "border")] + [tuple(x) for x in segs if x[0] != ""] + [(r, "border")])

    for i in range(start, end):
        out[i] = frame(out[i], "│", "│")
    out[start - 1] = frame(out[start - 1], "┌", "┐")
    out[end] = frame(out[end], "└", "┘")
    return out


def screen(w, h, pane, status=None, focus="prompt", follow_end=True):
    top = [header(w), rule(w)]
    bottom = [rule(w), prompt_row(w), rule(w), footer(w), bar(w, focus)]
    stat = [status] if status else []
    room = h - len(top) - len(bottom) - len(stat)
    if len(pane) > room:
        pane = pane[len(pane) - room:] if follow_end else pane[:room]
    pane = pane + [blank(w)] * (room - len(pane))
    return top + pane + stat + bottom


# ---------------------------------------------------------------- states

def t1(w, h):
    pane = [user(w, PROMPT, "15:31"), blank(w), thought_header(w, "2s", streaming=True, glyph="›")]
    pane += thought_body(w, THOUGHT[:4], "truncated")
    return screen(w, h, pane)


def t2(w, h):
    return screen(w, h, turn(w, "expanded"))


def t3(w, h):
    return screen(w, h, turn(w, "collapsed"))


def t4(w, h):
    return screen(w, h, turn(w, "truncated"))


def t5(w, h):
    pane = turn(w, "collapsed") + turn(w, "collapsed", PROMPT2, "15:33", THOUGHT2, "1.8s", REPLY2, META2)
    return screen(w, h, pane, ("status", L(w, ("┊ ", "muted"), ("Thinking blocks: collapsed · ctrl+t to expand", "muted"))))


def t6(w, h):
    pane = turn(w, "expanded") + turn(w, "expanded", PROMPT2, "15:33", THOUGHT2, "1.8s", REPLY2, META2)
    return screen(w, h, pane, ("status", L(w, ("┊ ", "muted"), ("Thinking blocks: expanded · ctrl+t to collapse", "muted"))))


def md_turn(w, mode="expanded", sel_thought=False):
    return turn(w, mode, MD_PROMPT, "17:30", MD_THOUGHT, "2.1s", MD_REPLY, MD_META,
                tool=("Read", "internal/agent/tool_coordinator.go"), sel_thought=sel_thought)


def m1(w, h):
    return screen(w, h, md_turn(w))


def _span(pane, first_pred, last_pred):
    start = next(i for i, r in enumerate(pane) if first_pred(plain(r[1])))
    end = next(i for i in range(start, len(pane)) if last_pred(plain(pane[i][1]))) + 1
    return start, end


def s1(w, h):
    pane = md_turn(w)
    start, end = _span(pane, lambda t: t.startswith("  tool_coordinator.go runs"), lambda t: "claude-sonnet-5-5 ·" in t)
    pane = boxed(pane, start, end)
    return screen(w, h, pane[start - 1:], focus="list", follow_end=False)


def s2(w, h):
    pane = md_turn(w, sel_thought=True)
    start, end = _span(pane, lambda t: "Thought for" in t, lambda t: t.startswith("    I will group it"))
    pane = boxed(pane, start, end)
    return screen(w, h, pane, focus="list", follow_end=False)


def s3(w, h):
    pane = md_turn(w, mode="collapsed", sel_thought=True)
    start, end = _span(pane, lambda t: "Thought for" in t, lambda t: "Thought for" in t)
    pane = boxed(pane, start, end)
    return screen(w, h, pane, focus="list", follow_end=False)


STATES = [
    ("T1", "Streaming: the last lines, for every setting",
     "While the thinking streams, the step is open: › Thinking…, one blank row, then … and the last 3 lines at "
     "column 4 in the thinking color. There is no rail. The setting does not change this state.",
     t1, [(80, 24)]),
    ("T2", "Ended with thinkingFold \"expanded\" (the default)",
     "When the thinking ends, the step stays open: › Thought, one blank row, all its lines, no rail. One blank row "
     "separates every two blocks. The reply has the 24h time at the right of its first row.",
     t2, [(80, 24), (40, 14)]),
    ("T3", "Ended with thinkingFold \"collapsed\"",
     "When the thinking ends, the step folds to one row with ◆.",
     t3, [(80, 24)]),
    ("T4", "Ended with thinkingFold \"truncated\"",
     "When the thinking ends, the step keeps … and its last 3 lines.",
     t4, [(80, 24)]),
    ("T5", "ctrl+t collapsed all, so the next thinking ends collapsed",
     "The setting is \"expanded\". After ctrl+t folds all thinking, the second turn's thinking ends collapsed too.",
     t5, [(80, 24)]),
    ("T6", "ctrl+t expanded all, so the next thinking ends expanded",
     "The setting is \"collapsed\". After ctrl+t opens all thinking, the second turn's thinking ends expanded.",
     t6, [(80, 24)]),
    ("M1", "Reply markdown in style A",
     "Headings without #, in h1/h2/h3 colors and bold, with one blank row under each. Inline code in blue bold with no backticks. A muted • for "
     "bullets and muted numbers. Tables in a dim box with a bold header. A code block on the code panel with no "
     "language row. At 80×24 the pane follows the end; at 120×40 the whole turn shows.",
     m1, [(80, 24), (120, 40)]),
    ("S1", "The reply is selected: thin box",
     "Tab, then k to the reply. A │ in the first and the last column of each reply row; ┌ ┐ on the blank row above "
     "and └ ┘ on the blank row below. No row is added. The accent ┃ gutter is gone.",
     s1, [(80, 24)]),
    ("S2", "An open thinking step is selected",
     "The box goes around the open step: header, blank row, body. ┌ ┐ on the blank row above, └ ┘ on the blank row "
     "below, before the Read step.",
     s2, [(80, 24)]),
    ("S3", "A collapsed thinking step is selected",
     "A one-row block gets the same box: ┌ ┐ on the blank row above, │ beside the row, └ ┘ on the blank row below. "
     "The glyph is › because the step is selected.",
     s3, [(80, 24)]),
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
