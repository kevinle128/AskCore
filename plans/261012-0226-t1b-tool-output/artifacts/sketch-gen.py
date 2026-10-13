#!/usr/bin/env python3
"""Generate exact-width sketches for the tool output, transcript view and /copy TUI design.

Each sketch line is a list of segments (text, roles). design.md uses the plain text;
preview.html renders the roles as colors. Both come from this file, so they cannot disagree.
Patterns follow docs/tui-style-guide.md; helpers are copied from the job notices generator.

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


def trunc(s, n):
    if cw(s) <= n:
        return s
    out = ""
    for c in s:
        if cw(out + c) > n - 1:
            break
        out += c
    return out + "…"


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

MODEL = "claude-sonnet-5-5"
BUILD = "npm run build"
TESTS = "go test ./... -count=1"
LOGS = "tail -f server.log"
Q1 = "run the build in the background and tell me when it is done"
Q2 = "meanwhile, what does the build step do?"
Q3 = "also check the lint"


def header(w):
    if w < 60:
        top = [L(w, ("◢◤ ", "logo"), ("ask", "logo"), ("  v0.1.0 · 7c1e", "muted"))]
    else:
        top = [L(w, ("◢◤ ", "logo"), ("ask", "logo"), ("   v0.1.0 · leader 7c1e · ~/src/web", "muted"))]
    label = "  model  " if w >= 60 else "  "
    return top + [L(w, (label, "muted"), ("anthropic/" + MODEL, "")), L(w)]


def user(w, q, time):
    rows = wrap(q, w - 12, "", "")
    out = [L(w, ("  ", "panel"), ("❯ ", "accent bold panel"), (rows[0], "user panel"),
             right=[(time + "  ", "dim panel")], fill="panel")]
    for r in rows[1:]:
        out.append(L(w, ("    ", "panel"), (r, "user panel"), fill="panel"))
    return out + [L(w)]


def answer(w, text, tail=True):
    rows = wrap(text, w - 4)
    out = [L(w, ("  ◆ " if k == 0 else "    ", "assistant bold"), (r, "")) for k, r in enumerate(rows)]
    return out


def meta(w, rest="2.1s · ↑3.2k ↓140 · $0.002"):
    return [L(w, ("    " + trunc(MODEL + " · " + rest, w - 4), "dim")), L(w)]


def tool_line(w, name, args, status, status_role, mark="●"):
    """`  ● <name>  <args>  <status> ` as the tool line of the style guide. Args are cut first."""
    left = [("  ", ""), (mark + " ", "tool"), (name, "tool bold")]
    right = [(status, status_role), (" ", "")]
    room = w - sum(cw(t) for t, _ in left) - sum(cw(t) for t, _ in right) - 4
    if room >= 2:
        left.append(("  " + trunc(args, room), "muted"))
    return L(w, *left, right=right)


def job_line(w, job, label, status, role):
    """The row that replaces a completion notice: `  ● job <id>  <label>  <status> `."""
    return tool_line(w, "job " + job, label, status, role)


def rail_status(w, glyph_role, word, tail):
    return L(w, ("┊ ", "dim"), ("◌ ", glyph_role), (word, glyph_role + " bold shimmer" if glyph_role == "accent" else glyph_role),
             (tail, "muted"))


def rail_jobs(w, jobs):
    """Idle rail: `┊ ◌ 1 background job · bash-1 npm run build`. Under 60 columns: `┊ ◌ 1 job`."""
    n = len(jobs)
    if w < 60:
        return L(w, ("┊ ", "dim"), ("◌ ", "muted"), (f"{n} job" + ("s" if n > 1 else ""), "muted"))
    word = f"{n} background job" + ("s" if n > 1 else "")
    if n == 1:
        tail = " · " + jobs[0][0] + " " + jobs[0][1]
    else:
        tail = " · " + ", ".join(j for j, _ in jobs)
    used = cw("┊ ◌ " + word)
    return L(w, ("┊ ", "dim"), ("◌ ", "muted"), (word, "muted"), (trunc(tail, w - used), "dim"))


def working(w, secs, jobs=0):
    short = w < 60
    tail = f" {secs}s · " + ("esc" if short else "esc to interrupt")
    if jobs:
        tail += f" · {jobs} job" + ("s" if jobs > 1 else "")
    return rail_status(w, "accent", "Working…", tail)


def queue_rows(w, items):
    rail = ("┆ ", "dim")
    out = []
    for kind, text in items:
        role = "assistant" if kind == "steer" else "tool"
        label = "⧗ steer   " if kind == "steer" else "⧗ next    "
        out.append(L(w, rail, (label, role), (trunc(text, w - 12), "")))
    hint = "alt+↑ edit queue · dashed rail = not sent yet"
    out.append(L(w, rail, (trunc(hint, w - 2), "dim italic")))
    return out


def editor(w, draft=None):
    inner = w - 2
    placeholder = "Ask anything  ·  / commands  ·  ctrl+l model" if w >= 60 else "Ask anything"
    if draft is None:
        segs = [("❯ ", "accent bold"), ("▌", "cursor"), (placeholder, "dim italic")]
    else:
        segs = [("❯ ", "accent bold"), (draft, ""), ("▌", "cursor")]
    used = 1 + sum(cw(t) for t, _ in segs)
    return [L(w, ("╭" + "─" * inner + "╮", "border")),
            [("│ ", "border")] + segs + [(" " * (inner - used), ""), ("│", "border")],
            L(w, ("╰" + "─" * inner + "╯", "border"))]


def ctx_bar(pct, n=8):
    full = round(pct / 100 * n)
    return ("▰" * full + "▱" * (n - full), "accent")


def footer(w, pct=12, cost="$0.01"):
    if w < 60:
        return [L(w, ("  ● ", "success"), ("7c1e", "muted"), right=[(MODEL, "assistant bold"), (f" {pct}% ", "muted")])]
    return [L(w, ("  ● ", "success"), ("leader 7c1e", "muted"), ("   ⌂ ", "muted"), ("~/src/web", "muted"),
              right=[(MODEL, "assistant bold"), ("  ", ""), ctx_bar(pct), (f" {pct}%", "muted"), (f"  {cost} ", "muted")])]


def frame(w, h, scroll, live):
    body = len(scroll) + len(live)
    if body >= h:
        keep = h - len(live)
        return scroll[len(scroll) - keep:] + [MARK] + live
    return [L(w)] * (h - body) + scroll + [MARK] + live


# ---------------------------------------------------------------- tool output content

TAB = 4
import pathlib
# The real file, so the sketch shows real text. Its first 14 lines appear at 80x24.
STATUS_GO = [l.expandtabs(TAB) for l in
             (pathlib.Path(__file__).resolve().parents[3] / "internal/tui/status.go").read_text().rstrip("\n").split("\n")]
STATUS_TOTAL = len(STATUS_GO)

TEST_OUT = [l.expandtabs(TAB) for l in """=== RUN   TestStatusRowOrder
--- PASS: TestStatusRowOrder (0.00s)
=== RUN   TestStatusJobsRow
--- PASS: TestStatusJobsRow (0.00s)
=== RUN   TestStatusCopiedNotice
    status_test.go:88: row = "┊ Copied", want "┊ ✓ Copied"
--- FAIL: TestStatusCopiedNotice (0.00s)
=== RUN   TestStatusNoModel
--- PASS: TestStatusNoModel (0.00s)
FAIL
FAIL	AskCore/internal/tui	0.412s
FAIL""".split("\n")]

DIFF = [(" ", "90", "	case s.failure != nil && s.failure.rev == s.editor.Revision():"),
        (" ", "91", "		return th.Render(RoleDim, GlyphRailStatus) + \" \" + th.Render(RoleError, GlyphFail+\" \"+s.failure.text)"),
        (" ", "92", "	case s.now().Before(s.copiedUntil):"),
        ("-", "93", "		return s.noticeRow(noticeView{text: \"Copied\"})"),
        ("+", "93", "		return s.noticeRow(noticeView{text: \"Copied\", ok: true})"),
        (" ", "94", "	case s.waitRow() != \"\":")]

BIG_TAIL = ["ok  	AskCore/internal/tools	3.104s".expandtabs(TAB),
            "ok  	AskCore/internal/tui	9.877s".expandtabs(TAB),
            "ok  	AskCore/internal/tuiclient	1.220s".expandtabs(TAB),
            "ok  	AskCore/internal/workspace	0.031s".expandtabs(TAB),
            "ok  	AskCore/pkg/protocol	0.044s".expandtabs(TAB)]
SPILL = "/tmp/ask-jobs-81f2/bash-4.log"

FIX_Q = "the status test fails, please fix it"


def out_row(w, line):
    """One output row under a tool line: four spaces, the line cut to one row with `…`."""
    return L(w, ("    ", ""), (trunc(line, w - 4), ""))


def cut_cols(line, n):
    """Split a line into pieces of at most n columns (hard wrap by columns)."""
    out, cur = [], ""
    for c in line:
        if cw(cur + c) > n:
            out.append(cur)
            cur = ""
        cur += c
    return out + [cur]


def full_rows(w, line):
    """Transcript view: a long line wraps; nothing is cut."""
    return [L(w, ("    ", ""), (p, "")) for p in cut_cols(line, w - 4)]


def hint(w, n):
    return L(w, ("    " + trunc(f"… +{n} lines (ctrl+o to expand)", w - 4), "muted"))


def trunc_line(w, text, path=""):
    """Model-side truncation: one muted row. When the path does not fit, it moves to its own
    row(s) and wraps; the path is never cut."""
    whole = text + (" · full output: " + path if path else "")
    if cw(whole) <= w - 4:
        return [L(w, ("    " + whole, "muted"))]
    rows = [L(w, ("    " + trunc(text, w - 4), "muted"))]
    if path:
        rows += [L(w, ("    " + p, "muted")) for p in cut_cols("full output: " + path, w - 4)]
    return rows


def edit_line(w, path, add, rem, status="✓ 0.1s"):
    left = [("  ", ""), ("● ", "tool"), ("edit", "tool bold")]
    stat = [("  +" + str(add), "success"), (" -" + str(rem), "error")]
    right = [(status, "success"), (" ", "")]
    room = w - 8 - sum(cw(t) for t, _ in stat) - sum(cw(t) for t, _ in right) - 4
    if room >= 2:
        left.append(("  " + trunc(path, room), "muted"))
    return L(w, *left, stat, right=right)


def diff_row(w, sign, num, text):
    role = {"+": "success", "-": "error", " ": "muted"}[sign]
    parts = cut_cols(text.expandtabs(TAB), w - 11)
    rows = [L(w, ("    ", ""), (f"{num:>4} ", "dim"), (sign + " ", role), (parts[0], "" if sign == " " else role))]
    rows += [L(w, (" " * 11, ""), (p, "" if sign == " " else role)) for p in parts[1:]]
    return rows


def collapsed(w):
    """History with collapsed previews: read head 10, edit diffstat, bash tail 5."""
    rows = header(w) + user(w, FIX_Q, "15:20")
    rows += [tool_line(w, "read", "internal/tui/status.go", "✓ 0.1s", "success")]
    rows += [out_row(w, l) for l in STATUS_GO[:10]] + [hint(w, STATUS_TOTAL - 10), L(w)]
    rows += [edit_line(w, "internal/tui/status.go", 1, 1), L(w)]
    rows += [tool_line(w, "bash", "go test ./internal/tui -run TestStatus", "✗ exit 1", "error")]
    rows += [out_row(w, l) for l in TEST_OUT[-5:]] + [hint(w, len(TEST_OUT) - 5), L(w)]
    return rows


def reply_tail(w):
    return answer(w, "The copy note had no ok mark. I added it; run the test again to check.") + meta(w)


def full_transcript(w):
    """The transcript view: every output in full, the diff of the edit."""
    rows = header(w) + user(w, FIX_Q, "15:20")
    rows += [tool_line(w, "read", "internal/tui/status.go", "✓ 0.1s", "success")]
    rows += [r for l in STATUS_GO for r in full_rows(w, l)] + [L(w)]
    rows += [edit_line(w, "internal/tui/status.go", 1, 1)]
    rows += [r for s, n, t in DIFF for r in diff_row(w, s, n, t)] + [L(w)]
    rows += [tool_line(w, "bash", "go test ./internal/tui -run TestStatus", "✗ exit 1", "error")]
    rows += [r for l in TEST_OUT for r in full_rows(w, l)] + [L(w)]
    rows += reply_tail(w)
    return rows


def view_frame(w, h, content, top, marker=False):
    """The transcript view on the alternate screen: header row, content window, key row."""
    body = h - 2
    win = content[top:top + body]
    win += [L(w)] * (body - len(win))
    pos = f"{top + 1}–{min(top + body, len(content))} of {len(content)} "
    title = "Showing detailed transcript · ctrl+o or Esc to return" if w >= 60 else "Transcript · esc to return"
    right = [(pos, "dim")]
    if marker and w < 60:
        title = "Transcript · esc"
    if marker:
        right = [("new output · End ", "accent bold")] if w < 100 else [("new output · End  ", "accent bold"), (pos, "dim")]
    head = L(w, (" " + title, "muted"), right=right)
    keys = "↑↓ scroll · PgUp PgDn page · Home End" if w >= 60 else "↑↓ PgUp PgDn Home End"
    return [head] + win + [L(w, (" " + keys, "dim"))]


# ---------------------------------------------------------------- states

def s_collapsed(w, h):
    live = editor(w, draft="also run go vet") + footer(w)
    return frame(w, h, collapsed(w) + reply_tail(w), live)


def s_streaming(w, h):
    scroll = header(w) + user(w, "run all tests", "15:31")
    run = [tool_line(w, "bash", "go test ./... -count=1", "running ⠋", "warning", mark="◇")]
    run += [out_row(w, l) for l in BIG_TAIL] + [hint(w, 37), L(w)]
    live = run + [working(w, 12)] + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def s_truncated(w, h):
    scroll = header(w) + user(w, "run all tests verbose, then read the big fixture", "15:40")
    scroll += [tool_line(w, "bash", "go test ./... -count=1 -v", "✓ 48.2s", "success")]
    scroll += [out_row(w, l) for l in BIG_TAIL] + [hint(w, 1995)]
    scroll += trunc_line(w, "model saw 2000 lines (50 KB) of 2.4 MB", SPILL) + [L(w)]
    scroll += [tool_line(w, "read", "testdata/events.jsonl", "✓ 0.2s", "success")]
    scroll += [out_row(w, '{"type":"agent_start","ts":1760256001}'), hint(w, 1999)]
    scroll += trunc_line(w, "model saw 2000 of 5120 lines · next offset 2001") + [L(w)]
    scroll += answer(w, "All packages pass. The fixture is long; I read the first 2000 lines.") + meta(w)
    return frame(w, h, scroll, editor(w) + footer(w))


def s_view_top(w, h):
    return view_frame(w, h, full_transcript(w), 0)


def s_view_diff(w, h):
    rows = full_transcript(w)
    top = next(i for i, r in enumerate(rows) if plain(r).lstrip().startswith("● edit"))
    return view_frame(w, h, rows, max(0, top - 3))


def s_view_new_output(w, h):
    rows = full_transcript(w)
    return view_frame(w, h, rows, 2, marker=True)


def s_back(w, h):
    live = editor(w, draft="also run go vet") + footer(w)
    return frame(w, h, collapsed(w) + reply_tail(w), live)


def note_row(w, text, role="dim", ok=False):
    segs = [("┊ ", "dim")]
    if ok:
        segs.append(("✓ ", "success"))
    segs.append((trunc(text, w - cw("┊ ✓ ")), role))
    return L(w, *segs)


def copy_state(text, role="dim", ok=False):
    def fn(w, h):
        scroll = collapsed(w) + reply_tail(w)
        return frame(w, h, scroll, [note_row(w, text, role, ok)] + editor(w) + footer(w))
    return fn


STATES = [
    ("P1", "Collapsed previews in the main view",
     "Each tool line keeps its one row and gets a short preview: read shows the first 10 lines, bash the last 5, "
     "edit only +N -M on the tool line. A muted hint counts the hidden lines.", s_collapsed, [(80, 24), (40, 14)]),
    ("P2", "A bash call streams its output",
     "While bash runs, its tool block is in the live region and the preview shows the latest lines from the "
     "partial tool results. Other tools show their preview when they end.", s_streaming, [(80, 24), (40, 14)]),
    ("P3", "The model saw only part of the output",
     "A muted line under the preview says what the model saw and where the full output is.", s_truncated,
     [(80, 24), (40, 14)]),
    ("V1", "Transcript view opened with ctrl+o",
     "A temporary alternate screen with the whole session and every output in full. It starts at the end; "
     "this sketch shows it after Home.", s_view_top, [(80, 24), (40, 14)]),
    ("V2", "Transcript view: the edit diff",
     "The edit shows its diff with line numbers. Removed lines use the error role, added lines the success role.",
     s_view_diff, [(80, 24)]),
    ("V3", "Transcript view: new output while scrolled up",
     "Events keep updating the view. When the user is not at the end, the position stays and the header shows "
     "a marker; End jumps to the end and follows it again.", s_view_new_output, [(80, 24), (40, 14)]),
    ("R1", "Back in the inline view",
     "ctrl+o or Esc leaves the alternate screen. The inline view is as before, and the draft is kept.", s_back,
     [(80, 24)]),
    ("C1", "/copy with a native clipboard command",
     "pbcopy, wl-copy, xclip, xsel or termux-clipboard-set returned success.", copy_state("Copied", "dim", True),
     [(80, 24)]),
    ("C2", "/copy through OSC 52",
     "Over SSH, or with no native command, the text goes to the terminal. The terminal gives no answer, "
     "so the note does not say Copied.", copy_state("Sent to the terminal clipboard"), [(80, 24)]),
    ("C3", "/copy with no assistant text",
     "No assistant message with text exists in this session yet.", copy_state("Nothing to copy"), [(80, 24)]),
    ("C4", "/copy fails",
     "The native command failed and the text is over the OSC 52 limit.",
     copy_state("Not copied: 180 KB is over the terminal clipboard limit of 75 KB", "error"), [(80, 24), (40, 14)]),
]


def block(rows):
    parts, cur = [], []
    for r in rows:
        if r == MARK:
            if cur:
                parts.append("```text\n" + "\n".join(cur) + "\n```")
            parts.append("`-- live region starts --`")
            cur = []
        else:
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
            if len([r for r in rows if r != MARK]) != h:
                raise SystemExit(f"{sid} {w}x{h}: {len(rows)} rows")
            for r in rows:
                if r != MARK and cw(plain(r)) != w:
                    raise SystemExit(f"{sid} {w}: width {cw(plain(r))}: {plain(r)!r}")
            out.append(f"{w}×{h}:\n")
            out.append(block(rows) + "\n")
    print("\n".join(out))


if __name__ == "__main__":
    main()
