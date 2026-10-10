#!/usr/bin/env python3
"""Generate exact-width sketches for the provider setup TUI design (style guide direction B4).

Each sketch line is a list of segments (text, roles). design.md uses the plain text;
preview.html renders the roles as colors. Both come from this file, so they cannot disagree.
Every secret and account in this file is a fake placeholder.

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


def chunks(s, n):
    """Hard wrap by column count. Used for URLs, which have no spaces."""
    return [s[i:i + n] for i in range(0, len(s), n)]


def trunc(s, n):
    return s if cw(s) <= n else s[:n - 1] + "…"


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

Q1 = "why does TestLeaderStale fail on CI?"
A1 = ("The test waits for the stale socket check, but CI runs it with a 1 s deadline. "
      "The leader removes the stale socket after the lock probe, and the probe can take 1.2 s.")
DEF = ("alibaba-token-plan", "deepseek-v4.1-flash")  # provider, model id of the session in most sketches
META_REST = "4.2s · ↑12.4k ↓1.8k · $0.004"
DRAFT_TEXT = "also check the lock timeout"
URL = ("https://auth.example.test/oauth/authorize?response_type=code&client_id=ask-cli"
       "&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&scope=openid+profile"
       "&state=Zk3pQ9xT&code_challenge=Vt8f0aLm2sQe&code_challenge_method=S256")
DEVICE_URL = "https://auth.example.test/device"
ACCOUNT = "dev@example.com"

PR = {"alibaba-token-plan": "prov-1", "anthropic": "prov-2", "openai": "prov-3", "xai": "prov-4"}
METHODS_HINT = {"alibaba-token-plan": "key", "anthropic": "key · account", "openai": "key · account",
                "xai": "key · account"}


def model_rows(w, pair, was=None, api=None):
    """The model line: one row, or more rows when the value is wider than the terminal.

    From 60 columns the line is `  model  <provider>/<model>`. Under 60 columns the label is dropped so that the
    longest catalog pair still fits in 40 columns; a pair that is still too wide wraps after the slash.
    A change line adds ` · was <old pair>` when it fits. A model on two wire formats adds ` (<api>)`; when the row
    is too wide, the suffix moves to its own row, aligned with the value."""
    label = "  model  " if w >= 60 else "  "
    first = [(label, "muted"), (pair, "")]
    rows_extra = []
    if api:
        if cw(label + pair + " (" + api + ")") <= w:
            first.append((" (" + api + ")", ""))
        else:
            rows_extra = [L(w, (" " * cw(label) + "(" + api + ")", ""))]
    if w >= 60:
        if was and not api and cw(label + pair + " · was " + was) <= w:
            first.append((" · was " + was, "dim"))
        return [L(w, *first)] + rows_extra
    if cw(label + pair) <= w:
        return [L(w, *first)] + rows_extra
    prov, _, mod = pair.partition("/")
    return [L(w, (label + prov + "/", "")), L(w, (label + mod, ""))] + rows_extra


def header(w, place="leader 7c1e", model=None):
    """Startup header, then the model line when the session has a model, then one empty row."""
    if w < 60:
        top = [L(w, ("◢◤ ", "logo"), ("ask", "logo"), ("  v0.1.0 · " + place.split()[-1], "muted"))]
    else:
        top = [L(w, ("◢◤ ", "logo"), ("ask", "logo"), (f"   v0.1.0 · {place} · ~/src/askcore", "muted"))]
    rows = top + (model_rows(w, "/".join(model)) if model else [])
    return rows + [L(w)]


def user(w, q, time):
    rows = wrap(q, w - 12, "", "")
    out = [L(w, ("  ", "panel"), ("❯ ", "accent bold panel"), (rows[0], "user panel"),
             right=[(time + "  ", "dim panel")], fill="panel")]
    for r in rows[1:]:
        out.append(L(w, ("    ", "panel"), (r, "user panel"), fill="panel"))
    return out + [L(w)]


def answer(w, text):
    rows = wrap(text, w - 4)
    return [L(w, ("  ◆ " if k == 0 else "    ", "assistant bold"), (r, "")) for k, r in enumerate(rows)]


def code(w):
    return [L(w), L(w, ("    go", "dim code-panel"), fill="code-panel"),
            L(w, ("    deadline := 5 * time.Second", "code code-panel"), fill="code-panel")]


def meta_row(w, model):
    """Meta line after a reply, as in the earlier design: the bare model id, then the usage.
    A line that is too wide is cut at the end with an ellipsis."""
    return [L(w, ("    " + trunc(model + " · " + META_REST, w - 4), "dim")), L(w)]


def history(w, place="leader 7c1e", model=DEF):
    return header(w, place, model) + user(w, Q1, "14:02") + answer(w, A1) + code(w) + meta_row(w, model[1])


def rail(w, *segs):
    """A notice on the live rail, in dim, as in the style guide."""
    return L(w, ("┊ ", "dim"), *segs)


def rail_work(w, text, tail="", role="accent"):
    return L(w, ("┊ ", role), ("◌ ", role + " bold"), (text, role + " bold shimmer"), (tail, "muted"))


def rail_err(w, head, tail):
    return L(w, ("┊ ", "error"), ("✗ ", "error bold"), (head, "error bold"), (tail, "muted"))


def note_ok(w, text, tail=None):
    out = [L(w, ("  ✓ ", "success bold"), (text, "success"))]
    if tail:
        out.append(L(w, ("    " + tail, "muted")))
    return out + [L(w)]


def note_err(w, text, tail=None):
    out = [L(w, ("  ✗ ", "error bold"), (text, "error"))]
    if tail:
        out.append(L(w, ("    " + tail, "muted")))
    return out + [L(w)]


def note_cancel(w, text):
    return [L(w, ("  ⊘ ", "muted"), (text, "muted")), L(w)]


def editor(w, lines=None, placeholder=None):
    """Rounded editor box. lines: list of row segment lists, or None for the empty prompt."""
    inner = w - 2
    if placeholder is None:
        placeholder = "Ask anything  ·  / commands  ·  ctrl+l model" if w >= 60 else "Ask anything"
    if lines is None:
        lines = [[("❯ ", "accent bold"), ("▌", "cursor"), (placeholder, "dim italic")]]
    out = [L(w, ("╭" + "─" * inner + "╮", "border"))]
    for segs in lines:
        used = 1 + sum(cw(t) for t, _ in segs)
        out.append([("│ ", "border")] + segs + [(" " * (inner - used), ""), ("│", "border")])
    out.append(L(w, ("╰" + "─" * inner + "╯", "border")))
    return out


DRAFT = [[("❯ ", "accent bold"), (DRAFT_TEXT, ""), ("▌", "cursor")]]

TRACE = "TEA_TRACE is on: typed and pasted text, keys included, is written to /tmp/ask-trace.log."


def trace_box_rows(w):
    """The TEA_TRACE warning inside a box with a secret input."""
    return [[t("  " + r, "warning")] for r in wrap(TRACE, w - 6)] + [[]]


def trace_scroll_rows(w):
    """The TEA_TRACE warning once at start, in scrollback."""
    return [L(w, ("  " + r, "warning")) for r in wrap(TRACE, w - 4)]


def ctx_bar(pct, n=8):
    full = round(pct / 100 * n)
    role = "error" if pct >= 90 else "warning" if pct >= 70 else "accent"
    return ("▰" * full + "▱" * (n - full), role)


def footer(w, dot=("●", "success"), place="leader 7c1e", model="deepseek-v4.1-flash", think="",
           pct=18, cost="$0.04", place_role="muted", nomodel=False):
    if nomodel:
        if w < 60:
            return [L(w, (f"  {dot[0]} ", dot[1]), (place.split()[-1], place_role), right=[("no model ", "muted")])]
        return [L(w, (f"  {dot[0]} ", dot[1]), (place, place_role), ("   ⌂ ", "muted"), ("~/src/askcore", "muted"),
                  right=[("no model ", "muted")])]
    if w < 60:
        return [L(w, (f"  {dot[0]} ", dot[1]), (place.split()[-1], place_role),
                  right=[(model, "assistant bold"), (f" {pct}% ", "muted")])]
    right = [(model, "assistant bold")]
    if think:
        right.append((f" ◐ {think}", "assistant"))
    right += [("  ", ""), ctx_bar(pct), (f" {pct}%", "muted"), (f"  {cost} ", "muted")]
    return [L(w, (f"  {dot[0]} ", dot[1]), (place, place_role), ("   ⌂ ", "muted"), ("~/src/askcore", "muted"),
              right=right)]


def frame(w, h, scroll, live, top_anchor=False):
    marker = [MARK] if live else []
    body = len(scroll) + len(live)
    if body >= h:
        keep = h - len(live)
        if keep < 0:
            raise SystemExit("live region taller than terminal")
        return scroll[len(scroll) - keep:] + marker + live
    filler = [L(w)] * (h - body)
    if top_anchor:
        return scroll + marker + live + filler
    return filler + scroll + marker + live


# ---------------------------------------------------------------- boxes

def _box_title(w, title):
    head = "╭─ " + title + " "
    return L(w, ("╭─ ", "border"), (title, "accent bold"), (" " + "─" * (w - 2 - cw(head) + 1) + "╮", "border"))


def _box_row(w, segs, right=None, fill=""):
    inner = w - 2
    rgt = right or []
    used = sum(cw(t) for t, _ in segs) + sum(cw(t) for t, _ in rgt)
    if used > inner:
        raise SystemExit(f"box row too wide ({used}>{inner}): {''.join(t for t, _ in segs + rgt)!r}")
    return [("│", "border")] + segs + [(" " * (inner - used), fill)] + rgt + [("│", "border")]


def _bottom(w):
    return L(w, ("╰" + "─" * (w - 2) + "╯", "border"))


def hint_row(w, parts):
    """Hint under a box. parts: (text, rank). Rank 0 is never dropped; the highest rank drops first."""
    sep = "  ·  " if w >= 60 else " · "
    keep = list(parts)
    while True:
        text = sep.join(t for t, _ in keep)
        if cw("   " + text) <= w or not any(r for _, r in keep):
            break
        top = max(r for _, r in keep)
        keep = [(t, r) for t, r in keep if r != top]
    return L(w, ("   " + text, "dim italic"))


def _filter_row(w, typed=""):
    segs = [("  ⌕ ", "muted"), (typed, ""), ("▌", "cursor")]
    if not typed:
        segs.append(("type to filter", "dim italic"))
    return _box_row(w, segs)


def dlg(w, title, rows, hint):
    """A dialog box in the editor slot. rows: lists of segments for the inside of the box."""
    return [_box_title(w, title)] + [_box_row(w, r) for r in rows] + [_bottom(w), hint_row(w, hint)]


def blank(w):
    return []


def t(text, role=""):
    return (text, role)


def url_rows(w, url=URL, cap=None):
    """The URL as hard-wrapped rows. The full URL is also copied by ctrl+y and is a link (OSC 8)."""
    parts = chunks(url, w - 5)
    rows = [[t("  "), t(p)] for p in parts]
    if cap is not None and len(parts) > cap:
        rows = rows[:cap] + [[t("  … ctrl+y copies the full URL", "dim italic")]]
    return rows


# ---------------------------------------------------------------- providers list
# One row for each login option, in the order of provider id, then account before API key.
# Names are checked against cmd/tui/auth_args.go (methods) and internal/providers/catalog.go (providers).

ROWS = [("alibaba-token-plan", "Alibaba Token Plan API key"),
        ("anthropic", "Claude Pro/Max sign-in"),
        ("anthropic", "Anthropic API key"),
        ("openai", "ChatGPT Plus/Pro sign-in"),
        ("openai", "OpenAI API key"),
        ("xai", "xAI Grok sign-in"),
        ("xai", "xAI API key")]
# Status of the credential that each row names: row index -> (suffix, role).
BASE = {0: ("api key", "success"), 2: ("env ANTHROPIC_API_KEY", "success"), 3: ("signed in", "success")}


def prov_rows(w, st, sel, first=0, count=None):
    out = []
    inner = w - 2
    end = len(ROWS) if count is None else min(len(ROWS), first + count)
    for k in range(first, end):
        prov, name = ROWS[k]
        suffix, srole = st.get(k, ("", ""))
        s = k == sel
        bg = " selected" if s else ""
        avail = inner - 5 - (cw(suffix) + 2 if suffix else 0)
        left = [(" ❯ " if s else "   ", "accent bold" + bg), ("● ", PR[prov] + bg),
                (trunc(name, avail), ("bold" if s else "") + bg)]
        right = [(suffix + " ", srole + bg)] if suffix else []
        out.append(_box_row(w, left, right=right, fill=bg.strip()))
    return out


def _search_row(w, typed=""):
    segs = [("  ⌕ ", "muted"), (typed, ""), ("▌", "cursor")]
    if not typed:
        segs.append(("Type to search", "dim italic"))
    return _box_row(w, segs)


def list_hint(w, stored):
    parts = [("enter manage" if stored else "enter sign in", 0)]
    if stored:
        parts.append(("delete remove", 2))
    parts += [("esc close", 0), ("your draft is kept", 1)]
    return parts


def providers_box(w, st, sel, typed="", first=0, count=None, error=None, hint=None):
    rows = [_box_title(w, "Providers")]
    rows += prov_rows(w, st, sel, first, count)
    if count is not None and first + count < len(ROWS):
        rows.append(_box_row(w, [("   … " + str(len(ROWS) - first - count) + " more", "dim italic")]))
    if error:
        rows.append(_box_row(w, []))
        for k, line in enumerate(error):
            rows.append(_box_row(w, [("  ✗ " if k == 0 else "    ", "error bold" if k == 0 else "muted"),
                                     (line, "error" if k == 0 else "muted")]))
    rows.append(_search_row(w, typed))
    rows.append(_bottom(w))
    stored = sel in st and not st[sel][0].startswith("env")
    rows.append(hint_row(w, hint or list_hint(w, stored)))
    return rows


# ---------------------------------------------------------------- states

def p_first(w, h):
    if w < 60:
        scroll = header(w, "leader 7c1e")
        live = [rail(w, t("Add a provider with /providers", "dim"))] + editor(w) + footer(w, nomodel=True)
        return frame(w, h, scroll, live, top_anchor=True)
    scroll = header(w)
    live = [rail(w, t("Add a provider with /providers", "dim"), t(" · then choose a model with /model", "dim"))] \
        + editor(w) + footer(w, nomodel=True)
    return frame(w, h, scroll, live, top_anchor=True)


def p_first_prompt(w, h):
    scroll = header(w)
    draft = [[("❯ ", "accent bold"), ("explain how the leader starts", ""), ("▌", "cursor")]]
    live = [rail_err(w, "Not sent: no model", " · draft kept · Add a provider with /providers")] + editor(w, draft) \
        + footer(w, nomodel=True)
    return frame(w, h, scroll, live, top_anchor=True)


def p_completion(w, h):
    comp = [L(w, ("  ❯ ", "accent bold selected"), ("/providers ", "accent bold selected"),
              ("Add or remove providers", "muted selected"), fill="selected")]
    live = comp + editor(w, [[("❯ ", "accent bold"), ("/p", ""), ("▌", "cursor")]]) + footer(w)
    return frame(w, h, history(w), live)


def p_list(w, h):
    live = providers_box(w, BASE, 5) + footer(w)
    return frame(w, h, history(w), live)


def p_list_narrow(w, h):
    live = providers_box(w, BASE, 3, count=3, first=1) + footer(w)
    return frame(w, h, header(w, model=DEF), live)


def p_actions(w, h):
    rows = [[t("  Signed in. What do you want to do?", "muted")], []]
    box = [_box_title(w, "ChatGPT Plus/Pro sign-in")] + [_box_row(w, r) for r in rows]
    for label, desc, sel in [("Sign in again", "Use another account or refresh this one", True),
                             ("Remove", "Delete the saved sign-in for openai", False)]:
        bg = " selected" if sel else ""
        box.append(_box_row(w, [t(" ❯ " if sel else "   ", "accent bold" + bg),
                                t(label.ljust(15), ("bold" if sel else "") + bg), t(desc, "muted" + bg)],
                            fill=bg.strip()))
    box += [_bottom(w), hint_row(w, [("enter choose", 0), ("esc back", 0), ("your draft is kept", 1)])]
    return frame(w, h, history(w), box + footer(w))


def key_box(w, state="empty", error=None, trace=False, title="OpenAI API key", note=None):
    rows = [[t("  Paste your API key. It is sent once to the leader and is never shown.", "muted")]]
    if note:
        rows.append([t("  " + note, "muted")])
    rows.append([])
    if trace:
        rows += trace_box_rows(w)
    if state == "empty":
        rows.append([t("  key  "), t("▌", "cursor"), t("paste or type the key; it stays hidden", "dim italic")])
        hint = [("enter save", 0), ("esc cancel", 0), ("ctrl+u clear", 1)]
    elif state == "pasted":
        rows.append([t("  key  "), t("(hidden)", "dim italic"), t(" "), t("▌", "cursor")])
        hint = [("enter save", 0), ("esc cancel", 0), ("ctrl+u clear", 1)]
    elif state == "checking":
        rows.append([t("  key  "), t("(hidden)", "dim italic")])
        hint = [("esc cancel", 0)]
    else:
        rows.append([t("  key  "), t("▌", "cursor"), t("paste a new key", "dim italic")])
        hint = [("enter save", 0), ("esc cancel", 0)]
    if error:
        rows += [[], [t("  ✗ ", "error bold"), t(error[0], "error")]]
        rows += [[t("    " + x, "muted")] for x in error[1:]]
    return dlg(w, title, rows, hint)


def p_key_empty(w, h):
    return frame(w, h, history(w), key_box(w) + footer(w))


def p_key_trace(w, h):
    return frame(w, h, history(w), key_box(w, trace=True) + footer(w))


UNCHECKED = "This provider cannot be checked before use; the key is saved as it is."


def p_key_unchecked_box(w, h):
    return frame(w, h, history(w), key_box(w, "pasted", title="Alibaba Token Plan API key", note=UNCHECKED) + footer(w))


def p_key_saved_unchecked(w, h):
    scroll = header(w) + note_ok(w, "alibaba-token-plan: Saved. This provider cannot be checked before use.",
                                 "Choose a model with /model.")
    live = [rail(w, t("✓ ", "success bold"), t("alibaba-token-plan key saved · 1 model · choose one with /model", ""))] \
        + editor(w, DRAFT) + footer(w, nomodel=True)
    return frame(w, h, scroll, live, top_anchor=True)


def p_key_bad_unchecked(w, h):
    scroll = header(w, model=DEF) + user(w, "explain how the leader starts", "14:05") + [
        L(w, ("  ✗ ", "error bold"), ("Error (auth): alibaba-token-plan refused the key (HTTP 401).", "error")),
        L(w, ("    Replace the key with /providers. Then send again.", "muted")), L(w)]
    return frame(w, h, scroll, editor(w) + footer(w, pct=0, cost="$0.00"), top_anchor=True)


def p_key_checking(w, h):
    live = [rail_work(w, "Checking the key with openai…", " 2s · esc to cancel")] + key_box(w, "checking") + footer(w)
    return frame(w, h, history(w), live)


def p_key_refused(w, h):
    err = ["openai refused the key: invalid_api_key (HTTP 401). Nothing was saved.",
           "The key is cleared from this screen. Paste a new key to try again."]
    return frame(w, h, history(w), key_box(w, "retry", err) + footer(w))


def p_key_saved(w, h):
    scroll = header(w) + note_ok(w, "openai: API key saved. 2 models ready.", "Choose a model with /model.")
    live = [rail(w, t("✓ ", "success bold"), t("openai signed in · 2 models · choose one with /model", ""))] \
        + editor(w, DRAFT) + footer(w, nomodel=True)
    return frame(w, h, scroll, live, top_anchor=True)


def browser_box(w, cap=None, waiting=True):
    rows = [[t("  Open this URL in a browser and sign in to openai:", "muted")], []]
    rows += url_rows(w, URL, cap)
    rows += [[], [t("  This box closes by itself when you approve.", "muted")]]
    return dlg(w, "ChatGPT Plus/Pro sign-in", rows, [("ctrl+y copy URL", 1), ("esc cancel", 0)])


def p_browser_wait(w, h):
    live = [rail_work(w, "Waiting for the browser…", " 12s · esc to cancel")] + browser_box(w) + footer(w)
    return frame(w, h, history(w), live)


def p_browser_narrow(w, h):
    rows = [[t("  Open this URL and sign in:", "muted")]]
    rows += url_rows(w, URL, 4)
    box = dlg(w, "ChatGPT Plus/Pro sign-in", rows, [("ctrl+y copy URL", 1), ("esc cancel", 0)])
    live = [rail_work(w, "Waiting…", " 12s")] + box + footer(w)
    return frame(w, h, header(w, model=DEF), live)


def p_browser_failed(w, h):
    rows = [[t("  ✗ ", "error bold"), t("Sign-in failed: the browser returned access_denied.", "error")], [],
            [t("  Nothing was saved. You can try again.", "muted")]]
    box = dlg(w, "ChatGPT Plus/Pro sign-in", rows, [("enter try again", 0), ("esc close", 0)])
    return frame(w, h, history(w), box + footer(w))


def p_busy(w, h):
    rows = [[t("  ✗ ", "error bold"), t("Could not start: another client is signing in to openai.", "error")], [],
            [t("  Try again in a moment.", "muted")]]
    box = dlg(w, "ChatGPT Plus/Pro sign-in", rows, [("enter try again", 0), ("esc close", 0)])
    return frame(w, h, history(w), box + footer(w))


def p_removed_elsewhere(w, h):
    rows = [[t("  ✗ ", "error bold"), t("Sign-in cancelled: another client removed openai.", "error")], [],
            [t("  Nothing was saved. You can sign in again.", "muted")]]
    box = dlg(w, "ChatGPT Plus/Pro sign-in", rows, [("enter try again", 0), ("esc close", 0)])
    return frame(w, h, history(w), box + footer(w))


def p_lost(w, h):
    rows = [[t("  ✗ ", "error bold"), t("The sign-in stopped because the leader is gone.", "error")], [],
            [t("  Nothing was saved. When the leader is back, start again with /providers.", "muted")]]
    live = [rail_err(w, "Disconnected from the leader", " · reconnecting (attempt 1, next in 1s)")]
    live += dlg(w, "ChatGPT Plus/Pro sign-in", rows, [("esc close", 0)])
    live += footer(w, dot=("●", "error"), place="leader lost", place_role="error")
    return frame(w, h, history(w), live)


def p_browser_cancelled(w, h):
    scroll = history(w) + note_cancel(w, "Sign-in to openai cancelled. Nothing was saved.")
    return frame(w, h, scroll, editor(w, DRAFT) + footer(w))


def p_browser_success(w, h):
    scroll = history(w) + note_ok(w, f"openai: signed in as {ACCOUNT}. 2 models ready.")
    live = [rail(w, t("✓ ", "success bold"), t("openai signed in · 2 models", ""))] + editor(w, DRAFT) + footer(w)
    return frame(w, h, scroll, live)


def code_box(w, state="empty", mode="address"):
    """Claude Pro/Max box. mode "address": browser flow that also takes the pasted callback address.
    mode "code": the callback port is busy, so the leader uses the copy-code flow."""
    if mode == "address":
        rows = [[t("  Open this URL and approve. The box closes by itself when you approve.", "muted")],
                [t("  If the browser runs on another machine, paste the address it opened.", "muted")], []]
        label, hint_text = "  address  ", "paste the address; it stays hidden"
        send = "enter send address"
    else:
        rows = [[t("  The callback port is busy, so this sign-in uses a code instead.", "muted")],
                [t("  Open this URL, approve, then paste the code that the page shows.", "muted")], []]
        label, hint_text = "  code  ", "paste the code; it stays hidden"
        send = "enter send code"
    rows += url_rows(w, URL)
    rows.append([])
    if state == "empty":
        rows.append([t(label), t("▌", "cursor"), t(hint_text, "dim italic")])
        hint = [(send, 0), ("ctrl+y copy URL", 2), ("esc cancel", 0)]
    else:
        rows.append([t(label), t("(hidden)", "dim italic")])
        hint = [("esc cancel", 0)]
    return dlg(w, "Claude Pro/Max sign-in", rows, hint)


def p_code_wait(w, h):
    live = [rail_work(w, "Waiting for the browser or an address…", " 8s")] + code_box(w) + footer(w)
    return frame(w, h, history(w), live)


def p_code_fallback(w, h):
    live = [rail_work(w, "Waiting for a code…", " 3s")] + code_box(w, mode="code") + footer(w)
    return frame(w, h, history(w), live)


def p_code_checking(w, h):
    live = [rail_work(w, "Checking the address with anthropic…", " 1s · esc to cancel")] + code_box(w, "checking") + footer(w)
    return frame(w, h, history(w), live)


def device_box(w, expired=False):
    if expired:
        rows = [[t("  ✗ ", "error bold"), t("The code expired. No sign-in happened.", "error")], [],
                [t("  Get a new code to try again.", "muted")]]
        return dlg(w, "xAI Grok sign-in", rows, [("enter get a new code", 0), ("esc close", 0)])
    rows = [[t("  1. Open this page on any device:", "muted")],
            [t("     " + DEVICE_URL)], [],
            [t("  2. Enter this code:", "muted")],
            [t("     "), t("WDJB-MJHT", "accent bold")], [],
            [t("  The code expires in 8:12.", "muted")]]
    return dlg(w, "xAI Grok sign-in", rows, [("ctrl+y copy code", 1), ("esc cancel", 0)])


def p_device_wait(w, h):
    live = [rail_work(w, "Waiting for approval…", " 24s · esc to cancel")] + device_box(w) + footer(w)
    return frame(w, h, history(w), live)


def p_device_expired(w, h):
    return frame(w, h, history(w), device_box(w, True) + footer(w))


def p_remove_confirm(w, h):
    rows = [[t("  This deletes the saved sign-in for openai from this leader.", "muted")],
            [t("  Environment variables are not changed.", "muted")]]
    box = dlg(w, "Remove ChatGPT Plus/Pro sign-in?", rows, [("enter remove", 0), ("esc keep it", 0)])
    return frame(w, h, history(w), box + footer(w))


def p_remove_env(w, h):
    scroll = history(w) + note_ok(w, "Removed the saved credential for anthropic.",
                                  "ANTHROPIC_API_KEY is still set, so anthropic stays usable.")
    return frame(w, h, scroll, editor(w, DRAFT) + footer(w))


def p_remove_none(w, h):
    err = ["Nothing to remove: anthropic uses ANTHROPIC_API_KEY from the environment.",
           "Unset the variable in your shell to stop using it."]
    live = providers_box(w, BASE, 2, error=err, hint=[("enter sign in", 0), ("esc close", 0)]) + footer(w)
    return frame(w, h, history(w), live)


def p_remove_current(w, h):
    scroll = history(w, model=("openai", "gpt-5.5")) + note_ok(w, "Removed the saved credential for openai.",
                                  "The current model gpt-5.5 is no longer available. Choose one with /model.")
    live = [rail(w, t("No model. Choose one with /model", "dim"))] + editor(w, DRAFT) + footer(w, nomodel=True)
    return frame(w, h, scroll, live)


def p_remove_prompt(w, h):
    scroll = history(w, model=("openai", "gpt-5.5")) + note_ok(w, "Removed the saved credential for openai.",
                                  "The current model gpt-5.5 is no longer available. Choose one with /model.")
    live = [rail_err(w, "Not sent: no model", " · draft kept · Choose one with /model")] + editor(w, DRAFT) \
        + footer(w, nomodel=True)
    return frame(w, h, scroll, live)


def p_needs_leader(w, h):
    scroll = header(w, "in-process agent")
    live = [rail(w, t("Needs the leader; this session runs in process.", "dim")),
            rail(w, t("Sign in from a shell: ask auth login --provider <id> --method <id>", "dim"))] \
        + editor(w, [[("❯ ", "accent bold"), ("▌", "cursor")]]) \
        + footer(w, dot=("○", "muted"), place="in-process", nomodel=True)
    return frame(w, h, scroll, live, top_anchor=True)


def model_group_rows(w, groups, sel):
    out, k = [], 0
    for prov, items in groups:
        out.append(_box_row(w, [t("   "), t("● ", PR[prov]), t(prov, "muted bold")]))
        for label, ctx, tag in items:
            s = k == sel
            bg = " selected" if s else ""
            out.append(_box_row(w, [t(" ❯ " if s else "   ", "accent bold" + bg), t("  "), t(label, ("bold" if s else "") + bg)],
                                right=[t(f"{ctx:>5}  ", "muted" + bg),
                                       t(f"{tag:<10} ", {"reasoning": "assistant", "default": "muted"}.get(tag, "success") + bg)],
                                fill=bg.strip()))
            k += 1
    return out


MODEL_GROUPS = [
    ("alibaba-token-plan", [("deepseek-v4.1-flash (anthropic-messages)", "1M", "current"),
                            ("deepseek-v4.1-flash (openai-completions)", "1M", "reasoning")]),
    ("openai", [("gpt-5.5", "1M", "reasoning"), ("gpt-5.6-sol", "1M", "reasoning")]),
]


def p_model_grouped(w, h):
    live = [rail(w, t("✓ ", "success bold"), t("openai signed in · 2 models", ""))]
    live += [_box_title(w, "Select model"), _filter_row(w)] + model_group_rows(w, MODEL_GROUPS, 2) + [
        _bottom(w), hint_row(w, [("enter select", 0), ("esc close", 0), ("your draft is kept", 1)])] + footer(w)
    return frame(w, h, history(w), live)


def p_model_empty(w, h):
    rows = [_box_title(w, "Select model"),
            _box_row(w, []),
            _box_row(w, [t("  No usable models yet.", "muted")]),
            _box_row(w, [t("  Use /providers to add providers.")]),
            _box_row(w, []),
            _bottom(w), hint_row(w, [("esc close", 0), ("your draft is kept", 1)])]
    return frame(w, h, header(w), rows + footer(w, nomodel=True), top_anchor=True)


def p_observer(w, h):
    st = dict(BASE)
    st[5] = ("signed in", "success")
    live = [rail(w, t("Providers changed in the leader: xai signed in · 1 model", "dim"))]
    live += providers_box(w, st, 5) + footer(w, dot=("◉", "warning"), place="observer 3f2a", place_role="warning")
    return frame(w, h, history(w), live)


def p_session_start(w, h):
    live = editor(w) + footer(w, pct=0, cost="$0.00")
    return frame(w, h, header(w, model=DEF), live, top_anchor=True)


def p_session_start_api(w, h):
    scroll = header(w, model=None)[:1] + model_rows(w, "/".join(DEF), api="anthropic-messages") + [L(w)]
    live = editor(w) + footer(w, pct=0, cost="$0.00")
    return frame(w, h, scroll, live, top_anchor=True)


def p_model_changed(w, h):
    scroll = history(w) + model_rows(w, "openai/gpt-5.5", "/".join(DEF)) + [L(w)]
    live = [rail(w, t("✓ ", "success bold"), t("Default model: openai/gpt-5.5", ""))]
    return frame(w, h, scroll, live + editor(w, DRAFT) + footer(w, model="gpt-5.5"))


def p_trace_start(w, h):
    scroll = header(w, model=DEF)[:-1] + trace_scroll_rows(w) + [L(w)]
    return frame(w, h, scroll, editor(w) + footer(w, pct=0, cost="$0.00"), top_anchor=True)


def p_fallback_start(w, h):
    note = "Default model openai/gpt-5.5 is not usable (not signed in)."
    scroll = header(w, model=DEF)[:-1]
    ind = "         " if w >= 60 else "  "
    scroll += [L(w, (ind + r, "muted")) for r in wrap(note, w - len(ind) - 1)] + [L(w)]
    return frame(w, h, scroll, editor(w) + footer(w, pct=0, cost="$0.00"), top_anchor=True)


def p_model_default(w, h):
    groups = [("alibaba-token-plan", [("deepseek-v4.1-flash (anthropic-messages)", "1M", "current"),
                                      ("deepseek-v4.1-flash (openai-completions)", "1M", "reasoning")]),
              ("openai", [("gpt-5.5", "1M", "default"), ("gpt-5.6-sol", "1M", "reasoning")])]
    live = [_box_title(w, "Select model"), _filter_row(w)] + model_group_rows(w, groups, 0) + [
        _bottom(w), hint_row(w, [("enter select", 0), ("esc close", 0), ("your draft is kept", 1)])] + footer(w)
    return frame(w, h, history(w), live)


def p_attach(w, h):
    scroll = [L(w, ("  ── session 8d07 ──", "dim")), L(w)] + model_rows(w, "/".join(DEF)) + [L(w)]
    scroll += user(w, "show me the probe code", "14:03") + answer(w, "The probe lives in internal/leader/client.go.") \
        + [L(w)] + meta_row(w, DEF[1])
    live = editor(w, [[("◉ ", "warning"), ("Watching. Type /take to take control when the run ends.", "dim italic")]]) \
        + footer(w, dot=("◉", "warning"), place="observer 8d07", place_role="warning")
    return frame(w, h, scroll, live)


def p_restored(w, h):
    scroll = history(w) + [L(w, ("  ↻ ", "success"), ("Reconnected. Session 3f2a restored.", "success"))]
    scroll += model_rows(w, "/".join(DEF)) + [L(w)]
    return frame(w, h, scroll, editor(w, DRAFT) + footer(w))


SIZES = [(80, 24), (120, 40)]
N80 = [(80, 24)]
STATES = [
    ("P1", "First run: no provider",
     "No provider is usable, so there is no default model. The header has no model line, the footer shows no model, and a notice names the next step.",
     p_first, [(80, 24), (40, 14)]),
    ("P2", "First prompt with no model",
     "You pressed enter. Nothing is sent, the draft stays, and the live rail gives a hint, not an error trace.",
     p_first_prompt, N80),
    ("P2a", "Command list shows /providers",
     "You typed /p. The command list opens above the editor, as for the other commands.",
     p_completion, N80),
    ("P3", "/providers: one row for each login option",
     "/providers replaces the editor with the provider box. Each row is one way to sign in; a row that has a credential shows where it comes from. The search row is always at the bottom. The draft is kept.",
     p_list, N80),
    ("P4", "/providers at 40 columns",
     "The box shows a window of three rows and a row that says how many are hidden. Long names are cut with an ellipsis so the status stays. The list is scrolled to the selected row.",
     p_list_narrow, [(40, 14)]),
    ("P5", "Enter on a signed-in row",
     "A row that has a saved credential asks what to do: sign in again, or remove. Other rows start their sign-in at once.",
     p_actions, N80),
    ("P6", "API key: masked input",
     "The key is pasted or typed. It never appears on screen; the input shows a fixed hint.",
     p_key_empty, N80),
    ("P6a", "API key with TEA_TRACE set",
     "TEA_TRACE writes raw keyboard input to a file, so the box warns before you paste. The same rows show in the Claude Pro/Max box.",
     p_key_trace, N80),
    ("P7", "API key: checking",
     "The key went to the leader. The input is locked and shows (hidden). The wait is on the live rail above the box.",
     p_key_checking, N80),
    ("P8", "API key: refused",
     "The leader refused the key. The reason is shown, the input is empty again, and nothing was saved.",
     p_key_refused, N80),
    ("P9", "API key: saved on first run",
     "The note is in scrollback, a status line names the result, and the footer still shows no model until you choose one.",
     p_key_saved, N80),
    ("P6b", "API key for a provider that cannot be checked",
     "The Alibaba Token Plan key is saved without a live check, and the box says so before you press enter.",
     p_key_unchecked_box, N80),
    ("P9a", "API key saved without a check",
     "There is no checking step. The note says the key was saved and cannot be checked before use, then names the next step.",
     p_key_saved_unchecked, N80),
    ("P9b", "First prompt with a bad unchecked key",
     "The key was wrong. The first prompt ends with the usual auth error block of the earlier design.",
     p_key_bad_unchecked, N80),
    ("P10", "Browser sign-in: waiting",
     "The URL is shown in full, wrapped. ctrl+y copies it and the terminal can open it as a link.",
     p_browser_wait, N80),
    ("P11", "Browser sign-in at 40 columns",
     "The URL shows as many rows as fit, then a row that tells you ctrl+y copies the full URL. The cancel key always stays.",
     p_browser_narrow, [(40, 14)]),
    ("P12", "Browser sign-in: failed",
     "The browser flow ended with an error. The reason is shown, and you can try again.",
     p_browser_failed, N80),
    ("P12a", "Sign-in cannot start: another client is signing in",
     "The leader allows one sign-in for each provider at a time. The box says so and nothing else changes.",
     p_busy, N80),
    ("P12b", "Leader lost during sign-in",
     "The connection dropped. The sign-in is not resumed; the dialog says what happened and nothing was saved.",
     p_lost, N80),
    ("P12c", "Sign-in cancelled because another client removed the provider",
     "Another client removed openai while this client was signing in. The removal wins, and this box says why.",
     p_removed_elsewhere, N80),
    ("P13", "Browser sign-in: cancelled",
     "esc cancelled the sign-in. A marker stays in scrollback and the draft is back.",
     p_browser_cancelled, N80),
    ("P14", "Browser sign-in: success",
     "The leader returned the account name. The note names it, and the status line says how many models are ready.",
     p_browser_success, N80),
    ("P15", "Claude Pro/Max: browser flow, or a pasted address",
     "The leader starts the browser flow. If the browser runs on another machine, you paste the address it opened. The address carries a one-time code, so the input is masked.",
     p_code_wait, N80),
    ("P15a", "Claude Pro/Max: the callback port is busy",
     "The leader could not open the callback port, so it started the copy-code flow. The box asks for the code.",
     p_code_fallback, N80),
    ("P16", "Claude Pro/Max: checking the address",
     "The address went to the leader. The input is locked.",
     p_code_checking, N80),
    ("P17", "Device-code sign-in: waiting",
     "xai shows a short page address and a code. The TUI waits while the leader polls.",
     p_device_wait, N80),
    ("P18", "Device-code sign-in: code expired",
     "The code ran out of time. enter asks the leader for a new code.",
     p_device_expired, N80),
    ("P19", "Remove: confirmation",
     "delete on a row with a saved credential, or Remove in the row menu, asks before it deletes.",
     p_remove_confirm, N80),
    ("P20", "Remove: done, environment key still applies",
     "The saved credential is gone, but an environment variable still works, so the note says so.",
     p_remove_env, N80),
    ("P21", "Remove: nothing to remove",
     "delete on a row that only uses an environment variable changes nothing and says how to stop using it.",
     p_remove_none, N80),
    ("P22", "Remove the provider of the current model",
     "The current model is no longer usable. The footer shows no model and a persistent notice names the next step.",
     p_remove_current, N80),
    ("P23", "Prompt after the current model was removed",
     "Enter sends nothing. The draft stays and the live rail points to /model.",
     p_remove_prompt, N80),
    ("P24", "/providers needs the leader",
     "The agent runs in this process, so sign-in is not available here. The message is the same as for /sessions.",
     p_needs_leader, N80),
    ("P28", "Session start: the model line",
     "One line under the header names the provider and the model of the session. At 40 columns the label is dropped so the longest pair fits.",
     p_session_start, [(80, 24), (40, 14)]),
    ("P28a", "Session start: a model on two wire formats",
     "The same model id exists on two wire formats under one provider, so the line keeps the format in parentheses, as the /model list does. When the row is too wide (at 40 columns), the format moves to its own row.",
     p_session_start_api, [(80, 24), (40, 14)]),
    ("P28b", "Session start with TEA_TRACE set",
     "The warning shows once at start, in scrollback under the start line.",
     p_trace_start, N80),
    ("P28c", "Session start: the saved default is not usable",
     "The start line names the model in use; the row under it says why the saved default was not used.",
     p_fallback_start, [(80, 24), (40, 14)]),
    ("P29", "Model changed: a new line in scrollback",
     "You chose openai/gpt-5.5 in /model. A new line shows the new pair and, when it fits, the old one. The rail says it is now the default. The footer shows the bare model id.",
     p_model_changed, N80),
    ("P30", "Attach as observer: divider, then the model line",
     "After the session divider, the model line names the model of the session that you watch. The followed history comes below.",
     p_attach, N80),
    ("P31", "Session restored after a reconnect",
     "The reconnect note is followed by the model line of the restored session.",
     p_restored, N80),
    ("P25", "/model after sign-in: models grouped by provider",
     "Only models of usable providers are listed. The new provider is a group of its own. Here you pressed down twice, so gpt-5.5 is selected and deepseek is still the current model.",
     p_model_grouped, N80),
    ("P25a", "/model: the saved default differs from the current model",
     "Another client saved openai/gpt-5.5 as the default. This session still uses deepseek. The row tag says default.",
     p_model_default, N80),
    ("P26", "/model with no usable provider",
     "The box shows one hint and no filter row.",
     p_model_empty, N80),
    ("P27", "Another client: providers changed",
     "The leader announced the change. This client shows a status line and updates the open list in place.",
     p_observer, N80),
]


def block(rows):
    """Markdown for one sketch: plain text, live-region marker outside the fences, trailing empty rows counted."""
    parts, cur = [], []
    for r in rows:
        if r == MARK:
            if cur:
                parts.append("```text\n" + "\n".join(cur) + "\n```")
            parts.append("`-- live region starts --`")
            cur = []
        else:
            cur.append(plain(r))
    empty = 0
    while cur and not cur[-1].strip():
        cur.pop()
        empty += 1
    parts.append("```text\n" + "\n".join(cur) + "\n```")
    if empty:
        parts.append(f"({empty} empty rows below)")
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
