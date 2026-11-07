package tui

import (
	"errors"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Project trust flow.
//
// Before each session/new of this client (the first session and /new), the
// view asks the host for the trust state of the cwd. When the folder needs a
// decision, the startup trust box opens; the choice is saved with SetTrust or
// becomes a trust override of that session only. A trust override of the
// command line skips the question and goes with every session/new.
//
// The host reports every override as the source "flag", so the view keeps its
// own record of what it sent (trustStart). The note after the session starts
// comes from that record first and from the session trust second.
//
// No call here changes the trust of a running session. /trust saves a decision
// for later sessions only.

// trustSent is what the view sent for the trust of a session start.
type trustSent int

const (
	// sentNothing: no override and no choice; the host decided alone.
	sentNothing trustSent = iota
	// sentNotNow: the user chose Not now, Esc or ctrl+c in the startup box.
	sentNotNow
	sentFolder
	sentParent
	sentDistrust
	// sentFlag: the override of the command line.
	sentFlag
)

// trustStart is the record of the last session start of this client.
type trustStart struct {
	sent trustSent
	// override goes with the session/new of this start, and with a later
	// session/new that the next prompt makes after a failed start.
	override protocol.ACPTrustOverride
	// root is the canonical root of the trust answer, or the cwd.
	root string
	// entry is the folder of the saved entry that applied at the check.
	entry string
	// checkErr is the text of a trust check that failed.
	checkErr string
	// session is the id of the session that the start created.
	session string
}

// trustPurpose tells which flow a trust request belongs to.
type trustPurpose int

const (
	trustForStart trustPurpose = iota
	trustForCommand
)

// trustStateMsg is the answer of TrustState.
type trustStateMsg struct {
	req     uint64
	purpose trustPurpose
	detach  bool
	res     protocol.ACPTrustStateResult
	err     error
}

// trustSetMsg is the answer of SetTrust.
type trustSetMsg struct {
	req    uint64
	choice trustChoice
	res    protocol.ACPTrustStateResult
	err    error
}

// startPending tells that no session of a start exists yet: the link opens,
// or a session start waits for the trust answer, the trust box or session/new.
func (s *sessionView) startPending() bool { return s.opening || s.starting }

// startSessionCmd starts a session: the first one, or the one of /new (detach
// set). Without an override of the command line it asks the trust state first.
func (s *sessionView) startSessionCmd(detach bool) tea.Cmd {
	s.starting = true
	if s.session.Trust != "" {
		s.start = trustStart{sent: sentFlag, override: s.session.Trust, root: s.cwd}
		return s.newSessionCmd(detach, s.session.Trust)
	}
	return s.trustStateCmd(trustForStart, detach)
}

func (s *sessionView) trustStateCmd(purpose trustPurpose, detach bool) tea.Cmd {
	s.trustReq++
	c, cwd, req := s.client, s.cwd, s.trustReq
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.TrustState(ctx, cwd)
		return trustStateMsg{req: req, purpose: purpose, detach: detach, res: res, err: err}
	}
}

func (s *sessionView) onTrustState(msg trustStateMsg) tea.Cmd {
	if msg.req != s.trustReq {
		return nil
	}
	if msg.purpose == trustForCommand {
		return s.onTrustCommandState(msg)
	}
	rec := trustStart{sent: sentNothing, root: s.cwd}
	switch {
	case errors.Is(msg.err, ErrTrustNotAvailable):
		// An older host: no question and no note.
	case msg.err != nil:
		rec.checkErr = errText(msg.err)
	default:
		rec.root, rec.entry = trustRoot(msg.res, s.cwd), msg.res.EntryPath
		if msg.res.Warning != "" || msg.res.NeedsDecision {
			s.showTrustPre(msg.res)
		}
		if msg.res.NeedsDecision {
			s.start = rec
			s.openStartBox(msg.res, msg.detach)
			return nil
		}
	}
	s.start = rec
	return s.newSessionCmd(msg.detach, "")
}

// ---- the block pane before the session ----

// reserveTrustPre puts the placeholders of the trust warning and of the
// explanation at the end of the transcript, unless they exist.
func (s *sessionView) reserveTrustPre() {
	if s.preWarn != "" {
		return
	}
	s.preWarn, s.preExplain = s.nextBlockID(), s.nextBlockID()
	s.m.applyBlock(AppendBlockMsg{ID: s.preWarn, Block: noRows{}, Stable: false})
	s.m.applyBlock(AppendBlockMsg{ID: s.preExplain, Block: noRows{}, Stable: false})
}

// showTrustPre shows the warning of a trust answer and, when the answer needs
// a decision, the explanation.
func (s *sessionView) showTrustPre(res protocol.ACPTrustStateResult) {
	s.reserveTrustPre()
	if res.Warning != "" {
		s.m.applyBlock(ReplaceBlockMsg{ID: s.preWarn, Block: noticeBlock{text: "Warning: " + safeText(res.Warning), role: RoleWarning}})
	}
	if res.NeedsDecision {
		s.m.applyBlock(ReplaceBlockMsg{ID: s.preExplain, Block: newTrustExplain(res, s.cwd, s.home)})
	}
}

// endTrustPre removes the explanation and makes the blocks of the start final.
// The warning stays.
func (s *sessionView) endTrustPre() {
	if s.preWarn == "" {
		return
	}
	s.m.applyBlock(ReplaceBlockMsg{ID: s.preExplain, Block: noRows{}})
	s.m.applyBlock(FinishBlockMsg{ID: s.preWarn})
	s.m.applyBlock(FinishBlockMsg{ID: s.preExplain})
	s.preWarn, s.preExplain = "", ""
}

// ---- the startup box ----

// parentShown tells whether the box offers the parent folder of root. The
// file system root has no parent, and the trust of the home folder would
// apply to every project of the user.
func (s *sessionView) parentShown(root string) bool {
	parent := filepath.Dir(root)
	return parent != root && parent != "/" && (s.home == "" || parent != s.home)
}

// pathText is a path for the screen: the home as "~" and no control
// character. short is the form "~/…/<last element>".
func (s *sessionView) pathText(p string) (full, short string) {
	full = safeText(tildePath(p, s.home))
	return full, lastElementPath(full)
}

func (s *sessionView) openStartBox(res protocol.ACPTrustStateResult, detach bool) {
	root := trustRoot(res, s.cwd)
	parent, parentShort := s.pathText(filepath.Dir(root))
	choices := []trustChoice{
		{label: "Not now", short: "Not now", desc: "Do not load them now. Ask again in the next session.", notNow: true},
		{label: "Trust this folder", short: "Trust this folder", desc: "Load them now and in later sessions.",
			target: protocol.ACPTrustTargetFolder, decision: protocol.ACPTrustDecisionTrust},
	}
	if s.parentShown(root) {
		choices = append(choices, trustChoice{label: "Trust parent folder", short: "Trust parent folder",
			desc: "Trust " + parent + " and every folder in it.", descShort: "Trust " + parentShort + " and every folder in it.",
			target: protocol.ACPTrustTargetParent, decision: protocol.ACPTrustDecisionTrust})
	}
	choices = append(choices, trustChoice{label: "Do not trust", short: "Do not trust", desc: "Do not load them. Do not ask again.",
		target: protocol.ACPTrustTargetFolder, decision: protocol.ACPTrustDecisionDistrust})
	s.tbox = &trustBox{kind: trustStartBox, choices: choices, detach: detach}
	s.m.SetFocus(FocusDialog)
}

func (s *sessionView) closeTrustBox() {
	s.tbox = nil
	s.m.SetFocus(FocusEditor)
}

// onTrustKey handles a key while a trust box is open. The box takes every
// key.
func (s *sessionView) onTrustKey(k tea.KeyPressMsg) tea.Cmd {
	tb := s.tbox
	switch tb.key(k) {
	case trustQuit:
		return s.requestQuit()
	case trustCancel:
		return s.trustCancel()
	case trustChoose:
		return s.trustChoose()
	}
	return nil
}

// onTrustCtrlC is ctrl+c while a trust box is open: the same as Esc. It never
// quits and never counts as the first press of the quit confirmation.
func (s *sessionView) onTrustCtrlC() tea.Cmd {
	s.quitUntil = time.Time{}
	if s.tbox.locked {
		return nil
	}
	return s.trustCancel()
}

// trustCancel is Esc: Not now in the startup box, close in the /trust box.
func (s *sessionView) trustCancel() tea.Cmd {
	tb := s.tbox
	s.closeTrustBox()
	if tb.kind == trustCommandBox {
		return nil
	}
	return s.startNotNow(tb.detach)
}

// startNotNow starts the session untrusted and saves nothing.
func (s *sessionView) startNotNow(detach bool) tea.Cmd {
	s.endTrustPre()
	s.start.sent, s.start.override = sentNotNow, protocol.ACPTrustDeny
	return s.newSessionCmd(detach, protocol.ACPTrustDeny)
}

// trustChoose acts on the highlighted row of the open box.
func (s *sessionView) trustChoose() tea.Cmd {
	tb := s.tbox
	ch, ok := tb.selected()
	if !ok {
		return nil
	}
	if ch.notNow {
		s.closeTrustBox()
		return s.startNotNow(tb.detach)
	}
	if tb.kind == trustCommandBox && ch.current {
		s.closeTrustBox()
		s.setNotice("No change.", false)
		return nil
	}
	tb.locked = true
	s.trustReq++
	tb.req = s.trustReq
	c, req := s.client, protocol.ACPTrustSetRequest{Cwd: s.cwd, Target: ch.target, Decision: ch.decision}
	n := tb.req
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.SetTrust(ctx, req)
		return trustSetMsg{req: n, choice: ch, res: res, err: err}
	}
}

func (s *sessionView) onTrustSet(msg trustSetMsg) tea.Cmd {
	tb := s.tbox
	if tb == nil || msg.req != tb.req || msg.req != s.trustReq {
		return nil
	}
	tb.locked = false
	if msg.err != nil {
		var re *RequestError
		if tb.kind == trustCommandBox && errors.As(msg.err, &re) && re.Kind == string(protocol.ACPErrInvalidState) && re.Message != "" {
			// The trust store cannot be changed: the box shows its warning.
			tb.storeErr = safeText(re.Message)
			return nil
		}
		tb.err = safeText(errText(msg.err))
		return nil
	}
	s.closeTrustBox()
	if tb.kind == trustCommandBox {
		s.setResult(s.savedText(tb.rootPath, msg.choice) + " · " + s.appliesText())
		return nil
	}
	switch {
	case msg.choice.target == protocol.ACPTrustTargetParent:
		s.start.sent = sentParent
	case msg.choice.decision == protocol.ACPTrustDecisionTrust:
		s.start.sent = sentFolder
	default:
		s.start.sent = sentDistrust
	}
	s.start.override = ""
	s.endTrustPre()
	return s.newSessionCmd(tb.detach, "")
}

// ---- /trust ----

// openTrust is /trust: it reads the trust state again and opens the box.
func (s *sessionView) openTrust() tea.Cmd {
	switch {
	case s.startPending():
		s.setNotice("Starting, please wait…", false)
		return nil
	case s.conn != connUp:
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	case s.trustReading:
		return nil
	}
	s.trustReading = true
	return s.trustStateCmd(trustForCommand, false)
}

func (s *sessionView) onTrustCommandState(msg trustStateMsg) tea.Cmd {
	s.trustReading = false
	switch {
	case errors.Is(msg.err, ErrTrustNotAvailable):
		s.setNotice("/trust is not available: the leader is an older version.", true)
		return nil
	case msg.err != nil:
		s.setNotice("Could not read the project trust: "+safeText(errText(msg.err)), true)
		return nil
	}
	if s.tbox != nil || s.boxOpen() || s.sel != nil {
		// Another box opened while the answer was on its way.
		return nil
	}
	res := msg.res
	if res.Source == trustSourceStoreError {
		s.tbox = &trustBox{kind: trustCommandBox, storeErr: safeText(res.Warning)}
		s.m.SetFocus(FocusDialog)
		return nil
	}
	root := trustRoot(res, s.cwd)
	rootText, rootShort := s.pathText(root)
	parentPath := filepath.Dir(root)
	parentText, parentShort := s.pathText(parentPath)
	saved := res.Source == trustSourceSaved || res.Source == trustSourceSavedAncestor
	folderCur := saved && res.EntryPath == root
	parentCur := saved && res.EntryPath == parentPath && res.EntryPath != root
	var choices []trustChoice
	choices = append(choices, trustChoice{label: "Trust this folder", short: "Trust this folder",
		desc: "Save trust for " + rootText + " only.", descShort: "Save trust for " + rootShort + " only.",
		target: protocol.ACPTrustTargetFolder, decision: protocol.ACPTrustDecisionTrust, current: folderCur && res.Trusted})
	if s.parentShown(root) {
		choices = append(choices, trustChoice{label: "Trust parent folder", short: "Trust parent folder",
			desc: "Trust " + parentText + " and every folder in it.", descShort: "Trust " + parentShort + " and every folder in it.",
			target: protocol.ACPTrustTargetParent, decision: protocol.ACPTrustDecisionTrust, current: parentCur && res.Trusted})
	}
	choices = append(choices, trustChoice{label: "Do not trust", short: "Do not trust", desc: "Do not load the project files here.",
		target: protocol.ACPTrustTargetFolder, decision: protocol.ACPTrustDecisionDistrust, current: folderCur && !res.Trusted})
	if res.EntryPath != "" && (res.EntryPath == root || res.EntryPath == parentPath) {
		target := protocol.ACPTrustTargetFolder
		if res.EntryPath != root {
			target = protocol.ACPTrustTargetParent
		}
		entry, entryShort := s.pathText(res.EntryPath)
		choices = append(choices, trustChoice{label: "Forget the saved choice", short: "Forget saved choice",
			desc: "Remove the entry of " + entry + ". Ask again.", descShort: "Remove the entry of " + entryShort + ". Ask again.",
			target: target, decision: protocol.ACPTrustDecisionClear})
	}
	// The first highlight is the saved decision, else the safe Do not trust.
	sel, safe := -1, 0
	for i, ch := range choices {
		if ch.current && sel < 0 {
			sel = i
		}
		if ch.decision == protocol.ACPTrustDecisionDistrust {
			safe = i
		}
	}
	if sel < 0 {
		sel = safe
	}
	s.tbox = &trustBox{kind: trustCommandBox, choices: choices, sel: sel, root: rootText, rootPath: root,
		saved: s.savedRowText(res, root), session: s.sessionTrustText()}
	s.m.SetFocus(FocusDialog)
	return nil
}

// savedRowText is the value of the "Saved:" row of the /trust box.
func (s *sessionView) savedRowText(res protocol.ACPTrustStateResult, root string) string {
	if res.EntryPath == "" {
		return "none"
	}
	entry, _ := s.pathText(res.EntryPath)
	if res.Source != trustSourceSaved && res.Source != trustSourceSavedAncestor {
		// The folder has no gated files, so the answer does not carry the
		// value of the entry.
		return "entry of " + entry
	}
	text := "not trusted"
	if res.Trusted {
		text = "trusted"
	}
	if res.EntryPath != root {
		text += ", from " + entry
	}
	return text
}

// sessionTrustText is the value of the "This session:" row of the /trust box.
func (s *sessionView) sessionTrustText() string {
	st := s.sessTrust
	if st == nil || s.stateSession != s.sess.ID {
		return "unknown"
	}
	text := "not trusted"
	if st.Trusted {
		text = "trusted"
	}
	reason := ""
	if s.start.session == s.sess.ID {
		switch {
		case s.start.sent == sentFlag && s.start.override == protocol.ACPTrustApprove:
			reason = "--approve"
		case s.start.sent == sentFlag:
			reason = "--no-approve"
		case s.start.sent == sentNotNow:
			reason = "not now"
		}
	}
	if reason == "" {
		switch st.Source {
		case trustSourceNoGatedFiles:
			reason = "no project files"
		case trustSourceDefaultAlways:
			reason = "default always"
		case trustSourceDefaultNever:
			reason = "default never"
		case trustSourceStoreError:
			reason = "trust store error"
		}
	}
	if reason != "" {
		text += " (" + reason + ")"
	}
	return text
}

// savedText is the result text of a /trust save, with the success glyph from
// setResult.
func (s *sessionView) savedText(root string, ch trustChoice) string {
	rootText, _ := s.pathText(root)
	parentText, _ := s.pathText(filepath.Dir(root))
	switch {
	case ch.decision == protocol.ACPTrustDecisionClear && ch.target == protocol.ACPTrustTargetParent:
		return "Saved: removed the saved choice of " + parentText
	case ch.decision == protocol.ACPTrustDecisionClear:
		return "Saved: removed the saved choice of " + rootText
	case ch.target == protocol.ACPTrustTargetParent:
		return "Saved: trust " + parentText + " and every folder in it"
	case ch.decision == protocol.ACPTrustDecisionTrust:
		return "Saved: trust " + rootText
	}
	return "Saved: do not trust " + rootText
}

// appliesText says when a /trust save applies.
func (s *sessionView) appliesText() string {
	switch s.session.Trust {
	case protocol.ACPTrustApprove:
		return "--approve still applies in this process"
	case protocol.ACPTrustDeny:
		return "--no-approve still applies in this process"
	}
	return "applies to the next session (/new)"
}

// ---- notes after the session starts ----

// addTrustSlots puts the placeholders of the trust note and of the settings
// problems at the end of the transcript. The state answer fills them.
func (s *sessionView) addTrustSlots() {
	note, diag := s.nextBlockID(), s.nextBlockID()
	s.m.applyBlock(AppendBlockMsg{ID: note, Block: noRows{}, Stable: false})
	s.m.applyBlock(AppendBlockMsg{ID: diag, Block: noRows{}, Stable: false})
	s.trustSlots = append(s.trustSlots, [2]string{note, diag})
}

// settleTrustSlots fills the placeholders with the note and the settings
// problems that the view knows now, and marks them final. Only a session that
// this client started shows them.
func (s *sessionView) settleTrustSlots() {
	own := s.start.session != "" && s.stateSession == s.start.session
	if len(s.trustSlots) > 0 {
		s.shownProblems = nil
		if own {
			s.shownProblems = append([]protocol.ACPDiagnostic(nil), s.diagnostics...)
		}
	}
	for _, ids := range s.trustSlots {
		var note, diag Block = noRows{}, noRows{}
		if own {
			note = s.trustNote()
			if len(s.diagnostics) > 0 {
				diag = diagnosticsBlock{items: s.diagnostics, home: s.home}
			}
		}
		s.m.applyBlock(ReplaceBlockMsg{ID: ids[0], Block: note})
		s.m.applyBlock(ReplaceBlockMsg{ID: ids[1], Block: diag})
		s.m.applyBlock(FinishBlockMsg{ID: ids[0]})
		s.m.applyBlock(FinishBlockMsg{ID: ids[1]})
	}
	s.trustSlots = nil
}

// trustNote is the note about the trust of the started session: the record of
// the start first, then the source of the session trust.
func (s *sessionView) trustNote() Block {
	root, _ := s.pathText(s.start.root)
	parent, _ := s.pathText(filepath.Dir(s.start.root))
	muted := func(text string) Block { return noticeBlock{text: text, role: RoleMuted} }
	switch s.start.sent {
	case sentFolder:
		return trustOKNote{text: "Trusted " + root + ". Its project files load in this session."}
	case sentParent:
		return trustOKNote{text: "Trusted " + parent + " and every folder in it. The project files of " + root + " load in this session."}
	case sentDistrust:
		return muted("Saved: do not trust " + root + ". Its project files are not loaded. To change it, use /trust.")
	case sentNotNow:
		return muted("The project files of " + root + " are not loaded in this session. The question comes again in the next session. To save a choice, use /trust.")
	case sentFlag:
		if s.start.override == protocol.ACPTrustDeny {
			return muted("The project files of " + root + " are not loaded (--no-approve).")
		}
		return noRows{}
	}
	st := s.sessTrust
	if st == nil || st.Trusted {
		return noRows{}
	}
	switch st.Source {
	case trustSourceSaved:
		return muted("The project files of " + root + " are not loaded: this folder is not trusted. To change it, use /trust.")
	case trustSourceSavedAncestor:
		entry := parent
		if s.start.entry != "" {
			entry, _ = s.pathText(s.start.entry)
		}
		return muted("The project files of " + root + " are not loaded: " + entry + " is not trusted. To change it, use /trust.")
	case trustSourceDefaultNever:
		return muted("The project files of " + root + " are not loaded: defaultProjectTrust is \"never\". To trust this folder, use /trust.")
	case trustSourceNoUI:
		text := "The project files of " + root + " are not loaded: Ask could not check the trust of this folder"
		if s.start.checkErr != "" {
			text += " (" + safeText(s.start.checkErr) + ")"
		}
		return noticeBlock{text: text + ".", role: RoleWarning}
	}
	return noRows{}
}

// applySessionFold applies the effective thinkingFold of a session that this
// client started, once: a later state answer must not undo ctrl+t.
func (s *sessionView) applySessionFold(fold string) {
	if fold == "" || s.sess.ID == "" || s.sess.ID != s.start.session || s.foldApplied == s.sess.ID {
		return
	}
	s.foldApplied = s.sess.ID
	switch fold {
	case "expanded":
		s.m.setThinkingFold(FoldExpanded)
	case "truncated":
		s.m.setThinkingFold(FoldTruncated)
	case "collapsed":
		s.m.setThinkingFold(FoldCollapsed)
	}
}
