package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Input handling.
//
// Input order follows the architecture: host exit and cancel handling first,
// then the active selector, then the editor with its completion list, then the
// application shortcuts. The editor reports the keys it does not own as
// unhandled, and the shortcuts below take them.

const (
	needsLeaderText = "Needs the leader; this session runs in process."
	modelBusyText   = "Model can change when the run ends."
	sessionBusyText = "Session can change when the run ends."
)

func (s *sessionView) onKey(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		return s.onCtrlC()
	}
	if s.sel != nil {
		return s.onSelectorKey(k)
	}
	res := s.editor.Handle(k)
	if res.Handled {
		return s.onEditorResult(res)
	}
	switch k.String() {
	case "esc":
		return s.cancelRun()
	case "ctrl+d":
		return s.requestQuit()
	case "alt+up":
		return s.dequeueCmd()
	case "ctrl+l":
		return s.openModelSelector()
	case "ctrl+t":
		return s.toggleThinking()
	case "shift+tab":
		return s.cycleThinking()
	}
	return nil
}

func (s *sessionView) onPaste(msg tea.PasteMsg) tea.Cmd {
	if s.sel != nil {
		return nil
	}
	s.editor.Handle(msg)
	return nil
}

// onCtrlC clears a draft, closes a selector, or quits. On an empty draft the
// first press opens a short confirmation window and the second press inside it
// quits. A press that only clears a draft never counts as the first one.
func (s *sessionView) onCtrlC() tea.Cmd {
	switch {
	case s.sel != nil:
		s.closeSelector()
		return nil
	case s.editor.Text() != "":
		s.editor.Clear()
		s.quitUntil = time.Time{}
		return nil
	case s.quitConfirmed():
		return s.requestQuit()
	}
	s.askQuitConfirmation()
	return nil
}

func (s *sessionView) onEditorResult(res Result) tea.Cmd {
	switch {
	case res.Command != "":
		return s.runCommand(res.Command)
	case res.Submit != nil:
		return s.submit(*res.Submit)
	}
	return nil
}

// ---- sending ----

// submit decides between a prompt, a steer and a follow-up from the run state.
// The draft stays in the editor until the agent accepts the input.
func (s *sessionView) submit(sub Submit) tea.Cmd {
	switch {
	case s.opening:
		s.setNotice("Starting, please wait…", false)
		return nil
	case s.conn != connUp:
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	case s.send != nil:
		return nil
	case s.run.running:
		return s.queueCmd(sub, sub.Alt)
	}
	s.failure = nil
	s.send = &sendState{text: sub.Text, sub: sub, calling: true}
	return s.promptCmd(sub)
}

// promptCmd sends a prompt. After a lost session it first creates a new one;
// the earlier prompt is never sent again.
func (s *sessionView) promptCmd(sub Submit) tea.Cmd {
	c, cwd, fresh := s.client, s.cwd, s.needNew
	return func() tea.Msg {
		out := promptCalledMsg{sub: sub}
		if fresh {
			ctx, cancel := callContext()
			info, err := c.NewSession(ctx, cwd)
			cancel()
			if err != nil {
				out.err = fmt.Errorf("start a new session: %w", err)
				return out
			}
			out.session = &info
		}
		out.id, out.err = c.Prompt(sub.Text)
		return out
	}
}

func (s *sessionView) queueCmd(sub Submit, follow bool) tea.Cmd {
	c, gen := s.client, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		var (
			id  string
			err error
		)
		if follow {
			id, err = c.FollowUp(ctx, sub.Text)
		} else {
			id, err = c.Steer(ctx, sub.Text)
		}
		return queuedMsg{gen: gen, sub: sub, follow: follow, id: id, err: err}
	}
}

// cancelRun asks the agent to stop. The run is over only when the agent
// reports that it settled.
func (s *sessionView) cancelRun() tea.Cmd {
	if !s.run.running || s.run.cancelling || s.conn != connUp {
		return nil
	}
	s.run.cancelling = true
	c := s.client
	return func() tea.Msg {
		if err := c.Cancel(); err != nil {
			return cancelFailedMsg{err: err}
		}
		return nil
	}
}

// cancelFailedMsg reports that the cancel request was not sent.
type cancelFailedMsg struct{ err error }

// ---- slash commands ----

func (s *sessionView) runCommand(name string) tea.Cmd {
	switch name {
	case "/model":
		return s.openModelSelector()
	case "/new":
		return s.newSession()
	case "/sessions":
		return s.listSessions()
	case "/take":
		return s.take()
	case "/quit":
		return s.requestQuit()
	}
	return nil
}

// idleFor checks that a change of model or session can happen now and tells
// the user why not.
func (s *sessionView) idleFor(busyText string) bool {
	switch {
	case s.opening:
		s.setNotice("Starting, please wait…", false)
	case s.conn != connUp:
		s.reconnect.hintUntil = s.now().Add(noticeFor)
	case s.run.running || s.send != nil:
		s.setNotice(busyText, false)
	default:
		return true
	}
	return false
}

func (s *sessionView) newSession() tea.Cmd {
	if !s.idleFor(sessionBusyText) {
		return nil
	}
	return s.newSessionCmd(true)
}

func (s *sessionView) listSessions() tea.Cmd {
	if s.info.Mode == ModeInProcess {
		s.setNotice(needsLeaderText, false)
		return nil
	}
	if s.conn != connUp {
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	}
	c, gen := s.client, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		list, err := c.ListSessions(ctx)
		return sessionsMsg{gen: gen, list: list, err: err}
	}
}

func (s *sessionView) onSessions(msg sessionsMsg) {
	if msg.gen != s.gen {
		return
	}
	if msg.err != nil {
		s.setNotice(pick(errors.Is(msg.err, ErrNeedsLeader), needsLeaderText, "Could not list sessions: "+errText(msg.err)), true)
		return
	}
	s.openSelector(newSessionSelector(msg.list, s.sess.ID, s.home))
}

func (s *sessionView) take() tea.Cmd {
	switch {
	case s.info.Mode == ModeInProcess:
		s.setNotice(needsLeaderText, false)
		return nil
	case s.conn != connUp:
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	case !s.isObserver():
		s.setNotice("This client already drives the session.", false)
		return nil
	}
	c, gen := s.client, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		_, err := c.Take(ctx)
		return takeMsg{gen: gen, err: err}
	}
}

func (s *sessionView) onTake(msg takeMsg) tea.Cmd {
	if msg.gen != s.gen {
		return nil
	}
	if msg.err != nil {
		s.setNotice(pick(errors.Is(msg.err, ErrNeedsLeader), needsLeaderText, "Could not take control: "+errText(msg.err)), true)
		return nil
	}
	s.sess.Role = protocol.ACPRoleDriver
	s.setNotice("You drive the session now.", false)
	return s.refreshCmd(false)
}

func (s *sessionView) onCancelFailed(msg cancelFailedMsg) {
	s.run.cancelling = false
	s.setNotice("Could not cancel: "+errText(msg.err), true)
}

// ---- model and thinking ----

func (s *sessionView) openModelSelector() tea.Cmd {
	if !s.idleFor(modelBusyText) || s.openingModel {
		return nil
	}
	s.openingModel = true
	c, gen := s.client, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.Models(ctx)
		return modelsMsg{gen: gen, res: res, err: err}
	}
}

func (s *sessionView) onModels(msg modelsMsg) {
	s.openingModel = false
	if msg.gen != s.gen {
		return
	}
	if msg.err != nil {
		s.setNotice("Could not read the models: "+errText(msg.err), true)
		return
	}
	s.models = msg.res.Models
	if s.modelID == "" {
		s.modelID = msg.res.Current
	}
	s.openSelector(newModelSelector(s.models, s.modelID))
}

func (s *sessionView) openSelector(sl *selector) {
	sl.gen = s.gen
	s.sel = sl
	s.m.SetFocus(FocusSelector)
}

func (s *sessionView) closeSelector() {
	s.sel = nil
	s.m.SetFocus(FocusEditor)
}

func (s *sessionView) onSelectorKey(k tea.KeyPressMsg) tea.Cmd {
	switch s.sel.key(k) {
	case selClose:
		s.closeSelector()
	case selChoose:
		return s.choose()
	}
	return nil
}

// choose applies the selected row of the open selector.
func (s *sessionView) choose() tea.Cmd {
	it, ok := s.sel.selected()
	if !ok {
		return nil
	}
	switch s.sel.kind {
	case selectModel:
		if it.id == s.modelID {
			s.closeSelector()
			return nil
		}
		return s.setModelCmd(it.id)
	case selectSession:
		if it.id == s.sess.ID {
			s.closeSelector()
			return nil
		}
		return s.attachCmd(it.id)
	}
	return nil
}

func (s *sessionView) setModelCmd(id string) tea.Cmd {
	s.modelReq++
	s.sel.busy = true
	c, gen, req := s.client, s.gen, s.modelReq
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.SetModel(ctx, id)
		return setModelMsg{gen: gen, req: req, id: id, res: res, err: err}
	}
}

// onSetModel applies the answer of the latest request. An older request, or one
// from before a session change or a reconnect, changes nothing.
func (s *sessionView) onSetModel(msg setModelMsg) tea.Cmd {
	if msg.gen != s.gen {
		// The link or the session changed while the request was in flight. The
		// selector would wait for an answer that no longer applies.
		if s.sel != nil && s.sel.kind == selectModel {
			s.closeSelector()
		}
		return nil
	}
	if msg.req != s.modelReq {
		return nil
	}
	if s.sel != nil && s.sel.kind == selectModel {
		s.sel.busy = false
	}
	if msg.err != nil {
		text, next := s.modelFailure(msg.id, msg.err)
		if s.sel != nil && s.sel.kind == selectModel {
			s.sel.err, s.sel.next = text, next
		} else {
			s.setNotice(strings.TrimSpace(text+" "+next), true)
		}
		return nil
	}
	s.modelID, s.thinking, s.thinkTry = msg.res.ModelID, msg.res.ThinkingLevel, ""
	if s.modelID == "" {
		s.modelID = msg.id
	}
	if s.sel != nil && s.sel.kind == selectModel {
		s.closeSelector()
	}
	return nil
}

// modelFailure words a failed model change. The model stays as it was.
func (s *sessionView) modelFailure(id string, err error) (text, next string) {
	row, _ := s.modelRow(id)
	provider := row.Provider
	var re *RequestError
	if errors.As(err, &re) && re.Kind == string(protocol.ACPErrNoAPIKey) && provider != "" {
		return provider + " is not ready: not signed in. Model not changed.", "Run in a shell: " + loginCommand(provider)
	}
	if errors.As(err, &re) && re.Kind == string(protocol.ACPErrModelAccess) && provider != "" {
		return provider + ": this model is not available with your ChatGPT sign-in. Model not changed.", ""
	}
	reason := strings.TrimRight(errText(err), ". ")
	if provider != "" {
		return provider + " is not ready: " + reason + ". Model not changed.", ""
	}
	return "Could not change the model: " + reason + ". Model not changed.", ""
}

func (s *sessionView) cycleThinking() tea.Cmd {
	if !s.reasoning() {
		return nil
	}
	if !s.idleFor("Thinking level can change when the run ends.") {
		return nil
	}
	base := s.thinkTry
	if base == "" {
		base = s.thinking
	}
	next := thinkingLevels[0]
	for i, l := range thinkingLevels {
		if l == base {
			next = thinkingLevels[(i+1)%len(thinkingLevels)]
			break
		}
	}
	c, gen := s.client, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		level, err := c.SetThinking(ctx, next)
		return thinkingMsg{gen: gen, asked: next, level: level, err: err}
	}
}

func (s *sessionView) onThinking(msg thinkingMsg) {
	if msg.gen != s.gen {
		return
	}
	if msg.err != nil {
		s.setNotice("Could not change the thinking level: "+errText(msg.err), true)
		return
	}
	s.thinking = msg.level
	// A level that the model clamps would stop the cycle. Keep the asked level
	// as the base of the next step.
	s.thinkTry = ""
	if msg.level != msg.asked {
		s.thinkTry = msg.asked
	}
}

// toggleThinking hides or shows all thinking blocks. The committed blocks are
// drawn again by a rebuild of the scrollback.
func (s *sessionView) toggleThinking() tea.Cmd {
	hidden := s.trl.ToggleThinking()
	s.setNotice(ThinkingNotice(hidden), false)
	return s.m.Rebuild()
}

// ---- attach ----

// attachCmd leaves the current session and watches another one.
func (s *sessionView) attachCmd(id string) tea.Cmd {
	s.sel.busy = true
	c, gen := s.client, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		if err := c.Detach(ctx); err != nil && !ignorableDetach(err) {
			return attachMsg{gen: gen, id: id, err: fmt.Errorf("leave the session: %w", err)}
		}
		info, err := c.Attach(ctx, id)
		return attachMsg{gen: gen, id: id, info: info, err: err}
	}
}

func (s *sessionView) onAttach(msg attachMsg) tea.Cmd {
	if s.sel != nil && s.sel.kind == selectSession {
		s.closeSelector()
	}
	if msg.gen != s.gen {
		return nil
	}
	if msg.err != nil {
		// The client left the old session before the attach failed, so it has
		// none. The next prompt starts a new one.
		s.needNew = true
		s.sess = SessionInfo{Cwd: s.sess.Cwd}
		s.setNotice("Could not watch session "+ShortID(msg.id)+": "+errText(msg.err), true)
		return nil
	}
	s.useSession(msg.info)
	return s.refreshCmd(false)
}
