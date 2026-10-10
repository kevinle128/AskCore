package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Session messages and their handling.
//
// Client messages reach Update through one wait command. After each message
// the view arms the next wait, so one wait is outstanding at any time, and the
// client goroutines never touch the model. Results of commands carry the
// generation in which they started.

// clientEventMsg wraps a message of the client for the update loop.
type clientEventMsg struct{ msg Message }

// clientClosedMsg reports that the event channel of the client closed.
type clientClosedMsg struct{}

// openedMsg is the result of Options.Open.
type openedMsg struct {
	client Client
	err    error
}

// newSessionMsg is the result of a command that created a session.
type newSessionMsg struct {
	gen  uint64
	info SessionInfo
	err  error
}

// promptCalledMsg is the result of the Prompt call. session is set when the
// command created a new session before it sent the prompt.
type promptCalledMsg struct {
	gen     uint64
	sub     Submit
	session *SessionInfo
	id      PromptID
	err     error
}

// queuedMsg is the result of a steer or follow-up call.
type queuedMsg struct {
	gen    uint64
	sub    Submit
	follow bool
	id     string
	err    error
}

// dequeuedMsg is the result of removing queued inputs.
type dequeuedMsg struct {
	gen      uint64
	restored []string
	taken    int
	err      error
}

// modelsMsg is the result of the catalog call that opens the selector.
type modelsMsg struct {
	gen, token uint64
	res        protocol.ACPModelsResult
	err        error
}

type setModelMsg struct {
	gen uint64
	req uint64
	id  string
	res protocol.ACPSetModelResult
	err error
}

type thinkingMsg struct {
	gen   uint64
	asked protocol.ThinkingLevel
	level protocol.ThinkingLevel
	err   error
}

type sessionsMsg struct {
	gen  uint64
	list []protocol.ACPLiveSession
	err  error
}

type attachMsg struct {
	gen  uint64
	id   string
	info SessionInfo
	err  error
}

type takeMsg struct {
	gen uint64
	err error
}

// refreshMsg is the result of the state, usage and catalog calls.
type refreshMsg struct {
	gen         uint64
	runSeq      uint64
	queueSeq    uint64
	modelSeq    uint64
	modelsToken uint64
	state       *protocol.ACPStateResult
	usage       *protocol.ACPUsageResult
	models      *protocol.ACPModelsResult
}

// tickMsg is the animation clock.
type tickMsg struct{}

// waitClient waits for the next message of the client.
func waitClient(ch <-chan Message) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return clientClosedMsg{}
		}
		return clientEventMsg{msg: msg}
	}
}

// init starts the view: it opens the link or the first session.
func (s *sessionView) init() tea.Cmd {
	if s.opening {
		open := s.open
		return func() tea.Msg {
			c, err := open(context.Background())
			return openedMsg{client: c, err: err}
		}
	}
	return tea.Batch(waitClient(s.events), s.newSessionCmd(false))
}

// update handles one message. It runs after the model's own handling.
func (s *sessionView) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.editor.SetWidth(msg.Width)
		s.maybeHeader()
	case tea.KeyPressMsg:
		cmd = s.onKey(msg)
	case tea.PasteMsg:
		cmd = s.onPaste(msg)
	case clientEventMsg:
		cmd = tea.Batch(s.onClient(msg.msg), waitClient(s.events))
	case openedMsg:
		cmd = s.onOpened(msg)
	case newSessionMsg:
		cmd = s.onNewSession(msg)
	case promptCalledMsg:
		cmd = s.onPromptCalled(msg)
	case queuedMsg:
		s.onQueued(msg)
	case dequeuedMsg:
		s.onDequeued(msg)
	case modelsMsg:
		s.onModels(msg)
	case setModelMsg:
		cmd = s.onSetModel(msg)
	case thinkingMsg:
		s.onThinking(msg)
	case sessionsMsg:
		s.onSessions(msg)
	case attachMsg:
		cmd = s.onAttach(msg)
	case takeMsg:
		cmd = s.onTake(msg)
	case refreshMsg:
		s.onRefresh(msg)
	case provListedMsg:
		s.onProvListed(msg)
	case loginStartedMsg:
		cmd = s.onLoginStarted(msg)
	case keySavedMsg:
		cmd = s.onKeySaved(msg)
	case loginEndedMsg:
		cmd = s.onLoginEnded(msg)
	case codeSentMsg:
		s.onCodeSent(msg)
	case cancelAnsweredMsg:
		cmd = s.onCancelAnswered(msg)
	case logoutDoneMsg:
		cmd = s.onLogoutDone(msg)
	case modelsFetchedMsg:
		cmd = s.onModelsFetched(msg)
	case cancelFailedMsg:
		s.onCancelFailed(msg)
	case tickMsg:
		s.onTick()
	}
	return tea.Batch(cmd, s.schedule())
}

// ---- opening and first session ----

func (s *sessionView) onOpened(msg openedMsg) tea.Cmd {
	if msg.err != nil || msg.client == nil {
		err := msg.err
		if err == nil {
			err = errors.New("no client")
		}
		s.startErr = err
		s.m.quitting = true
		return tea.Quit
	}
	s.adoptClient(msg.client)
	s.maybeHeader()
	return tea.Batch(waitClient(s.events), s.newSessionCmd(false))
}

// newSessionCmd creates a session. With detach set it leaves the current one
// first; the in-process client and a client without a session answer with an
// error that does not matter here.
func (s *sessionView) newSessionCmd(detach bool) tea.Cmd {
	c, cwd, gen := s.client, s.cwd, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		if detach {
			if err := c.Detach(ctx); err != nil && !ignorableDetach(err) {
				return newSessionMsg{gen: gen, err: fmt.Errorf("leave the session: %w", err)}
			}
		}
		info, err := c.NewSession(ctx, cwd)
		return newSessionMsg{gen: gen, info: info, err: err}
	}
}

func ignorableDetach(err error) bool {
	return errors.Is(err, ErrNeedsLeader) || errors.Is(err, ErrNoSession) || errors.Is(err, ErrSessionGone)
}

func (s *sessionView) onNewSession(msg newSessionMsg) tea.Cmd {
	if msg.gen != s.gen {
		return nil
	}
	if msg.err != nil {
		s.needNew = true
		s.settleStarts()
		s.appendBlock(NewErrorBlock(ErrorHeadline("session", sentence("Could not start a session: "+errText(msg.err))),
			"Your next prompt tries again."))
		return nil
	}
	s.useSession(msg.info)
	return s.refreshCmd(true)
}

// useSession makes a session the current one.
func (s *sessionView) useSession(info SessionInfo) {
	if info.ID != s.sess.ID {
		s.hadModel = false
	}
	if info.Cwd == "" {
		info.Cwd = s.sess.Cwd
	}
	s.sess = info
	s.needNew = false
	s.gen++
}

// ---- messages of the client ----

func (s *sessionView) onClient(msg Message) tea.Cmd {
	switch v := msg.(type) {
	case ConnectedMsg:
		if v.Info.InstanceID != s.info.InstanceID {
			// A new leader counts the providers generation from zero again.
			s.ownGen, s.deferred = 0, nil
			s.providersGen = 0
			if s.pbox != nil {
				s.pbox.gen = 0
			}
		}
		s.info = v.Info
		s.conn = connUp
		s.reconnect = reconnectView{}
		s.maybeHeader()
		return s.onBoxReconnected()
	case ProvidersChangedMsg:
		return s.onProvidersChanged(v)
	case DisconnectedMsg:
		s.conn = connDown
		s.reconnect = reconnectView{at: s.now()}
		s.gen++
		s.onBoxDisconnected()
	case ReconnectingMsg:
		s.conn = connDown
		s.reconnect.attempt, s.reconnect.wait, s.reconnect.at = v.Attempt, v.Wait, s.now()
	case SessionRestoredMsg:
		return s.onRestored(v)
	case SessionGoneMsg:
		s.onGone(v)
	case SnapshotMsg:
		return s.onSnapshot(v)
	case SessionEventMsg:
		return s.onSessionEvent(v)
	case PromptAcceptedMsg, PromptSendFailedMsg, PromptRunErrorMsg, PromptDoneMsg:
		s.onPromptMessage(v)
	case ClientErrorMsg:
		return s.onClientError(v)
	}
	return nil
}

func (s *sessionView) onClientError(v ClientErrorMsg) tea.Cmd {
	s.appendBlock(NewErrorBlock(ErrorHeadline("client", sentence(errText(v.Err))), ""))
	if v.Fatal {
		s.fatal = v.Err
		s.dropSecrets()
		s.m.quitting = true
		return tea.Quit
	}
	return nil
}

// maybeHeader adds the startup header once the size and the link are known.
func (s *sessionView) maybeHeader() {
	if s.headerShown || !s.m.sized || s.opening {
		return
	}
	s.headerShown = true
	where := "in-process agent"
	if s.info.Mode != ModeInProcess {
		where = "leader " + ShortID(s.info.InstanceID)
	}
	s.headerBlock = startupHeader{version: s.version, where: where, cwd: tildePath(s.cwd, s.home)}
	// The header stays open until the start line below it is known; the model
	// comes with the first state answer.
	s.m.applyBlock(AppendBlockMsg{ID: "header", Block: s.headerBlock, Stable: false})
	s.starts = append(s.starts, startSlot{id: "header", kind: startHeader})
	s.settleStartsIfKnown()
}

// ---- snapshot and events ----

// resetSessionState forgets the run, the queue and the usage of the session
// that the view followed before.
func (s *sessionView) resetSessionState() {
	s.run = runView{seq: s.run.seq + 1}
	s.queue = queueView{seq: s.queue.seq + 1}
	s.failure = nil
	s.ctxTokens, s.costMicro = 0, 0
}

// replaceTranscript drops the blocks and draws the history again. The model
// clears the screen and the scrollback and writes the new committed rows.
func (s *sessionView) replaceTranscript() {
	s.m.transcript = NewTranscript()
	s.m.rebuildWanted = true
	s.headerShown = false
	s.starts = nil
	s.maybeHeader()
}

func (s *sessionView) onSnapshot(v SnapshotMsg) tea.Cmd {
	var refresh bool
	s.baseline = v.Cursor
	if v.Head.Epoch == v.Cursor.Epoch && v.Head.Seq > v.Cursor.Seq {
		// A resumed follow replays what the client missed. Those events are in
		// the state that the view reads next, so they draw no change line.
		s.baseline = v.Head
	}
	switch {
	case v.SessionID != s.followed:
		s.hadModel = false
		if s.followed != "" {
			// The old run continues. Freeze only the content we stopped following.
			s.apply(s.trl.Detach())
			// A replacement session can publish its snapshot before the prompt
			// command returns. Keep that submission for its acceptance message.
			if s.sess.ID != v.SessionID && (!s.needNew || s.send == nil) {
				s.send, s.accepted = nil, nil
				s.promptID, s.promptSession = 0, ""
			}
			s.appendBlock(NewDividerBlock("session " + ShortID(v.SessionID)))
			s.addStartSlot(startDivider, "")
		}
		s.trl.Reset()
		s.resetSessionState()
		s.followed = v.SessionID
		// The reset forgets a run that started before the first snapshot; read
		// the state again so that the status shows it.
		refresh = true
	case v.Resync || (!v.Resumed && len(v.Entries) > 0):
		s.replaceTranscript()
		s.trl.Reset()
		s.resetSessionState()
		refresh = true
	default:
		return nil
	}
	s.apply(s.trl.Snapshot(v))
	if tokens := snapshotContext(v); tokens > 0 {
		s.ctxTokens = tokens
	}
	if refresh {
		return s.refreshCmd(false)
	}
	return nil
}

// snapshotContext is the context use of the last reply in a snapshot.
func snapshotContext(v SnapshotMsg) int64 {
	var tokens int64
	for _, e := range v.Entries {
		if e.Kind != "message" {
			continue
		}
		if am, ok := assistantOf(e.Message); ok {
			tokens = usageTokens(am.Usage)
		}
	}
	return tokens
}

// usageTokens is the number of tokens a reply used in the context window.
func usageTokens(u protocol.Usage) int64 {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.Input + u.Output + u.CacheRead + u.CacheWrite
}

func (s *sessionView) onSessionEvent(v SessionEventMsg) tea.Cmd {
	if s.followed == "" || v.SessionID != s.followed {
		return nil
	}
	s.apply(s.trl.Event(v.Event))
	var cmd tea.Cmd
	switch e := v.Event.(type) {
	case *protocol.AgentStart:
		s.run.running, s.run.started, s.run.cancelling, s.run.retry = true, s.now(), false, nil
		s.run.seq++
		if s.notice.ok {
			// A result line must not hide the status of the run that starts now.
			s.notice = noticeView{}
		}
	case *protocol.AutoRetryStart:
		s.run.retry = &retryView{
			attempt: e.Attempt, limit: e.MaxAttempts, reason: retryReason(e.ErrorMessage),
			due: s.now().Add(time.Duration(e.DelayMs) * time.Millisecond),
		}
	case *protocol.AutoRetryEnd, *protocol.AttemptStart:
		s.run.retry = nil
	case *protocol.MessageEnd:
		if am, ok := assistantOf(e.Message); ok {
			if tokens := usageTokens(am.Usage); tokens > 0 {
				s.ctxTokens = tokens
			}
		}
	case *protocol.QueueUpdate:
		s.onQueueUpdate(e)
	case *protocol.AgentSettled:
		s.endRun()
		cmd = s.refreshCmd(false)
	case *protocol.AgentDisposed:
		s.endRun()
	case *protocol.ModelUpdate:
		s.onModelUpdate(v, e)
	}
	return cmd
}

func (s *sessionView) endRun() {
	s.promptID, s.promptSession = 0, ""
	s.run.running, s.run.cancelling, s.run.retry = false, false, nil
	s.run.seq++
}

// retryReason shortens the error of a failed attempt for the status row: the
// text before the first parenthesis, for example "rate limit (HTTP 429)".
func retryReason(msg string) string {
	msg = retryCause(msg)
	if i := strings.Index(msg, " ("); i > 0 {
		msg = msg[:i]
	}
	return strings.TrimRight(msg, ". ")
}

// ---- prompt messages ----

func (s *sessionView) onPromptMessage(msg Message) {
	if s.send != nil && s.send.calling {
		s.send.early = append(s.send.early, msg)
		return
	}
	if done, ok := msg.(PromptDoneMsg); ok {
		if s.accepted != nil && done.ID == s.accepted.id {
			s.accepted = nil
		}
		if done.ID != s.promptID || s.promptSession != s.followed {
			return
		}
		// The followed lifecycle and resync state own run completion. A queued
		// run can already have started when this request's answer arrives.
		return
	}
	// A start event can belong to another client's run. The correlated refusal
	// is authoritative; retain the submission until its request has an answer.
	if failed, ok := msg.(PromptSendFailedMsg); ok && s.accepted != nil && failed.ID == s.accepted.id {
		if s.accepted.cleared && s.editor.Text() != s.accepted.sub.display {
			s.editor.RestoreQueued([]string{s.accepted.sub.Text})
		}
		s.accepted = nil
		s.promptID, s.promptSession = 0, ""
		s.failure = &failureView{text: s.failureText(failed.Err, failed.OutcomeUnknown), rev: s.editor.Revision()}
		return
	}
	if failed, ok := msg.(PromptRunErrorMsg); ok && s.accepted != nil && failed.ID == s.accepted.id {
		s.accepted = nil
	}
	if s.send == nil {
		return
	}
	switch v := msg.(type) {
	case PromptAcceptedMsg:
		if v.ID == s.send.id {
			s.send.cleared = s.editor.Accept(s.send.sub)
			s.accepted = s.send
			s.send, s.failure = nil, nil
		}
	case PromptSendFailedMsg:
		if v.ID == s.send.id {
			s.send = nil
			s.failure = &failureView{text: s.failureText(v.Err, v.OutcomeUnknown), rev: s.editor.Revision()}
		}
	case PromptRunErrorMsg:
		// The events already tell what failed; the draft is cleared at acceptance.
		if v.ID == s.send.id {
			s.send = nil
		}
	}
}

func (s *sessionView) onPromptCalled(msg promptCalledMsg) tea.Cmd {
	if s.send == nil || s.send.sub.Revision != msg.sub.Revision {
		return nil
	}
	var cmd tea.Cmd
	if msg.session != nil && msg.gen == s.gen {
		s.useSession(*msg.session)
		cmd = s.refreshCmd(true)
	}
	s.send.calling = false
	if msg.err != nil {
		s.send = nil
		if errors.Is(msg.err, ErrSessionGone) || errors.Is(msg.err, ErrNoSession) {
			s.needNew = true
		}
		s.failure = &failureView{text: s.failureText(msg.err, false), rev: s.editor.Revision()}
		return cmd
	}
	s.send.id = msg.id
	s.promptID, s.promptSession = msg.id, s.sess.ID
	early := s.send.early
	s.send.early = nil
	for _, m := range early {
		s.onPromptMessage(m)
	}
	return cmd
}

// failureText is the text of the "Not sent" row.
func (s *sessionView) failureText(err error, outcomeUnknown bool) string {
	var re *RequestError
	switch {
	case outcomeUnknown || errors.Is(err, ErrOutcomeUnknown):
		return "Not sent: link lost, outcome unknown · draft kept · nothing is resent"
	case errors.As(err, &re):
		kind := re.Kind
		if kind == "" {
			kind = fmt.Sprintf("code %d", re.Code)
		}
		if kind == string(protocol.ACPErrNotDriver) {
			return "Not sent: " + kind + " · draft kept · /take to drive"
		}
		if kind == string(protocol.ACPErrNoModel) {
			return s.noModelRefusal()
		}
		return "Not sent: " + kind + " · draft kept"
	case errors.Is(err, ErrDisconnected):
		return "Not sent: not connected · draft kept"
	case errors.Is(err, ErrPromptPending):
		return "Not sent: the previous run is still ending · draft kept"
	case errors.Is(err, ErrSessionGone), errors.Is(err, ErrNoSession):
		return "Not sent: the session is gone · draft kept · send again to start a new one"
	}
	return "Not sent: " + oneLine(err.Error()) + " · draft kept"
}

// noModelRefusal is the "Not sent" row of a prompt with no model.
func (s *sessionView) noModelRefusal() string {
	next := "Add a provider with /providers"
	if s.hadModel || len(s.models) > 0 {
		next = "Choose one with /model"
	}
	return "Not sent: no model · draft kept · " + next
}

// errText is the one-line text of an error.
func errText(err error) string {
	if err == nil {
		return ""
	}
	var re *RequestError
	if errors.As(err, &re) && re.Message != "" {
		return oneLine(re.Message)
	}
	return oneLine(err.Error())
}

// ---- queue ----

func (s *sessionView) onQueueUpdate(e *protocol.QueueUpdate) {
	s.queue.steer = append([]string(nil), e.Steering...)
	s.queue.follow = append([]string(nil), e.FollowUp...)
	s.queue.seq++
	// An input that left the queue has no use for its id any more.
	used := make([]bool, len(s.queue.known))
	for _, it := range s.queueItems() {
		if it.index >= 0 {
			used[it.index] = true
		}
	}
	kept := s.queue.known[:0]
	for i, k := range s.queue.known {
		if used[i] {
			kept = append(kept, k)
		}
	}
	s.queue.known = kept
}

// queueItem is one queued input with the id of the matching known input, or
// index -1 when this client does not know an id for it.
type queueItem struct {
	text   string
	follow bool
	index  int
}

// queueItems lists the queue in order, steering first, and matches each item
// with an input that this client queued. A queue update carries only texts, so
// the match is by kind and text.
func (s *sessionView) queueItems() []queueItem {
	used := make([]bool, len(s.queue.known))
	match := func(follow bool, text string) int {
		for i, k := range s.queue.known {
			if !used[i] && k.follow == follow && k.text == text {
				used[i] = true
				return i
			}
		}
		return -1
	}
	var items []queueItem
	for _, t := range s.queue.steer {
		items = append(items, queueItem{text: t, index: match(false, t)})
	}
	for _, t := range s.queue.follow {
		items = append(items, queueItem{text: t, follow: true, index: match(true, t)})
	}
	return items
}

func (s *sessionView) onQueued(msg queuedMsg) {
	if msg.gen != s.gen {
		return
	}
	if msg.err != nil {
		s.setNotice("Not queued: "+errText(msg.err)+" · draft kept", true)
		return
	}
	s.editor.Accept(msg.sub)
	if msg.gen == s.gen {
		s.queue.known = append(s.queue.known, knownInput{id: msg.id, follow: msg.follow, text: msg.sub.Text})
	}
}

// dequeueCmd removes the queued inputs that this client can name and gives
// their texts back, so that they join the draft.
func (s *sessionView) dequeueCmd() tea.Cmd {
	items := s.queueItems()
	if len(items) == 0 {
		return nil
	}
	type target struct{ id, text string }
	var targets []target
	unknown := 0
	for _, it := range items {
		if it.index < 0 {
			unknown++
			continue
		}
		targets = append(targets, target{id: s.queue.known[it.index].id, text: it.text})
	}
	c, gen := s.client, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		out := dequeuedMsg{gen: gen, taken: unknown}
		for _, t := range targets {
			removed, err := c.Remove(ctx, t.id)
			switch {
			case err != nil:
				out.err = err
				out.taken++
			case removed:
				out.restored = append(out.restored, t.text)
			default:
				out.taken++
			}
		}
		return out
	}
}

func (s *sessionView) onDequeued(msg dequeuedMsg) {
	if msg.gen != s.gen {
		return
	}
	s.editor.RestoreQueued(msg.restored)
	switch {
	case msg.err != nil:
		s.setNotice("Could not remove a queued input: "+errText(msg.err), true)
	case msg.taken > 0:
		s.setNotice("Some queued input stays: already taken by the agent or queued by another client", false)
	}
}

// ---- state refresh ----

// refreshCmd reads the state, the usage and, when asked, the model catalog.
func (s *sessionView) refreshCmd(withModels bool) tea.Cmd {
	c, gen := s.client, s.gen
	runSeq, queueSeq, modelSeq := s.run.seq, s.queue.seq, s.modelSeq
	var modelsToken uint64
	if withModels {
		s.fetchSeq++
		modelsToken = s.fetchSeq
	}
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		out := refreshMsg{gen: gen, runSeq: runSeq, queueSeq: queueSeq, modelSeq: modelSeq, modelsToken: modelsToken}
		if withModels {
			if res, err := c.Models(ctx); err == nil {
				out.models = &res
			}
		}
		if res, err := c.State(ctx); err == nil {
			out.state = &res
		}
		if res, err := c.Usage(ctx); err == nil {
			out.usage = &res
		}
		return out
	}
}

func (s *sessionView) onRefresh(msg refreshMsg) {
	if msg.gen != s.gen {
		return
	}
	if msg.models != nil && s.applyModelList(msg.modelsToken, *msg.models) {
		if msg.state == nil && msg.modelSeq == s.modelSeq {
			s.modelID = msg.models.Current
		}
	}
	if st := msg.state; st != nil {
		if msg.modelSeq == s.modelSeq {
			s.modelID = st.ModelID
		}
		s.thinking = st.ThinkingLevel
		s.fallback = st.ModelFallback
		s.haveState, s.stateGen = true, s.gen
		if msg.runSeq == s.run.seq && st.Running != s.run.running {
			s.run.running = st.Running
			if st.Running {
				s.run.started = s.now()
			} else {
				s.run.cancelling, s.run.retry = false, nil
			}
		}
		if msg.queueSeq == s.queue.seq {
			s.queue.steer = append([]string(nil), st.Steering...)
			s.queue.follow = append([]string(nil), st.FollowUp...)
		}
	}
	if msg.usage != nil {
		s.costMicro = msg.usage.Total.Cost.Total
	}
	if s.modelID != "" {
		s.hadModel = true
	}
	// The state of the session is known now, or the read failed; a start line
	// must not wait for ever.
	s.settleStarts()
}

// ---- notices and the animation clock ----

func (s *sessionView) setNotice(text string, isErr bool) { s.setNoticeFor(text, noticeFor, isErr) }

// setResult shows a result line with the success glyph for a short time.
func (s *sessionView) setResult(text string) {
	s.notice = noticeView{text: text, until: s.now().Add(noticeFor), ok: true}
}

// setNoticeFor shows a notice for a given time.
func (s *sessionView) setNoticeFor(text string, d time.Duration, isErr bool) {
	s.notice = noticeView{text: text, until: s.now().Add(d), err: isErr}
}

func (s *sessionView) noticeActive() bool {
	return s.notice.text != "" && s.now().Before(s.notice.until)
}

// needsTick tells whether something on screen changes with time.
func (s *sessionView) needsTick() bool {
	return s.run.running || s.send != nil || s.run.retry != nil || s.conn != connUp ||
		s.noticeActive() || s.now().Before(s.reconnect.hintUntil) || s.dialogTicks() || s.now().Before(s.copiedUntil)
}

// schedule arms the animation clock when it is needed and not armed yet.
func (s *sessionView) schedule() tea.Cmd {
	if s.tick < 0 || s.tickPending || !s.needsTick() {
		return nil
	}
	s.tickPending = true
	return tea.Tick(s.tick, func(time.Time) tea.Msg { return tickMsg{} })
}

func (s *sessionView) onTick() {
	s.tickPending = false
	s.trl.View().SetSpinnerFrame(int(s.now().UnixMilli() / int64(defaultTick/time.Millisecond)))
	if s.notice.text != "" && !s.now().Before(s.notice.until) {
		s.notice = noticeView{}
	}
}
