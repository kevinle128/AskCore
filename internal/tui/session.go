package tui

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Session view.
//
// The session view holds the client side of one agent session: the link state,
// the run state, the queue projection, the model and usage numbers, and the
// local request state of a prompt that is not accepted yet. The agent stays the
// authority for execution; this file keeps only a projection and the facts the
// client itself knows.
//
// Only the root update loop touches this state. Calls to the Client run in
// commands and come back as messages that carry the generation they started in.
// A result from an older generation changes nothing.

const (
	// noticeFor is how long a short notice stays in the status row.
	noticeFor = 3 * time.Second
	// defaultTick is the step of the animation clock.
	defaultTick = 80 * time.Millisecond
	// callTimeout bounds a client call that a command makes.
	callTimeout = 30 * time.Second
)

// connState is the state of the link as the view knows it.
type connState int

const (
	// connStarting means no client exists yet.
	connStarting connState = iota
	connUp
	connDown
)

// runView is the projection of the run of the followed session.
type runView struct {
	running    bool
	started    time.Time
	cancelling bool
	retry      *retryView
	// seq grows at agent_start and agent_settled. A state answer applies the
	// run flag only if no such event arrived since the call started.
	seq uint64
}

type retryView struct {
	attempt, limit int
	reason         string
	due            time.Time
}

// sendState is a prompt that the leader has not accepted yet.
type sendState struct {
	text    string
	cleared bool
	sub     Submit
	id      PromptID
	// calling is true until the Prompt call has returned its id.
	calling bool
	// early holds messages about the prompt that arrived before its id was
	// known, because the call and the event queue are separate goroutines.
	early []Message
}

// failureView is the "Not sent" row. It stays while the draft stays as it was.
type failureView struct {
	text string
	rev  uint64
}

// knownInput is an input that this client queued, with the id the agent gave.
// A queue update carries only texts, so ids come from here.
type knownInput struct {
	id     string
	follow bool
	text   string
}

type queueView struct {
	steer, follow []string
	known         []knownInput
	// seq grows with every queue update, so that a state answer does not
	// replace a newer queue.
	seq uint64
}

type noticeView struct {
	text  string
	until time.Time
	err   bool
	// ok puts the success glyph before the text: a result line.
	ok bool
	// cycle is the result of a model cycle press; it replaces text.
	cycle *cycleNotice
}

func (n noticeView) shown() bool { return n.text != "" || n.cycle != nil }

type reconnectView struct {
	attempt int
	wait    time.Duration
	at      time.Time
	// hintUntil is when the "Reconnecting, please wait…" hint stops.
	hintUntil time.Time
}

type sessionView struct {
	m      *Model
	client Client
	events <-chan Message
	open   func(ctx context.Context) (Client, error)

	editor *Editor
	trl    *Translator

	cwd  string
	home string
	now  func() time.Time
	tick time.Duration

	conn      connState
	info      ConnInfo
	reconnect reconnectView

	// sess is the session as the commands know it. followed is the session
	// whose snapshot was applied last; events of other sessions are ignored.
	sess     SessionInfo
	followed string
	// needNew means the session is gone; the next prompt creates a new one.
	needNew bool

	gen uint64

	run           runView
	send          *sendState
	accepted      *sendState
	promptID      PromptID
	promptSession string
	failure       *failureView
	queue         queueView
	notice        noticeView
	// clip is the machine of /copy. copySeq numbers its requests.
	clip    ClipboardOptions
	copySeq uint64

	models  []protocol.ACPModel
	modelID string
	// hadModel keeps the next step after a model was removed from this session.
	hadModel bool
	// haveState is true once a state answer of the current session was applied;
	// stateGen is the generation that answer belonged to. Until then an empty
	// modelID does not mean that the session has no model.
	haveState bool
	stateGen  uint64
	// modelSeq grows with every model event, so a state answer that started
	// before one does not set the model back.
	modelSeq uint64
	// fallback is the saved default that the session did not start on.
	fallback *protocol.ACPModelFallback
	// levelDropped is the thinking level that the start model did not use.
	levelDropped *protocol.ACPLevelDropped
	// baseline is the cursor of the last follow cut. A model event at or below
	// it is already part of the state.
	baseline protocol.ACPCursor
	// starts are the block ids of the fallback and dropped-level notices that
	// wait for the state.
	starts    [][2]string
	trace     string
	thinking  protocol.ThinkingLevel
	thinkTry  protocol.ThinkingLevel
	ctxTokens int64
	// modelReq numbers the set-model requests. Only the answer to the latest
	// request, in the generation it started in, changes the model.
	modelReq uint64

	sel          *selector
	openingModel bool
	// cycling is true while the list read or the model change of a cycle press
	// waits. cycleFetch and cycleReq are the token and the request of that press.
	cycling    bool
	cycleFetch uint64
	cycleReq   uint64
	// shownProblems is the last set of settings problems that the pane showed.
	shownProblems []protocol.ACPDiagnostic

	// pbox is the provider box and dlg the login or removal box. At most one of
	// them is open; a flow replaces the provider box with its own box.
	pbox         *providerBox
	dlg          *loginDialog
	dlgSeq       uint64
	provReq      uint64
	providersGen uint64
	// closed holds sign-ins whose box is gone and whose end is not known yet.
	closed map[string]*closedLogin
	// afterModels runs when the model list that a token asked for arrives.
	afterModels  map[uint64]func(res *protocol.ACPModelsResult) tea.Cmd
	fetchSeq     uint64
	appliedFetch uint64
	modelList    protocol.ACPModelsResult
	modelListGen uint64
	// ownGen is the latest providers generation that this client caused. A
	// change notice at or below it is not shown. deferred holds notices that
	// arrived while a change of this client was still open.
	ownGen   uint64
	deferred []ProvidersChangedMsg

	tickPending bool
	// startShown is true once the start notices of the transcript were added.
	startShown bool
	blockSeq   int

	opening  bool
	startErr error
	fatal    error

	// starting is true from the start of a session start until the answer of
	// its session/new: the trust check and the trust box come before the
	// session. A prompt is not sent while it is true.
	starting bool
	// session holds the options of the command line. They go with every
	// session/new. A trust override skips the trust check.
	session NewSessionOptions
	// start is the record of the last session start of this client.
	start trustStart
	// tbox is the open trust box, or nil.
	tbox *trustBox
	// trustReq numbers the trust requests; only the answer to the latest one
	// applies. trustReading is true while /trust waits for the trust state.
	trustReq     uint64
	trustReading bool
	// preWarn and preExplain are the blocks of the trust warning and of the
	// explanation of the start that is open. trustSlots are the placeholders
	// of the trust note and the settings problems that wait for the state.
	preWarn, preExplain string
	trustSlots          [][2]string
	// sessTrust and diagnostics are the trust and the settings problems of the
	// state of stateSession. foldApplied is the session whose thinkingFold the
	// view applied.
	sessTrust    *protocol.ACPSessionTrust
	diagnostics  []protocol.ACPDiagnostic
	stateSession string
	foldApplied  string

	// quitUntil ends the confirmation window of the first ctrl+c. quitNote is
	// set when the user asked to quit.
	quitUntil time.Time
	quitNote  *quitNote
	// copiedUntil is the end of the "Copied" line. It shows above the wait line
	// of a login, because the copy happens while the box waits.
	copiedUntil time.Time
}

// newSessionView builds the view of a Model.
func newSessionView(m *Model, o Options) *sessionView {
	s := &sessionView{
		m:      m,
		client: o.Client,
		open:   o.Open,
		editor: NewEditor(DefaultCommands()),
		trace:  o.TraceFile,
		cwd:    o.Cwd,
		home:   o.Home,
		now:    o.Now,
		tick:   o.TickInterval,
		clip:   o.Clipboard.resolved(),
		// init and onOpened start the first session with startSessionCmd,
		// which sets starting.
		session: o.Session,
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.tick == 0 {
		s.tick = defaultTick
	}
	if s.home == "" {
		s.home, _ = os.UserHomeDir()
	}
	s.closed = map[string]*closedLogin{}
	s.afterModels = map[uint64]func(res *protocol.ACPModelsResult) tea.Cmd{}
	s.trl = NewTranslator(TranslatorOptions{Location: o.Location})
	if s.client != nil {
		s.adoptClient(s.client)
	} else {
		s.opening = true
	}
	return s
}

// adoptClient makes a client the link of the view.
func (s *sessionView) adoptClient(c Client) {
	s.client = c
	s.events = c.Events()
	s.info = c.Info()
	s.conn = connUp
	s.opening = false
}

// nextBlockID returns a new id for a block that the view adds itself.
func (s *sessionView) nextBlockID() string {
	s.blockSeq++
	return "v" + strconv.Itoa(s.blockSeq)
}

// appendBlock adds a final block to the transcript.
func (s *sessionView) appendBlock(b Block) {
	s.m.applyBlock(AppendBlockMsg{ID: s.nextBlockID(), Block: b, Stable: true})
}

// apply applies the block messages of the translator.
func (s *sessionView) apply(msgs []tea.Msg) {
	for _, msg := range msgs {
		s.m.applyBlock(msg)
	}
}

// callContext returns a context for one client call.
func callContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), callTimeout)
}

// ---- display helpers ----

// ShortID shortens a session or leader id for display. The ids of the leader
// are long random hex strings; four characters tell them apart on one machine.
func ShortID(id string) string {
	id = strings.TrimPrefix(id, "sess_")
	if r := []rune(id); len(r) > 8 {
		return string(r[:4])
	}
	return id
}

// tildePath shortens the home directory to "~".
func tildePath(path, home string) string {
	if home != "" && home != "/" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			return "~" + path[len(home):]
		}
	}
	return path
}

// modelRow finds the catalog row of a model id.
func (s *sessionView) modelRow(id string) (protocol.ACPModel, bool) {
	for _, row := range s.models {
		if row.ModelID == id {
			return row, true
		}
	}
	return protocol.ACPModel{}, false
}

func (s *sessionView) reasoning() bool {
	row, ok := s.modelRow(s.modelID)
	return ok && row.Reasoning
}

func (s *sessionView) contextWindow() int64 {
	row, _ := s.modelRow(s.modelID)
	return int64(row.ContextWindow)
}

// isObserver reports whether another client drives the session.
func (s *sessionView) isObserver() bool { return s.sess.Role == protocol.ACPRoleObserver }

// ---- screen parts ----

// modalHeight is the number of rows that a selector or box may use: the prompt
// pane slot, as layout gives it once the status row has its row. A box that
// is told more rows than layout gives would lose its last row, the hint.
func (s *sessionView) modalHeight(status string) int {
	return max(1, allocate(s.m.height, demand{prompt: promptSlotRows(s.m.height), status: statusRowCount(status)}).prompt)
}

// Slots implements SlotSource. The root model calls it from View, so it reads
// state and never waits.
func (s *sessionView) Slots(width int) Slots {
	th := s.m.theme
	out := Slots{
		Header:  s.headerText(width),
		Footer:  s.footerText(width),
		Status:  s.statusText(width),
		Bar:     s.barRow(width),
		Pending: s.pendingRows(width),
	}
	switch {
	case s.tbox != nil:
		out.Editor = s.tbox.slot(width, s.modalHeight(out.Status), th)
		return out
	case s.dlg != nil:
		out.Editor = s.dlg.slot(s, width, s.modalHeight(out.Status))
		return out
	case s.pbox != nil:
		out.Editor = s.pbox.slot(width, s.modalHeight(out.Status), th)
		return out
	case s.sel != nil:
		out.Editor = s.sel.slot(width, s.modalHeight(out.Status), th)
		return out
	}
	out.Editor = s.editorSlot(width)
	if s.listFocused() {
		// The keys go to the block list, so the prompt shows no cursor.
		out.Editor.Cursor = nil
	}
	out.Menu = s.editor.MenuRows(width, th)
	return out
}

// editorSlot is the editor, or the watching text while another client drives
// and the draft is empty. The draft stays editable so that /take can be typed.
func (s *sessionView) editorSlot(width int) EditorSlot {
	th := s.m.theme
	if s.isObserver() && s.editor.Text() == "" {
		textW := max(1, PromptInnerWidth(width))
		text := "Watching. Type /take to take control when the run ends."
		if width < shortWidth {
			text = "Watching. Type /take"
		}
		return EditorSlot{
			Rows:   []string{th.Render(RoleWarning, GlyphObserver) + " " + truncateTo(text, max(1, textW-2))},
			Cursor: &CursorPos{Row: 0, Col: promptWidth},
		}
	}
	return s.editor.Slot(width, th)
}
