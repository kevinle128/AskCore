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
	text string
	sub  Submit
	id   PromptID
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
}

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

	version string
	cwd     string
	home    string
	now     func() time.Time
	tick    time.Duration

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

	run     runView
	send    *sendState
	failure *failureView
	queue   queueView
	notice  noticeView

	models    []protocol.ACPModel
	modelID   string
	thinking  protocol.ThinkingLevel
	thinkTry  protocol.ThinkingLevel
	ctxTokens int64
	costMicro int64
	// modelReq numbers the set-model requests. Only the answer to the latest
	// request, in the generation it started in, changes the model.
	modelReq uint64

	sel          *selector
	openingModel bool

	tickPending bool
	headerShown bool
	blockSeq    int

	opening  bool
	startErr error
	fatal    error

	// quitUntil ends the confirmation window of the first ctrl+c. quitNote is
	// set when the user asked to quit.
	quitUntil time.Time
	quitNote  *quitNote
}

// newSessionView builds the view of a Model.
func newSessionView(m *Model, o Options) *sessionView {
	s := &sessionView{
		m:       m,
		client:  o.Client,
		open:    o.Open,
		editor:  NewEditor(DefaultCommands()),
		version: o.Version,
		cwd:     o.Cwd,
		home:    o.Home,
		now:     o.Now,
		tick:    o.TickInterval,
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

// modelName is the short name of the current model.
func (s *sessionView) modelName() string {
	if s.modelID == "" {
		return ""
	}
	if row, ok := s.modelRow(s.modelID); ok && row.ID != "" {
		return row.ID
	}
	name := s.modelID
	if i := strings.LastIndex(name, "@"); i >= 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
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

// ---- live region ----

// Slots implements SlotSource. The root model calls it from View, so it reads
// state and never waits.
func (s *sessionView) Slots(width int) Slots {
	th := s.m.theme
	out := Slots{
		Status:  s.statusText(width),
		Footer:  s.footerText(width),
		Pending: s.pendingRows(width),
	}
	if s.sel != nil {
		out.Editor = s.sel.slot(width, s.m.height, th)
		return out
	}
	out.Editor = s.editorSlot(width)
	out.Menu = s.editor.MenuRows(width, th)
	return out
}

// editorSlot is the editor, or the watching text while another client drives
// and the draft is empty. The draft stays editable so that /take can be typed.
func (s *sessionView) editorSlot(width int) EditorSlot {
	th := s.m.theme
	if s.isObserver() && s.editor.Text() == "" {
		textW := max(1, BoxInnerWidth(width))
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
