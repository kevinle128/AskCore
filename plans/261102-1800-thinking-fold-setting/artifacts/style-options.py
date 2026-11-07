#!/usr/bin/env python3
"""Render four style options for the block pane (thinking, reply markdown, selection) as one HTML page.

Run from the plan directory: python3 artifacts/style-options.py
Output: artifacts/style-options.html. Every option draws the same turn at 100x44; only the style changes.
Fixed in all options (user decisions): thin selection box like Grok, existing palette roles, 24h time on the
first reply row, no table copy row.
"""
import html
import pathlib
import re
import unicodedata

here = pathlib.Path(__file__).resolve().parent
W, H = 100, 44

P = {
    "bg": "#13141c", "fg": "#d9dbe8", "muted": "#6f7390", "dim": "#4c4f66", "border": "#4c4f66",
    "panel": "#1d1f2b", "code-panel": "#252838", "accent": "#f472b6", "assistant": "#a78bfa",
    "tool": "#2dd4bf", "blue": "#7aa2f7", "code": "#fb923c", "thinking": "#8f95b3", "success": "#4ade80",
    "box": "#3a3d52",
}


def cw(s):
    return sum(0 if unicodedata.combining(c) else (2 if unicodedata.east_asian_width(c) in "WF" else 1) for c in s)


def pad(segs, w=W, fill=""):
    used = sum(cw(t) for t, _ in segs)
    if used > w:
        raise SystemExit(f"row too wide {used}: {''.join(t for t, _ in segs)!r}")
    return segs + [(" " * (w - used), fill)]


def inline(text, opt, base=""):
    """Split `code` and **bold** spans into segments with the option's inline roles."""
    out = []
    for part in re.split(r"(`[^`]+`|\*\*[^*]+\*\*)", text):
        if not part:
            continue
        if part.startswith("`"):
            out.append((opt["code_wrap"](part[1:-1]), opt["code_role"]))
        elif part.startswith("**"):
            out.append((part[2:-2], (base + " bold").strip()))
        else:
            out.append((part, base))
    return out


def wrap_segs(segs, first, rest):
    """Wrap a segment list by words: the first row has `first` columns, the others `rest`."""
    rows, cur, used, limit = [], [], 0, first
    for text, role in segs:
        for i, word in enumerate(re.split(r"( )", text)):
            if word == "":
                continue
            if word == " " and used == 0:
                continue
            if used + cw(word) > limit and word != " ":
                while cur and cur[-1][0] == " ":
                    cur.pop()
                rows.append(cur)
                cur, used, limit = [], 0, rest
            if word == " " and used == 0:
                continue
            cur.append((word, role))
            used += cw(word)
    rows.append(cur)
    return rows


# ---------------------------------------------------------------- the example turn

PROMPT = "đọc file internal/agent/tool_coordinator.go và cho tôi biết nội dung"
THINK = [
    "The user wants a summary of tool_coordinator.go.",
    "I will read the file, then group it: the types, the batch loop, and the safety parts.",
]
INTRO = "File `tool_coordinator.go` điều phối **một batch tool call**: chuẩn bị, chạy song song có giới hạn, commit theo thứ tự."
TABLE = [("Type", "Việc làm"), ("toolSlot", "Trạng thái một call: prepared, ready, outcome"),
         ("toolDone", "Worker báo xong: index, outcome, invoked"), ("updater", "Progress không chặn: chỉ giữ giá trị mới nhất")]
STEPS = ["Tìm entry assistant gần nhất trong session log.",
         "`MaxParallelTools` (mặc định 10) giới hạn số worker.",
         "Commit theo thứ tự: `progress` → `afterTool` → `emitToolEnd`."]
SAFE = ["`toolGuarded` bắt panic và đổi thành error.", "`cloneToolResult` clone sâu trước khi emit."]
CODE = ["body, e = p.tool.Execute(bodyCtx, tc, bytes.Clone(p.args))"]
META = "claude-sonnet-5-5 · 14s · ↑10.2k ↓1.8k"


# ---------------------------------------------------------------- shared rows

def header():
    return pad([(" ◢◤ ask", "logo")])


def rule():
    return [("─" * W, "border")]


def footer_rows():
    return [rule(), pad([(" ❯ ", "accent"), ("Ask anything  ·  / commands  ·  ctrl+l model", "muted")]), rule(),
            pad([(" ⌂ ~/src/AskCore", "muted")] + [(" " * 44, ""), ("anthropic/claude-sonnet-5-5", "assistant"),
                                                  ("  ", ""), ("▰▱▱▱▱▱▱▱", "success"), (" 9%", "muted")]),
            pad([(" j/k select · e fold · Enter open · Tab prompt", "muted")])]


def prompt_band():
    t = "17:30"
    segs = [("  ❯ ", "accent panel bold"), (PROMPT, "fg panel bold")]
    used = sum(cw(x) for x, _ in segs) + cw(t) + 2
    return [segs + [(" " * (W - used), "panel"), (t, "dim panel"), ("  ", "panel")]]


def thinking_rows(opt):
    rows = [pad([("  ◆ ", "thinking"), ("Thought", "fg bold"), (" for 2.1s", "muted")])]
    for line in THINK:
        rows.append(pad([(" " * opt["think_indent"], ""), (line, opt["think_role"])]))
    return rows


def tool_row():
    return pad([("  ◆ ", "tool"), ("Read", "fg bold"), (" internal/agent/tool_coordinator.go", "muted")])


def heading(level, text, opt, inner):
    return opt["heading"](level, text, inner)


def table_rows(opt, inner):
    return opt["table"](TABLE, inner)


def reply_body(opt, inner):
    """Reply rows as segment lists, each at most `inner` columns wide."""
    rows = []
    rows += wrap_segs(inline(INTRO, opt), inner - 8, inner - 1)
    rows.append([])
    rows += heading(2, "Kiểu dữ liệu", opt, inner)
    rows += table_rows(opt, inner)
    rows.append([])
    rows += heading(2, "Luồng runToolBatch", opt, inner)
    for i, s in enumerate(STEPS, 1):
        rows.append([(f"{i}. ", opt["marker_role"])] + inline(s, opt))
    rows.append([])
    rows += heading(3, "An toàn", opt, inner)
    for s in SAFE:
        rows.append([(opt["bullet"] + " ", opt["marker_role"])] + inline(s, opt))
    rows.append([])
    for c in CODE:
        rows.append([("  " + c, "code code-panel")] + [(" " * (inner - cw(c) - 2), "code-panel")])
    return rows


def selected_reply(opt):
    """The reply inside the thin selection box: corners on the pad rows, │ on both sides."""
    inner = W - 4
    body = reply_body(opt, inner)
    first = body[0]
    out = [pad([("┌", "box")], W - 1) + [("┐", "box")]]
    for i, segs in enumerate(body):
        line = [("│ ", "box")] + segs
        used = sum(cw(t) for t, _ in line)
        if i == 0:
            # 24h time at the right of the first reply row.
            t = "17:30"
            used_txt = used
            if used_txt + cw(t) + 3 > W:
                raise SystemExit("time does not fit")
            line += [(" " * (W - 2 - used_txt - cw(t)), ""), (t, "dim"), (" ", ""), ("│", "box")]
        else:
            line += [(" " * (W - 1 - used), ""), ("│", "box")]
        out.append(line)
    out.append(pad([("│ ", "box"), (META, "dim")], W - 1) + [("│", "box")])
    out.append(pad([("└", "box")], W - 1) + [("┘", "box")])
    return out


def screen(opt):
    rows = [header(), rule()] + prompt_band() + [pad([])] + thinking_rows(opt) + [tool_row(), pad([])]
    rows += selected_reply(opt)
    foot = footer_rows()
    room = H - len(rows) - len(foot)
    if room < 0:
        raise SystemExit(f"{opt['id']}: {len(rows) + len(foot)} rows > {H}")
    rows += [pad([])] * room + foot
    for r in rows:
        if cw("".join(t for t, _ in r)) != W:
            raise SystemExit(f"{opt['id']} width: {''.join(t for t, _ in r)!r}")
    return rows


# ---------------------------------------------------------------- tables

def box_table(rows, inner, header_role="fg bold", border="dim"):
    widths = [max(cw(r[i]) for r in rows) for i in range(2)]

    def line(l, m, r):
        return [(l + "─" * (widths[0] + 2) + m + "─" * (widths[1] + 2) + r, border)]

    def cells(r, role):
        return [("│ ", border), (r[0] + " " * (widths[0] - cw(r[0])), role), (" │ ", border),
                (r[1] + " " * (widths[1] - cw(r[1])), role if role != "code_cell" else "fg"), (" │", border)]

    out = [line("┌", "┬", "┐"), cells(rows[0], header_role), line("├", "┼", "┤")]
    out += [cells(r, "fg") for r in rows[1:]]
    out.append(line("└", "┴", "┘"))
    return out


def rule_table(rows, inner):
    """Light table: bold header, one dim rule under it, no outer box."""
    widths = [max(cw(r[i]) for r in rows) for i in range(2)]
    out = [[(rows[0][0] + " " * (widths[0] - cw(rows[0][0]) + 3), "fg bold"), (rows[0][1], "fg bold")],
           [("─" * (widths[0] + 3 + widths[1]), "dim")]]
    for r in rows[1:]:
        out.append([(r[0] + " " * (widths[0] - cw(r[0]) + 3), "tool"), (r[1], "fg")])
    return out


# ---------------------------------------------------------------- the four options

HEAD_ROLE = {1: "tool bold", 2: "blue bold", 3: "assistant bold"}

OPTIONS = [
    {
        "id": "A", "name": "Grok: quiet and colored",
        "note": "Closest to Grok CLI. Thinking text is plain and muted, under the verb, with no rail. Headings lose "
                "the #, take a color by level, and are bold. Inline code is blue and bold with no backticks. Bullets "
                "are a muted •. Tables have a dim box. Code blocks have the code panel and no language row.",
        "think_indent": 4, "think_role": "muted",
        "heading": lambda lvl, t, inner: [[(t, HEAD_ROLE[lvl])]],
        "code_role": "blue bold", "code_wrap": lambda s: s,
        "bullet": "•", "marker_role": "muted",
        "table": lambda rows, inner: box_table(rows, inner),
    },
    {
        "id": "B", "name": "Sections with underline",
        "note": "Like A, and each h1/h2 gets a dim line under it, so long answers read as sections. Thinking keeps "
                "Ask's italic thinking color. Inline code keeps Ask's orange code color.",
        "think_indent": 4, "think_role": "thinking italic",
        "heading": lambda lvl, t, inner: ([[(t, HEAD_ROLE[lvl])], [("─" * cw(t), "dim")]] if lvl <= 2
                                          else [[(t, HEAD_ROLE[lvl])]]),
        "code_role": "code", "code_wrap": lambda s: s,
        "bullet": "•", "marker_role": "muted",
        "table": lambda rows, inner: box_table(rows, inner),
    },
    {
        "id": "C", "name": "Editorial: small caps titles",
        "note": "Headings are UPPERCASE in a muted bold, with a dim line to the right edge, like a document. "
                "Color stays for code only. Thinking is muted italic. Tables are light: a bold header and one rule.",
        "think_indent": 4, "think_role": "muted italic",
        "heading": lambda lvl, t, inner: [[(t.upper() + " ", "muted bold"),
                                           ("─" * (inner - cw(t) - 1 - 2), "dim")]],
        "code_role": "tool", "code_wrap": lambda s: s,
        "bullet": "·", "marker_role": "muted",
        "table": lambda rows, inner: rule_table(rows, inner),
    },
    {
        "id": "D", "name": "Monochrome with code chips",
        "note": "Almost no color in the text: headings are bold body text, h3 is muted. Inline code is a chip with the "
                "code panel behind it. Bullets are a dash. Thinking is dim, the least visible of all four.",
        "think_indent": 4, "think_role": "dim",
        "heading": lambda lvl, t, inner: [[(t, "fg bold" if lvl <= 2 else "muted bold")]],
        "code_role": "fg code-panel", "code_wrap": lambda s: f" {s} ",
        "bullet": "–", "marker_role": "dim",
        "table": lambda rows, inner: box_table(rows, inner, border="border"),
    },
]


# ---------------------------------------------------------------- html

def style_for(roles):
    css, cls, fg = [], [], None
    for r in roles.split():
        if r in ("panel", "code-panel"):
            css.append(f"background:{P[r]}")
        elif r == "bold":
            css.append("font-weight:700")
        elif r == "italic":
            css.append("font-style:italic")
        elif r == "logo":
            cls.append("logo")
        elif r in P:
            fg = P[r]
    if fg:
        css.append(f"color:{fg}")
    return ";".join(css), " ".join(cls)


def term(rows):
    out = []
    for r in rows:
        spans = "".join(f'<span class="{c}" style="{s}">{html.escape(t)}</span>'
                        for t, roles in r for s, c in [style_for(roles)])
        out.append(f'<div class="row">{spans}</div>')
    return f'<div class="term" style="width:{W}ch">' + "".join(out) + "</div>"


def main():
    sections = []
    for o in OPTIONS:
        sections.append(f'<section id="{o["id"]}"><h2>Option {o["id"]}: {html.escape(o["name"])}</h2>'
                        f'<p class="cap">{html.escape(o["note"])}</p>{term(screen(o))}</section>')
    nav = "".join(f'<a href="#{o["id"]}">Option {o["id"]}: {html.escape(o["name"])}</a>' for o in OPTIONS)
    page = f"""<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Block pane styles</title>
<style>
body {{ margin:0; font-family: system-ui, sans-serif; background:#0b0c11; color:#cfd2e3; display:flex; }}
nav {{ position:sticky; top:0; height:100vh; width:240px; padding:16px; box-sizing:border-box; background:#11121a;
       border-right:1px solid #22243a; font-size:13px; flex:none; }}
nav a {{ display:block; color:#aeb2c9; text-decoration:none; padding:4px 0; }}
main {{ padding:24px 32px; flex:1; min-width:0; overflow-x:auto; }}
h1 {{ font-size:20px; margin:0 0 4px; }} h2 {{ font-size:16px; margin:0 0 4px; }}
.note, .cap {{ color:#aeb2c9; font-size:14px; max-width:960px; }}
section {{ margin:32px 0; }}
.term {{ background:{P['bg']}; color:{P['fg']}; font:13px/1.4 "JetBrains Mono","SF Mono",Menlo,monospace; padding:14px 16px;
         border-radius:12px; border:1px solid #23253a; box-shadow:0 10px 40px rgba(0,0,0,.5); box-sizing:content-box; }}
.row {{ white-space:pre; height:calc(13px * 1.4); }}
.logo {{ background:linear-gradient(90deg,{P['assistant']},{P['accent']},{P['code']}); -webkit-background-clip:text;
         background-clip:text; color:transparent !important; font-weight:700; }}
</style></head><body><nav><b>Style options</b><br><br>{nav}</nav><main>
<h1>Block pane: four style options (100×44)</h1>
<p class="note">The same turn in each option. Fixed in all four: the thin selection box (the reply is selected),
colors from the existing palette, the 24h time on the first reply row, no table copy row, and no rail on thinking.</p>
{"".join(sections)}</main></body></html>"""
    (here / "style-options.html").write_text(page)
    print(here / "style-options.html")


if __name__ == "__main__":
    main()
