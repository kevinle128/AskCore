#!/usr/bin/env python3
"""Generate exact-width sketches for the project trust TUI design.

Screen model: the alternate screen as the current goldens draw it (header row, rule, block pane,
status row, rule, prompt pane or dialog slot, rule, footer row, shortcut bar).
Dialog look: the row menu and boxes of the provider setup design (TestProviderRowMenu,
TestRemoveConfirmFlow): rounded box, `❯` on the `selected` row, a dim italic hint row under the box.

Each screen row is (region, segments). A segment is (text, roles). design.md uses the plain
text; preview.html renders the roles as colors and the region names beside the terminal.

Usage: python3 sketch-gen.py > sketches.md
"""
import re
import unicodedata

SHORT = 60  # shortWidth in internal/tui: under 60 columns the short forms apply


def cw(s):
    return sum(0 if unicodedata.combining(c) else (2 if unicodedata.east_asian_width(c) in "WF" else 1) for c in s)


def plain(row):
    return "".join(t for t, _ in row)


def L(w, *segs, right=None, fill=""):
    right = list(right or [])
    used = sum(cw(t) for t, _ in segs) + sum(cw(t) for t, _ in right)
    if used > w:
        raise SystemExit(f"row too wide ({used}>{w}): {plain(list(segs) + right)!r}")
    return list(segs) + [(" " * (w - used), fill)] + right


def cut(text, room):
    """Cut text to room columns with … at the end."""
    if cw(text) <= room:
        return text
    out = ""
    for ch in text:
        if cw(out + ch) > room - 1:
            break
        out += ch
    return out + "…"


def wrap_words(segs, first, rest):
    """Wrap a segment list at spaces; the first row has `first` columns, the others `rest`."""
    rows, cur, used, limit = [], [], 0, first
    for text, role in segs:
        for word in re.split(r"( )", text):
            if word == "" or (word == " " and used == 0):
                continue
            if word != " " and used + cw(word) > limit and used > 0:
                while cur and cur[-1][0] == " ":
                    cur.pop()
                rows.append(cur)
                cur, used, limit = [], 0, rest
            # A word wider than a whole row (a long path) continues by columns; it is never cut.
            while word != " " and cw(word) > limit - used:
                head = ""
                for ch in word:
                    if cw(head + ch) > limit - used:
                        break
                    head += ch
                cur.append((head, role))
                rows.append(cur)
                cur, used, limit = [], 0, rest
                word = word[len(head):]
            cur.append((word, role))
            used += cw(word)
    rows.append(cur)
    return rows


def wrap_cols(text, room):
    """Wrap by columns (a path is never cut; it continues on the next row)."""
    rows, cur = [], ""
    for ch in text:
        if cw(cur + ch) > room:
            rows.append(cur)
            cur = ""
        cur += ch
    rows.append(cur)
    return rows


# ---------------------------------------------------------------- fixed screen parts

MODEL = "anthropic/claude-sonnet-5-5"
CWD = "~/src/acme"


def header(w):
    return L(w, (" ◢◤ ask", "logo"))


def rule(w):
    return [("─" * w, "border")]


def footer(w, cwd=CWD, model=True):
    if w >= SHORT:
        left = [(" ⌂ " + cwd, "muted")]
        # Before the first session the footer row has no model text, only the meter (footer.go).
        right = [(MODEL, "assistant"), ("  ", ""), ("▱▱▱▱▱▱▱▱ 0% ", "accent")] if model else [("▱▱▱▱▱▱▱▱ 0% ", "accent")]
        return L(w, *left, right=right)
    right = [(MODEL, "assistant"), (" 0% ", "accent")] if model else [("0% ", "accent")]
    return L(w, right=right)


def bar(w):
    if w >= SHORT:
        return L(w, (" Tab blocks · ctrl+t thinking · ctrl+l model · / commands", "dim"))
    return L(w, (" Tab blocks · / commands", "dim"))


def prompt_row(w, draft=None):
    if draft is not None:
        return L(w, (" ❯ ", "accent bold"), (draft, ""), ("▌", "cursor"))
    hint = "Ask anything  ·  / commands  ·  ctrl+l model" if w >= SHORT else "Ask anything"
    return L(w, (" ❯ ", "accent bold"), (hint, "dim italic"))


def status_row(w, segs):
    return L(w, ("┊ ", "dim"), *segs)


# ---------------------------------------------------------------- box (dialog slot)

def box_top(w, title):
    text = " " + title + " "
    fill = w - 2 - cw(text) - 1
    return [("╭─", "border"), (text, "accent bold"), ("─" * fill + "╮", "border")]


def box_row(w, segs, sel=False, right=None):
    inner = w - 4
    right = list(right or [])
    used = sum(cw(t) for t, _ in segs) + sum(cw(t) for t, _ in right)
    if used > inner:
        raise SystemExit(f"box row too wide ({used}>{inner}): {plain(segs)!r}")
    bg = " selected" if sel else ""
    body = [(t, (r + bg).strip()) for t, r in segs] + [(" " * (inner - used), bg.strip())] + \
           [(t, (r + bg).strip()) for t, r in right]
    return [("│ ", "border")] + body + [(" │", "border")]


def box_bottom(w):
    return [("╰" + "─" * (w - 2) + "╯", "border")]


def hint_row(w, long, short):
    text = long if w >= SHORT and cw(long) <= w - 3 else short
    return L(w, ("   ", ""), (text, "dim italic"))


def menu(w, options, sel, tags=None, room_rows=None):
    """Option rows: `❯ label  description` at 60 columns and more, `❯ label` under 60.

    options: list of (label, short label, description). tags: {index: tag} shown at the right.
    room_rows: when set and the list does not fit, the window keeps the selected row and the
    last row is `… <n> more`.
    """
    tags = tags or {}
    wide = w >= SHORT
    labw = max(cw(o[0]) for o in options) + 2
    rows = []
    for i, (label, short, desc) in enumerate(options):
        mark = [("❯ ", "accent bold")] if i == sel else [("  ", "")]
        right = [(tags[i], "muted"), (" ", "")] if i in tags else []
        if wide:
            room = w - 4 - 2 - labw - sum(cw(t) for t, _ in right)
            segs = mark + [(label.ljust(labw), "fg bold" if i == sel else "")] + [(cut(desc, room), "muted")]
        else:
            segs = mark + [(short, "fg bold" if i == sel else "")]
        rows.append(box_row(w, segs, sel=(i == sel), right=right))
    if room_rows is not None and len(rows) > room_rows:
        keep = room_rows - 1
        start = min(max(0, sel - keep + 1), len(rows) - keep)
        hidden = len(rows) - keep
        rows = rows[start:start + keep] + [box_row(w, [("  … %d more" % hidden, "muted")])]
    return rows


def slot(rows):
    return [("dialog", r) for r in rows]


# ---------------------------------------------------------------- block pane content

def text_block(w, segs, indent=2, cont=None):
    """A notice block: wrapped at spaces, indent columns at the left, 2 blank columns at the right."""
    cont = indent if cont is None else cont
    first, rest = w - indent - 2, w - cont - 2
    out = []
    for i, r in enumerate(wrap_words(segs, first, rest)):
        out.append([(" " * (indent if i == 0 else cont), "")] + r)
    return out


def path_rows(w, path, role, indent):
    return [[(" " * indent, ""), (r, role)] for r in wrap_cols(path, w - indent - 2)]


def prompt_band(w, text, time):
    used = 4 + cw(text) + cw(time) + 2
    return [("  ", "panel"), ("❯ ", "accent bold panel"), (text, "user panel"),
            (" " * (w - used), "panel"), (time, "dim panel"), ("  ", "panel")]


def turn(w):
    return [
        [prompt_band(w, "why does TestInvoiceTotals fail?", "09:12")],
        text_block(w, [("The test rounds each line before the sum. The fix rounds once, at the end.", "")]),
        [[("  ", ""), ("claude-sonnet-5-5 · 6.1s · ↑9.2k ↓380 · $0.006", "dim")]],
    ]


def screen(w, h, blocks, below=None, status=None, cwd=CWD, model=True, draft=None):
    """A whole screen. blocks: list of blocks (lists of rows). below: dialog slot rows, or None
    for the prompt pane. The pane follows the end: when the blocks do not fit, the top rows go."""
    top = [("header", header(w)), ("rule", rule(w))]
    bottom = []
    for s in status or []:
        bottom.append(("status", status_row(w, s)))
    bottom.append(("rule", rule(w)))
    bottom += below if below is not None else [("prompt pane", prompt_row(w, draft))]
    bottom += [("rule", rule(w)), ("footer", footer(w, cwd, model)), ("bar", bar(w))]
    n = h - len(top) - len(bottom)
    lines = []
    for b in blocks:
        lines += b + [[]]
    lines = lines[-n:] if len(lines) > n else lines + [[]] * (n - len(lines))
    pane = [("block pane", L(w, *r)) for r in lines]
    return top + pane + bottom


# ---------------------------------------------------------------- trust prompt parts

TITLE = "Trust this project folder?"


def prompt_options(parent):
    return [
        ("Not now", "Not now", "Do not load them now. Ask again in the next session."),
        ("Trust this folder", "Trust this folder", "Load them now and in later sessions."),
        ("Trust parent folder", "Trust parent folder", f"Trust {parent} and every folder in it."),
        ("Do not trust", "Do not trust", "Do not load them. Do not ask again."),
    ]


PROMPT_HINT = ("↑↓ choose  ·  enter confirm  ·  esc not now  ·  your draft is kept",
               "↑↓ · enter · esc not now")


def explain(w, root, gated_rel, gated_abs, warning=None):
    """The explanation block in the block pane while the trust box is open."""
    blocks = []
    if warning:
        blocks.append(text_block(w, [("Warning: " + warning, "warning")]))
    rows = text_block(w, [(root, "fg bold"), (" has project files that Ask has not read or run:", "")])
    # Each file row records how many entries it starts, so that "… n more" counts entries.
    files = []
    if gated_rel:
        for r in text_block(w, [("  ".join(gated_rel), "code")], indent=4):
            files.append((r, len([t for t, _ in r if t.strip()])))
    for p in gated_abs:
        for i, r in enumerate(path_rows(w, p, "code", 4)):
            files.append((r, 1 if i == 0 else 0))
    if len(files) > 4:
        hidden = sum(n for _, n in files[3:])
        files = files[:3] + [([("    ", ""), ("… %d more" % hidden, "muted")], 0)]
    rows += [r for r, _ in files]
    rows += text_block(w, [("If you trust this folder, Ask loads its project settings, system prompt files, "
                            "skills, prompts, extensions and MCP config.", "")])
    rows += text_block(w, [("Trust does not give permission to run shell commands or tools. "
                            "Trust does not apply to AGENTS.md or CLAUDE.md.", "")])
    blocks.append(rows)
    return blocks


def trust_box(w, parent, sel=0, room=None):
    rows = [box_top(w, TITLE)] + menu(w, prompt_options(parent), sel, room_rows=room) + [box_bottom(w)]
    return slot(rows + [hint_row(w, *PROMPT_HINT)])


GATED = [".ask/settings.json", ".ask/skills", ".agents/skills"]


# ---------------------------------------------------------------- states

def s0(w, h):
    return screen(w, h, [], status=[[("Starting, please wait…", "dim")]], model=False,
                  draft="fix the failing invoice test")


def p1(w, h):
    return screen(w, h, explain(w, CWD, GATED, []), below=trust_box(w, "~/src"), model=False)


LONG_ROOT = "~/work/clients/northwind-traders/platform/services/billing-reconciliation-worker"
LONG_REL = [".ask/settings.json", ".ask/SYSTEM.md", ".ask/APPEND_SYSTEM.md", ".ask/mcp.json",
            ".ask/models.json", ".ask/skills", ".ask/prompts", ".ask/extensions", ".agents/skills"]
LONG_ABS = ["~/work/clients/northwind-traders/.agents/skills"]


def p2(w, h):
    return screen(w, h, explain(w, LONG_ROOT, LONG_REL, LONG_ABS),
                  below=trust_box(w, "~/…/services"), cwd="~/…/billing-reconciliation-worker", model=False)


def p2_narrow(w, h):
    # 40×12: the slot has 5 rows, so the option list is a window with "… n more".
    return screen(w, h, explain(w, LONG_ROOT, LONG_REL, LONG_ABS),
                  below=trust_box(w, "~/…/services", sel=0, room=2), model=False)


CHECK_WARN = ("cannot check the project files of /Users/kevin/src/acme; "
              "the project needs a trust decision")


def p3(w, h):
    return screen(w, h, explain(w, CWD, [".ask/skills"], [], warning=CHECK_WARN),
                  below=trust_box(w, "~/src", sel=0), model=False)


def p4(w, h):
    # The highlight moved down twice with ↓; Enter now would trust the parent folder.
    return screen(w, h, explain(w, CWD, GATED, []), below=trust_box(w, "~/src", sel=2), model=False)


def p5(w, h):
    rows = [box_top(w, TITLE)] + menu(w, prompt_options("~/src"), 1)
    rows.append(box_row(w, [(" ✗ ", "error"), ("Not saved: no answer from the leader in 5s.", "error")]))
    rows.append(box_bottom(w))
    return screen(w, h, explain(w, CWD, GATED, []), below=slot(rows + [hint_row(w, *PROMPT_HINT)]), model=False)


def t0(w, h):
    return screen(w, h, turn(w), status=[[("Reading project trust…", "dim")]])


def ok_note(w, text):
    return text_block(w, [("✓ ", "success"), (text, "")], cont=4)


def r1(w, h):
    return screen(w, h, [ok_note(w, "Trusted ~/src/acme. Its project files load in this session.")],
                  draft="fix the failing invoice test")


def r2(w, h):
    return screen(w, h, [ok_note(w, "Trusted ~/src and every folder in it. The project files of ~/src/acme "
                                    "load in this session.")], draft="fix the failing invoice test")


def muted_note(w, text):
    return text_block(w, [(text, "muted")])


NOT_NOW = ("The project files of {root} are not loaded in this session. The question comes again in the next session. "
           "To save a choice, use /trust.")


def r3(w, h):
    root = CWD if w >= SHORT else "~/项目/客户-acme"
    return screen(w, h, [muted_note(w, NOT_NOW.format(root=root))], draft="fix the failing invoice test",
                  cwd=root)


def r4(w, h):
    return screen(w, h, [muted_note(w, "Saved: do not trust ~/src/acme. Its project files are not loaded. "
                                       "To change it, use /trust.")])


def u1(w, h):
    return screen(w, h, [muted_note(w, "The project files of ~/src/acme are not loaded (--no-approve).")])


STORE_WARN = ("trust store /Users/kevin/.ask/trust.json is corrupt or unsafe; "
              "no project is trusted until you fix or remove it")


def w1(w, h):
    return screen(w, h, [text_block(w, [("Warning: " + STORE_WARN, "warning")])])


def diag_block(w, root_disp, short=False):
    """Settings problems, grouped by file. Key in fg bold, text in muted."""
    rows = text_block(w, [("Settings problems", "warning bold")])
    if short:
        groups = [(root_disp + "/.ask/settings.json",
                   [("defaultProjectTrust", "a project file cannot set this key; it is ignored")])]
        for path, items in groups:
            rows += path_rows(w, path, "muted", 4)
            for key, text in items:
                rows += text_block(w, [(key + ": ", "fg bold"), (text, "muted")], indent=6, cont=8)
        return rows
    groups = [
        (root_disp + "/.ask/settings.json", [
            ("defaultProjectTrust", "a project file cannot set this key; it is ignored"),
            ("DefaultModel", "unknown key; did you mean defaultModel?"),
        ]),
        ("~/.ask/settings.json", [
            ("thinkingFold", 'must be "expanded", "truncated" or "collapsed"; the default is used'),
        ]),
    ]
    for path, items in groups:
        rows += path_rows(w, path, "muted", 4)
        for key, text in items:
            rows += text_block(w, [(key + ": ", "fg bold"), (text, "muted")], indent=6, cont=8)
    return rows


def d1(w, h):
    root = CWD if w >= SHORT else "~/项目/客户-acme"
    return screen(w, h, [ok_note(w, "Trusted " + root + ". Its project files load in this session."),
                         diag_block(w, root, short=w < SHORT)], cwd=root)


# ---------------------------------------------------------------- /trust

def trust_cmd_options(root, parent):
    return [
        ("Trust this folder", "Trust this folder", f"Save trust for {root} only."),
        ("Trust parent folder", "Trust parent folder", f"Trust {parent} and every folder in it."),
        ("Do not trust", "Do not trust", "Do not load the project files here."),
        ("Forget the saved choice", "Forget saved choice", f"Remove the entry of {parent}. Ask again."),
    ]


CMD_HINT = ("↑↓ choose  ·  enter save  ·  esc close  ·  applies to the next session",
            "enter save · esc close")


def cmd_box(w, saved, session, sel, current, room=None, options=None):
    opts = options or trust_cmd_options(CWD, "~/src")
    rows = [box_top(w, "Project trust")]
    if w >= SHORT:
        rows += [box_row(w, [(" " + CWD, "fg bold")]),
                 box_row(w, [(" Saved: ", "muted"), (saved, "")]),
                 box_row(w, [(" This session: ", "muted"), (session, "")]),
                 box_row(w, [])]
    rows += menu(w, opts, sel, tags={current: "current"} if current is not None else None, room_rows=room)
    rows.append(box_bottom(w))
    return slot(rows + [hint_row(w, *CMD_HINT)])


def t1(w, h):
    return screen(w, h, turn(w), below=cmd_box(w, "trusted, from ~/src", "trusted", 1, 1))


def t2(w, h):
    return screen(w, h, turn(w), status=[[("✓ ", "success"),
                                          ("Saved: do not trust ~/src/acme · applies to the next session (/new)",
                                           "dim")]])


def t3(w, h):
    rows = [box_top(w, "Project trust")]
    for r in wrap_words([("✗ ", "error"), (STORE_WARN + ".", "error")], w - 6, w - 8):
        rows.append(box_row(w, [(" ", "")] + r if rows[1:] == [] else [("   ", "")] + r))
    rows.append(box_row(w, [(" Ask does not change it. Fix or remove it; the next session reads it again.", "muted")]))
    rows.append(box_bottom(w))
    return screen(w, h, turn(w), below=slot(rows + [hint_row(w, "esc close", "esc close")]))


def t4(w, h):
    return screen(w, h, turn(w), status=[[("/trust is not available: the leader is an older version.", "error")]])


def t5(w, h):
    rows = [box_top(w, "Project trust"),
            box_row(w, [(" " + CWD, "fg bold")]),
            box_row(w, [(" Saved: ", "muted"), ("none", "")]),
            box_row(w, [(" This session: ", "muted"), ("not trusted (not now)", "")]),
            box_row(w, [])]
    opts = trust_cmd_options(CWD, "~/src")[:3]
    rows += menu(w, opts, 0)
    rows.append(box_row(w, [(" ✗ ", "error"), ("Not saved: no answer from the leader in 5s.", "error")]))
    rows.append(box_bottom(w))
    return screen(w, h, turn(w), below=slot(rows + [hint_row(w, *CMD_HINT)]))


STATES = [
    ("S0", "Before the trust answer: typed-ahead draft",
     "The link is open; the TUI waits for _ask/trust/state. No session exists, so Enter sends nothing and the rail "
     "shows `Starting, please wait…`. The draft stays. Today `opening` clears when the link opens; phase 6 keeps the "
     "view in this starting state until the first session exists.",
     s0, [(80, 24)]),
    ("P1", "Startup prompt: folder with gated files and no saved decision",
     "The explanation block is in the block pane; the trust box takes the prompt pane slot. Not now is first and "
     "highlighted. At 120×40 the layout is the same; the text uses fewer rows. At 40×14 the box shows labels only, "
     "and the pane has room for one row only (the gap row), so the explanation does not show.",
     p1, [(80, 24), (120, 40), (40, 14)]),
    ("P2", "Startup prompt: long path, long gated list, small height",
     "The root path wraps by columns and is never cut. Relative entries wrap at spaces; an ancestor .agents/skills "
     "shows as an absolute path on its own row. More than 4 file rows: 3 rows and `… n more`. In the option "
     "description the path shortens to ~/…/<last element>. At 40×12 the slot has 5 rows: the option list is a "
     "window that keeps the highlighted row, with `… n more`.",
     p2, [(80, 24)]),
    ("P2n", "Startup prompt at 40×12",
     "Same state as P2 at 40×12.",
     p2_narrow, [(40, 12)]),
    ("P3", "Startup prompt with a check warning",
     "ResolveTrust could not check a gated entry. The state has needsDecision and a warning; the warning is its own "
     "block above the explanation, in the warning role, verbatim after `Warning: `.",
     p3, [(80, 24)]),
    ("P4", "Startup prompt: highlight moved",
     "After ↓ ↓ the highlight is on Trust parent folder. Only Enter confirms it.",
     p4, [(80, 24)]),
    ("P5", "Startup prompt: the save failed",
     "Enter on Trust this folder; SetTrust returned an error. The box stays open with an error row under the "
     "options; the highlight stays. No session starts until a choice succeeds or the user takes Not now.",
     p5, [(80, 24)]),
    ("R1", "After Trust this folder",
     "SetTrust(folder, trust), then session/new with no override. The explanation block is replaced by one success "
     "note. The draft is back in the prompt pane. The footer row shows the model of the new session.",
     r1, [(80, 24)]),
    ("R2", "After Trust parent folder",
     "SetTrust(parent, trust): the leader saves ~/src and removes the entry of ~/src/acme. Then session/new with no "
     "override.",
     r2, [(80, 24)]),
    ("R3", "After Not now, Esc or ctrl+c",
     "No SetTrust. session/new with _meta[\"ask.dev/trust\"] = \"deny\". Nothing is saved. At 40×14 the path has "
     "CJK characters (2 columns each) and the note wraps at spaces.",
     r3, [(80, 24), (40, 14)]),
    ("R4", "After Do not trust",
     "SetTrust(folder, distrust), then session/new with no override (source \"saved\").",
     r4, [(80, 24)]),
    ("U1", "Untrusted start with no prompt: --no-approve",
     "The command line had --no-approve: no _ask/trust/state call, the override rides every session/new of this "
     "process. The other no-prompt sources (saved false, saved false on an ancestor, defaultProjectTrust \"never\", "
     "no_ui) use the same one-block layout with the texts of section 8.",
     u1, [(80, 24)]),
    ("W1", "Corrupt or unsafe trust.json",
     "_ask/trust/state has source store_error and a warning. No prompt. session/new with no override; the session "
     "is untrusted. One warning block, the server text verbatim after `Warning: `. No second untrusted note.",
     w1, [(80, 24), (40, 14)]),
    ("D1", "Settings diagnostics after the session starts",
     "_ask/session/state has diagnostics. One block after the trust note: a `Settings problems` row, then the rows "
     "of each file (home shown as ~). Paths wrap by columns; texts wrap at spaces under the key. The 40×14 example "
     "has one diagnostic and a CJK path.",
     d1, [(80, 24), (40, 14)]),
    ("T0", "/trust: waiting for the trust state",
     "/trust asks _ask/trust/state again. Until the answer comes, the rail shows one dim row; the prompt pane and "
     "the draft do not change.",
     t0, [(80, 24)]),
    ("T1", "/trust: saved trust inherited from the parent",
     "/trust opens the box in the prompt pane slot and keeps the draft. The rows come from a fresh "
     "_ask/trust/state and the trust of the current session. The highlight starts on the current row. At 40×14 the "
     "box shows the options only.",
     t1, [(80, 24), (40, 14)]),
    ("T2", "/trust: saved",
     "Enter on Do not trust: SetTrust(folder, distrust). The box closes; the rail shows the result. This session "
     "does not change.",
     t2, [(80, 24)]),
    ("T3", "/trust: corrupt or unsafe trust.json",
     "The box shows the warning and no options. Only Esc acts.",
     t3, [(80, 24)]),
    ("T4", "/trust: the host has no trust methods",
     "An older leader answers unsupported. The rail shows the error text; no box opens.",
     t4, [(80, 24)]),
    ("T5", "/trust: the save failed",
     "SetTrust returned an error. The box stays open with an error row; the highlight and the draft stay. "
     "Forget is not shown because no entry is saved.",
     t5, [(80, 24)]),
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
                    raise SystemExit(f"{sid} {w}: {cw(plain(r))} {plain(r)!r}")
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
