#!/usr/bin/env python3
"""Generate exact-width sketches for the background job notices TUI design.

Each sketch line is a list of segments (text, roles). design.md uses the plain text;
preview.html renders the roles as colors. Both come from this file, so they cannot disagree.
Patterns follow docs/tui-style-guide.md; helpers follow the T1c provider setup generator.

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


def started(w):
    """History: the prompt, the background bash call and the reply."""
    return (header(w) + user(w, Q1, "14:02")
            + [tool_line(w, "bash", BUILD, "✓ 0.1s", "success")]
            + [L(w)] + answer(w, "Started the build as background job bash-1. I will tell you when it finishes.")
            + meta(w))


# ---------------------------------------------------------------- states

def s_running(w, h):
    live = [rail_jobs(w, [("bash-1", BUILD)])] + editor(w) + footer(w)
    return frame(w, h, started(w), live)


def s_running_two(w, h):
    scroll = (started(w) + user(w, "and run the tests in the background too", "14:03")
              + [tool_line(w, "bash", TESTS, "✓ 0.1s", "success")] + [L(w)]
              + answer(w, "Started the tests as bash-3.") + meta(w))
    live = [rail_jobs(w, [("bash-1", BUILD), ("bash-3", TESTS)])] + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def s_working_with_job(w, h):
    scroll = started(w) + user(w, Q2, "14:03") + answer(w, "The build step runs `vite build`, which")
    live = [working(w, 3, jobs=1)] + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def s_finished_idle(w, h):
    scroll = (started(w) + [job_line(w, "bash-1", BUILD, "✓ completed · exit 0", "success")] + [L(w)]
              + answer(w, "The build finished with exit code 0. The bundle is in dist/."))
    live = [working(w, 1)] + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def s_finished_busy(w, h):
    """The job ends while the agent answers another prompt: its notice waits in the queue, hidden."""
    scroll = started(w) + user(w, Q2, "14:03") + answer(w, "The build step runs `vite build`, which bundles")
    live = [working(w, 6)] + queue_rows(w, [("next", Q3)]) + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def s_finished_busy_only_notice(w, h):
    scroll = started(w) + user(w, Q2, "14:03") + answer(w, "The build step runs `vite build`, which bundles")
    live = [working(w, 6)] + editor(w) + footer(w)
    return frame(w, h, scroll, live)


def s_failed_killed(w, h):
    scroll = (started(w)
              + [job_line(w, "bash-1", BUILD, "✗ failed · exit 2", "error")] + [L(w)]
              + answer(w, "The build failed: `src/app.ts` has a type error on line 14.") + meta(w)
              + [job_line(w, "bash-3", TESTS, "✗ killed · timeout", "error")] + [L(w)]
              + answer(w, "The test job hit its timeout and was stopped.") + meta(w))
    live = editor(w) + footer(w)
    return frame(w, h, scroll, live)


def s_monitor(w, h):
    scroll = (header(w) + user(w, "watch the server log and tell me about errors", "14:10")
              + [tool_line(w, "monitor", LOGS, "✓ 0.1s", "success")] + [L(w)]
              + answer(w, "Watching server.log as job bash-2.") + meta(w)
              + [job_line(w, "bash-2", LOGS, "3 events", "muted")] + [L(w)]
              + answer(w, "Three new lines; one is an error: `connection refused` on port 5432."))
    live = [working(w, 2)] + editor(w) + footer(w)
    return frame(w, h, scroll, live)


STATES = [
    ("S1", "One job runs, the session is idle",
     "The background bash call shows as a normal tool line. While the job runs and the agent is idle, "
     "the status rail shows the job count and the job.", s_running, [(80, 24), (40, 14)]),
    ("S2", "Two jobs run",
     "The rail counts the jobs and names their ids; the labels are in the tool lines above.", s_running_two,
     [(80, 24)]),
    ("S3", "The agent works while a job runs",
     "The run state keeps the rail; the job count is added to its tail.", s_working_with_job, [(80, 24), (40, 14)]),
    ("S4", "A job finishes while the session is idle",
     "The notice is not drawn as a user prompt. A job line takes its place in scrollback, and the agent's answer "
     "to the notice follows it.", s_finished_idle, [(80, 24), (40, 14)]),
    ("S5", "A job finishes while the agent works, with a typed follow-up",
     "The notice waits in the agent queue but has no queue row. The user's own queued input keeps its row.",
     s_finished_busy, [(80, 24)]),
    ("S6", "A job finishes while the agent works, nothing else queued",
     "Only the notice is queued, so the queue block (rows and hint) is not drawn.", s_finished_busy_only_notice,
     [(80, 24)]),
    ("S7", "A job fails, a job is killed",
     "The job line uses the error role and the reason from the notice.", s_failed_killed, [(80, 24)]),
    ("S8", "Monitor events",
     "A monitor notice is one job line with the event count; the event text goes to the model only.", s_monitor,
     [(80, 24)]),
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
