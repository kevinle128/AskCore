package tui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"AskCore/pkg/protocol"
)

// Key box, login boxes and removal box.
//
// One dialog at a time replaces the editor slot. The dialog owns a secret
// input for the key, the code or the pasted callback address. Its phase says
// what the box shows and which keys work; the session view moves it from
// phase to phase when an answer of the leader arrives.

// loginPhase is the state of a dialog.
type loginPhase int

const (
	// phaseStarting: the start call is in flight; the box has nothing to show yet.
	phaseStarting loginPhase = iota
	// phaseInput: the box waits for a key, a code or an address.
	phaseInput
	// phaseChecking: a secret went to the leader; the input is locked.
	phaseChecking
	// phaseWaiting: a URL or a device code shows and the leader waits.
	phaseWaiting
	// phaseFailed: the sign-in ended or could not start; enter tries again.
	phaseFailed
	// phaseLost: the connection dropped during the sign-in.
	phaseLost
	// phaseConfirm: the removal asks for a confirmation.
	phaseConfirm
	// phaseRemoving: the removal call is in flight.
	phaseRemoving
)

const (
	keyIntroText        = "Paste your API key. It is sent once to the leader and is never shown."
	keyNotCheckedText   = "This provider cannot be checked before use; the key is saved as it is."
	keyPlaceholder      = "paste or type the key; it stays hidden"
	keyRetryPlaceholder = "paste a new key"
	keyRefusedNextText  = "The key is cleared from this screen. Paste a new key to try again."
	urlCapText          = "… ctrl+y copies the full URL"
	deviceExpiredText   = "The code expired. No sign-in happened."
	deviceExpiredNext   = "Get a new code to try again."
	loginTryAgainText   = "Try again in a moment."
	loginNothingSaved   = "Nothing was saved. You can try again."
)

// uncheckedProviders are the providers whose key the leader saves without a
// live check. The leader decides; the box says so before the user presses
// enter, and the protocol has no field for it before the save.
var uncheckedProviders = map[string]bool{"alibaba-token-plan": true}

// loginDialog is the state of one box.
type loginDialog struct {
	// id numbers the dialogs of the view. A result carries it, so an answer for
	// a box that is gone changes nothing.
	id uint64
	// req numbers the calls of this box; only the answer to the latest applies.
	req   uint64
	opt   protocol.ACPLoginOption
	phase loginPhase

	// isKey is true for an API key row.
	isKey bool
	// loginID names the sign-in or the key save: made by the client for a key,
	// by the leader for a sign-in.
	loginID string
	kind    protocol.ACPLoginKind
	url     string
	// userCode is the code of a device sign-in.
	userCode string
	expires  time.Time
	since    time.Time

	secret secretInput
	// refusedText and refusedNext are the rows under the input after a refusal.
	refusedText, refusedNext string
	// failedText and failedNext are the rows of a failed or lost box.
	failedText, failedNext string
	// retryable is true when enter starts the sign-in again.
	retryable bool
	// removedReason is true when another client removed the provider.
	removed bool
	// cancelAfterStart asks to cancel as soon as the start call answers.
	cancelAfterStart bool
	// runActive is true when a run may use the provider (removal box).
	runActive bool
}

func (d *loginDialog) title() string { return oneLine(d.opt.Name) }

// hasInput tells that the box shows an input row.
func (d *loginDialog) hasInput() bool {
	return d.isKey || d.kind == protocol.ACPLoginKindBrowserAddress || d.kind == protocol.ACPLoginKindCopyCode
}

// inputLabel names the input row.
func (d *loginDialog) inputLabel() string {
	switch {
	case d.isKey:
		return "key"
	case d.kind == protocol.ACPLoginKindBrowserAddress:
		return "address"
	}
	return "code"
}

// loginOpen tells that a sign-in or key save of this box is open on the leader.
func (d *loginDialog) loginOpen() bool {
	switch d.phase {
	case phaseStarting, phaseInput, phaseChecking, phaseWaiting:
		return d.loginID != "" || d.phase == phaseStarting
	}
	return false
}

// urlRows cuts a URL by columns, at most max rows. When the URL needs more,
// the result is the first rows and capped is true.
func urlRows(raw string, width, max int) (rows []string, capped bool) {
	var cur strings.Builder
	w := 0
	for _, r := range raw {
		rw := ansi.StringWidth(string(r))
		if w+rw > width && w > 0 {
			rows = append(rows, cur.String())
			cur.Reset()
			w = 0
		}
		cur.WriteRune(r)
		w += rw
	}
	if cur.Len() > 0 {
		rows = append(rows, cur.String())
	}
	if len(rows) > max {
		return rows[:max], true
	}
	return rows, false
}

// linkable tells that a URL is safe to put in a link sequence: a plain
// absolute URL without control characters.
func linkable(raw string) bool {
	if raw == "" || strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f || r == ' ' }) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// urlRowCap is the largest number of URL rows in a slot of height rows.
func urlRowCap(height int) int { return max(1, height-4) }

// countdownText writes the time left as m:ss.
func countdownText(left time.Duration) string {
	if left < 0 {
		left = 0
	}
	secs := int((left + time.Second - 1) / time.Second)
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// slot draws the dialog for the editor slot.
func (d *loginDialog) slot(s *sessionView, width, height int) EditorSlot {
	th := s.m.theme
	b := newBox(th, width)
	short := width < shortWidth
	var c boxContent
	hint := ""
	cancelHint := []hintPart{{"esc cancel", 0}}

	switch d.phase {
	case phaseConfirm, phaseRemoving:
		c.addWrapped(th, b.inner, "This deletes the saved sign-in for "+d.opt.ProviderID+" from this leader.", RoleFg, "")
		c.addWrapped(th, b.inner, "Environment variables are not changed.", RoleFg, "")
		if d.runActive {
			c.addWrapped(th, b.inner, "A run is active and may use this provider.", RoleWarning, "")
		}
		hint = hintText(width, []hintPart{{"enter remove", 0}, {"esc keep it", 0}})
		title := "Remove " + d.title() + "?"
		if d.phase == phaseRemoving {
			hint = hintText(width, []hintPart{{"removing…", 0}})
		}
		return b.slot(title, c, hint)

	case phaseFailed, phaseLost:
		c.addWrapped(th, b.inner, d.failedText, RoleError, GlyphFail+" ")
		if d.failedNext != "" {
			c.add("")
			c.addWrapped(th, b.inner, d.failedNext, RoleFg, "")
		}
		switch {
		case d.phase == phaseLost:
			hint = hintText(width, []hintPart{{"esc close", 0}})
		case d.opt.State == protocol.ACPLoginStateNone && d.kind == protocol.ACPLoginKindDevice && d.failedText == deviceExpiredText:
			hint = hintText(width, []hintPart{{"enter get a new code", 0}, {"esc close", 0}})
		case d.failedText == deviceExpiredText:
			hint = hintText(width, []hintPart{{"enter get a new code", 0}, {"esc close", 0}})
		case d.retryable:
			hint = hintText(width, []hintPart{{"enter try again", 0}, {"esc close", 0}})
		default:
			hint = hintText(width, []hintPart{{"esc close", 0}})
		}
		return b.slot(d.title(), c, hint)
	}

	title := d.title()
	if height < 6 {
		title = "Sign in to " + d.opt.ProviderID + " · esc cancel"
	}
	switch {
	case d.isKey:
		c.addWrapped(th, b.inner, keyIntroText, RoleFg, "")
		if uncheckedProviders[d.opt.ProviderID] {
			c.addWrapped(th, b.inner, keyNotCheckedText, RoleFg, "")
		}
		introRows := len(c.rows)
		c.add("")
		if s.trace != "" {
			c.addWrapped(th, b.inner, traceWarningText(s.trace), RoleWarning, "")
			c.add("")
		}
		hint = hintText(width, []hintPart{{"enter save", 0}, {"esc cancel", 0}, {"ctrl+u clear", 1}})
		if d.phase == phaseChecking {
			hint = hintText(width, cancelHint)
		}
		d.inputRow(&c, th)
		if d.refusedText != "" {
			c.add("")
			c.addWrapped(th, b.inner, d.refusedText, RoleError, GlyphFail+" ")
			c.addWrapped(th, b.inner, d.refusedNext, RoleError, "  ")
			hint = hintText(width, []hintPart{{"enter save", 0}, {"esc cancel", 0}})
		}
		return compactLoginSlot(b, title, c, hint, height, introRows)

	case d.kind == protocol.ACPLoginKindDevice:
		if d.phase == phaseStarting {
			c.addWrapped(th, b.inner, "Starting the sign-in…", RoleMuted, "")
			return b.slot(title, c, hintText(width, cancelHint))
		}
		c.addWrapped(th, b.inner, "1. Open this page on any device:", RoleFg, "")
		c.add("    " + th.Render(RoleFg, d.url))
		c.add("")
		c.addWrapped(th, b.inner, "2. Enter this code:", RoleFg, "")
		c.add("    " + th.Style(RoleAccent).Bold(true).Render(d.userCode))
		c.add("")
		left := d.expires.Sub(s.now())
		c.addWrapped(th, b.inner, "The code expires in "+countdownText(left)+".", RoleFg, "")
		hint = hintText(width, []hintPart{{"ctrl+y copy code", 2}, {"esc cancel", 0}})
		return b.slot(title, c, hint)
	}

	// Browser, address and code boxes show the URL.
	if d.phase == phaseStarting {
		c.addWrapped(th, b.inner, "Starting the sign-in…", RoleMuted, "")
		return b.slot(title, c, hintText(width, cancelHint))
	}
	switch d.kind {
	case protocol.ACPLoginKindBrowserAddress:
		c.addWrapped(th, b.inner, "Open this URL and approve. The box closes by itself when you approve.", RoleFg, "")
		c.addWrapped(th, b.inner, "If the browser runs on another machine, paste the address it opened.", RoleFg, "")
	case protocol.ACPLoginKindCopyCode:
		c.addWrapped(th, b.inner, "The callback port is busy, so this sign-in uses a code instead.", RoleFg, "")
		c.addWrapped(th, b.inner, "Open this URL, approve, then paste the code that the page shows.", RoleFg, "")
	default:
		intro := "Open this URL in a browser and sign in to " + d.opt.ProviderID + ":"
		if short {
			intro = "Open this URL and sign in:"
		}
		c.addWrapped(th, b.inner, intro, RoleFg, "")
	}
	if !short {
		c.add("")
	}
	introRows := len(c.rows)
	var afterURL boxContent
	if d.hasInput() {
		afterURL.add("")
		if s.trace != "" {
			afterURL.addWrapped(th, b.inner, traceWarningText(s.trace), RoleWarning, "")
			afterURL.add("")
		}
		d.inputRow(&afterURL, th)
	} else if !short {
		afterURL.add("")
		afterURL.addWrapped(th, b.inner, "This box closes by itself when you approve.", RoleFg, "")
	}
	var urls boxContent
	d.urlRows(&urls, th, b.inner, urlRowCap(height))
	budget := loginContentBudget(height)
	over := func() bool { return len(c.rows)+len(urls.rows)+len(afterURL.rows) > budget }
	if over() {
		c.dropBlanks()
		afterURL.dropBlanks()
		for len(c.rows) > 0 && over() {
			c.rows = c.rows[:len(c.rows)-1]
		}
		introRows = len(c.rows)
		for cap := urlRowCap(height) - 1; cap >= 1 && over(); cap-- {
			urls = boxContent{}
			d.urlRows(&urls, th, b.inner, cap)
		}
		if over() {
			// The input and the warning come before the URL. The hint row
			// still offers ctrl+y to copy it.
			urls = boxContent{}
		}
	}
	c.rows = append(c.rows, urls.rows...)
	if afterURL.cursor != nil {
		c.cursor = &CursorPos{Row: len(c.rows) + afterURL.cursor.Row, Col: afterURL.cursor.Col}
	}
	c.rows = append(c.rows, afterURL.rows...)
	switch {
	case d.phase == phaseChecking:
		hint = hintText(width, cancelHint)
	case d.kind == protocol.ACPLoginKindBrowserAddress:
		hint = hintText(width, []hintPart{{"enter send address", 0}, {"ctrl+y copy URL", 2}, {"esc cancel", 0}})
	case d.kind == protocol.ACPLoginKindCopyCode:
		hint = hintText(width, []hintPart{{"enter send code", 0}, {"ctrl+y copy URL", 2}, {"esc cancel", 0}})
	default:
		hint = hintText(width, []hintPart{{"ctrl+y copy URL", 2}, {"esc cancel", 0}})
	}
	// A terminal so low that the hint row has no room gives the title the key.
	if height < 6 {
		title = "Sign in to " + d.opt.ProviderID + " · esc cancel"
	}
	return compactLoginSlot(b, title, c, hint, height, introRows)
}

func loginContentBudget(height int) int {
	if height < 6 {
		return max(1, height-3)
	}
	// The box keeps its title, bottom and hint rows.
	return max(1, min(modalMaxRows, height)-3)
}

func (c *boxContent) dropRow(i int) {
	c.rows = append(c.rows[:i], c.rows[i+1:]...)
	if c.cursor != nil && c.cursor.Row > i {
		c.cursor.Row--
	}
}

func (c *boxContent) dropBlanks() {
	for i := len(c.rows) - 1; i >= 0; i-- {
		if c.rows[i] == "" {
			c.dropRow(i)
		}
	}
}

func compactLoginSlot(b boxDraw, title string, c boxContent, hint string, height, introRows int) EditorSlot {
	budget := loginContentBudget(height)
	if len(c.rows) > budget {
		for i := len(c.rows) - 1; i >= 0 && len(c.rows) > budget; i-- {
			if c.rows[i] == "" {
				c.dropRow(i)
				if i < introRows {
					introRows--
				}
			}
		}
		for introRows > 0 && len(c.rows) > budget {
			introRows--
			c.dropRow(introRows)
		}
		for len(c.rows) > budget {
			i := len(c.rows) - 1
			if c.cursor != nil && c.cursor.Row == i {
				i--
			}
			c.dropRow(i)
		}
	}
	out := b.slot(title, c, hint)
	if height < 6 {
		out.Rows = out.Rows[:len(out.Rows)-1]
	}
	return out
}

// inputRow adds the input row: "key  ▌placeholder" or "key  (hidden)".
func (d *loginDialog) inputRow(c *boxContent, th *Theme) {
	label := d.inputLabel()
	lead := " " + th.Render(RoleMuted, label) + "  "
	leadW := displayWidth(" " + label + "  ")
	switch {
	case d.phase == phaseChecking:
		c.add(lead + th.Render(RoleDim, secretHiddenText))
		return
	case !d.secret.empty():
		c.add(lead + th.Render(RoleDim, secretHiddenText))
		c.cursor = &CursorPos{Row: len(c.rows) - 1, Col: leadW + displayWidth(secretHiddenText) + 1}
		return
	}
	placeholder := "paste the " + label + "; it stays hidden"
	if d.isKey {
		placeholder = keyPlaceholder
		if d.refusedText != "" {
			placeholder = keyRetryPlaceholder
		}
	}
	if d.secret.refusal != "" {
		c.add(lead + th.Style(RoleDim).Italic(true).Render(placeholder))
		c.cursor = &CursorPos{Row: len(c.rows) - 1, Col: leadW}
		c.add("")
		c.add(" " + th.Render(RoleError, GlyphFail+" "+d.secret.refusal))
		return
	}
	c.add(lead + th.Style(RoleDim).Italic(true).Render(placeholder))
	c.cursor = &CursorPos{Row: len(c.rows) - 1, Col: leadW}
}

// urlRows adds the rows of the URL. Each row is a link to the whole URL.
// The renderer does not write a link again when the row before has the same
// link, so the later rows would lose it. Each row has its own link id to make
// the links differ. The id depends only on the row, so a repeated frame does
// not change any cell.
func (d *loginDialog) urlRows(c *boxContent, th *Theme, inner, maxRows int) {
	rows, capped := urlRows(d.url, max(1, inner-1), maxRows)
	link := linkable(d.url)
	for i, r := range rows {
		text := th.Render(RoleFg, r)
		if link {
			text = ansi.SetHyperlink(d.url, "id=ask-url-"+strconv.Itoa(i)) + text + ansi.ResetHyperlink()
		}
		c.add(" " + text)
	}
	if capped {
		c.add(" " + th.Render(RoleDim, urlCapText))
	}
}
