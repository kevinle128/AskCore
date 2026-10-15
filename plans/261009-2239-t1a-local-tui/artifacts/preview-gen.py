#!/usr/bin/env python3
"""Render the design sketches as terminal windows in one HTML page.

Run from the plan directory: python3 artifacts/preview-gen.py
Output: artifacts/preview.html. Open it in a browser.
Rows and roles come from sketch-gen.py; colors follow docs/tui-style-guide.md.
"""
import html
import importlib.util
import pathlib

here = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("sketch_gen", here / "sketch-gen.py")
sg = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sg)

# Dark theme palette from docs/tui-style-guide.md.
P = {
    "bg": "#13141c", "fg": "#d9dbe8", "muted": "#6f7390", "dim": "#4c4f66", "border": "#4c4f66",
    "panel": "#1d1f2b", "code-panel": "#252838", "selected": "#2d3150",
    "accent": "#f472b6", "assistant": "#a78bfa", "tool": "#2dd4bf", "steer": "#a78bfa",
    "success": "#4ade80", "warning": "#facc15", "error": "#f87171", "code": "#fb923c", "thinking": "#8f95b3",
    "prov-1": "#fb923c", "prov-2": "#f472b6", "prov-3": "#2dd4bf", "prov-4": "#7aa2f7",
}

CAPTIONS = {
    "S1": "You run ask. No leader runs yet, so ask starts one and waits.",
    "S2": "The leader could not be reached at startup, so ask runs the agent in this process, as Grok does.",
    "S3": "Ready. Earlier turns are in scrollback; the editor box waits for a prompt.",
    "S4": "You typed / in an empty editor. The command list opens above the editor.",
    "S5": "A prompt over several lines. ctrl+j or shift+enter adds a line.",
    "S5a": "You pressed enter. The text stays in the editor until the leader accepts it.",
    "S5b": "The leader refused the prompt. The draft is kept and the reason shows on the live rail.",
    "S6": "The reply streams. A tool call shows running, and the working line sits on the dashed live rail.",
    "S7": "While the agent works you sent a steer (enter) and a follow-up (alt+enter). They wait on the dashed rail.",
    "S8": "The provider failed with HTTP 429. ask waits and retries; the failure note stays in scrollback.",
    "S9": "The run ended with an auth error. The error and the next action stay in scrollback.",
    "S10": "You pressed esc. The partial reply stays, with a cancel marker.",
    "S11": "ctrl+l opens the model box in place of the editor. Your draft is kept.",
    "S12": "You filtered to anthropic and chose a model whose provider is not signed in. The model does not change.",
    "S12a": "/sessions lists live sessions on this leader. enter starts watching the chosen one.",
    "S13": "The leader is gone. ask reconnects by itself; you can type, but nothing is sent.",
    "S13a": "Reconnected to the same leader. The session continues.",
    "S13b": "The leader restarted, so the old session is gone. The old transcript stays.",
    "S14": "Another client drives this session. You watch; /take gives you control when the run ends.",
    "S15": "First ctrl+c with an empty editor. A second ctrl+c quits; the run continues in the leader.",
    "S16": "After quit. The terminal is back to the shell.",
    "S17": "A 40-column terminal with Japanese text. Wide characters are not split; the footer is short.",
    "S18": "A reasoning model. Its thinking shows in italics, marked ∴, before the reply (default).",
    "S19": "After ctrl+t every thinking block shrinks to one line. ctrl+t again shows it.",
}


def style_for(roles):
    css, classes = [], []
    rs = roles.split()
    bgs = {"panel", "code-panel", "selected"}
    fg = None
    for r in rs:
        if r in bgs:
            css.append(f"background:{P[r]}")
        elif r == "bold":
            css.append("font-weight:700")
        elif r == "italic":
            css.append("font-style:italic")
        elif r in ("shimmer", "cursor", "logo"):
            classes.append(r)
        elif r == "user":
            fg = P["fg"]
            css.append("font-weight:700")
        elif r in P:
            fg = P[r]
    if "thinking" in rs:
        css.append("font-style:italic")
    if fg:
        css.append(f"color:{fg}")
    return ";".join(css), " ".join(classes)


def row_html(row):
    spans = []
    for text, roles in row:
        css, cls = style_for(roles)
        spans.append(f'<span class="{cls}" style="{css}">{html.escape(text)}</span>')
    return '<div class="row">' + "".join(spans) + "</div>"


def terminal(rows, w):
    out = []
    for r in rows:
        if r == sg.MARK:
            out.append('<div class="boundary"><span>live region</span></div>')
        else:
            if sg.cw(sg.plain(r)) != w:
                raise SystemExit(f"width {sg.cw(sg.plain(r))} != {w}: {sg.plain(r)!r}")
            out.append(row_html(r))
    return f'<div class="term" style="width:{w}ch">' + "\n".join(out) + "</div>"


def main():
    nav, body = [], []
    for sid, title, fn, sizes in sg.STATES:
        nav.append(f'<a href="#{sid}">{sid} {html.escape(title)}</a>')
        body.append(f'<section id="{sid}"><h2>{sid}. {html.escape(title)}</h2>')
        body.append(f'<p class="cap">{html.escape(CAPTIONS.get(sid, ""))}</p>')
        for (w, h) in sizes:
            body.append(f'<div class="size">{w}×{h}</div>')
            body.append(terminal(fn(w, h), w))
        body.append("</section>")
    page = f"""<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>T1a TUI design preview</title>
<style>
body {{ margin:0; font-family: system-ui, sans-serif; background:#0b0c11; color:#cfd2e3; display:flex; }}
nav {{ position:sticky; top:0; height:100vh; overflow:auto; width:270px; padding:16px; box-sizing:border-box;
       background:#11121a; border-right:1px solid #22243a; font-size:13px; }}
nav a {{ display:block; color:#aeb2c9; text-decoration:none; padding:3px 0; }}
nav a:hover {{ color:{P['accent']}; }}
main {{ padding:24px 32px; flex:1; }}
h1 {{ font-size:20px; margin:0 0 4px; }}
.note {{ color:#8b8fa8; font-size:14px; max-width:900px; }}
section {{ margin:36px 0; }}
h2 {{ font-size:16px; margin:0 0 4px; }}
.cap {{ margin:0 0 10px; color:#aeb2c9; font-size:14px; max-width:900px; }}
.size {{ font-size:12px; color:#6f7390; margin:10px 0 4px; }}
.term {{ background:{P['bg']}; color:{P['fg']}; font:13.5px/1.42 "JetBrains Mono","SF Mono",Menlo,monospace;
         padding:14px 16px; border-radius:12px; box-shadow:0 10px 40px rgba(0,0,0,.5); border:1px solid #23253a; box-sizing:content-box; }}
.row {{ white-space:pre; min-height:1.42em; }}
.cursor {{ color:{P['accent']}; animation: blink 1s steps(1) infinite; }}
@keyframes blink {{ 50% {{ opacity:0; }} }}
.logo {{ background: linear-gradient(90deg,{P['assistant']},{P['accent']},{P['code']}); -webkit-background-clip:text; background-clip:text;
         color:transparent !important; font-weight:700; }}
.shimmer {{ background: linear-gradient(90deg,currentColor,#ffffff,currentColor); background-size:200% 100%;
            -webkit-background-clip:text; background-clip:text; -webkit-text-fill-color:transparent; animation: sh 2s linear infinite; }}
@keyframes sh {{ from {{ background-position:200% 0; }} to {{ background-position:-200% 0; }} }}
.boundary {{ position:relative; border-top:1px dashed #3a3d55; margin:3px -16px; height:0; }}
.boundary span {{ position:absolute; right:10px; top:-9px; font:10px system-ui; color:#7d81a0; background:{P['bg']}; padding:0 4px; }}
</style></head><body>
<nav><b>T1a TUI states</b><br><br>{"".join(nav)}</nav>
<main>
<h1>T1a TUI design preview · direction B4</h1>
<p class="note">Each box is one terminal at the stated size. Rows above the dashed line are native scrollback;
rows below it are the live region that ask redraws. The dashed line is not on screen.
Colors follow docs/tui-style-guide.md. Source: artifacts/sketch-gen.py, the same rows as design.md.</p>
{"".join(body)}
</main></body></html>
"""
    (here / "preview.html").write_text(page)
    print(here / "preview.html")


if __name__ == "__main__":
    main()
