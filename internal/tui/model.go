package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// Root model composition.
//
// The Model is the single Bubble Tea model of the TUI. Only its Update method
// changes retained state. View builds the frame from that state without I/O.
// Components plug in through the SlotSource, and session events reach the
// transcript as block messages (see messages.go).

// Options configure a Model.
type Options struct {
	// Slots supplies the editor, status, pending, menu and footer rows. When it
	// is nil and a Client or Open is set, the session view supplies them. When
	// all are nil the live region is empty.
	Slots SlotSource
	// Profile is the color profile before the terminal reports its own.
	Profile colorprofile.Profile
	// SettleDelay is how long a size must stay the same before the
	// scrollback is rebuilt. Zero means 120 ms.
	SettleDelay time.Duration

	// Client is the open link to an agent. Init creates the first session on it.
	// Leave it nil when Open is set. The caller closes the client after the
	// program has stopped.
	Client Client
	// Open starts the link while the program already shows "Starting leader…".
	// It runs in a command, never in the update loop. An error ends the program;
	// StartError returns it. The caller closes the client that Open returned.
	Open func(ctx context.Context) (Client, error)
	// Cwd is the working directory of the session.
	Cwd string
	// Home is the home directory that paths shorten to "~". Empty means the
	// home directory of the current user.
	Home string
	// Version is the build version in the startup header.
	Version string
	// TraceFile is the file that Bubble Tea writes raw input to, or empty. The
	// warning names it. The package does not read the environment.
	TraceFile string
	// Location is the time zone of the send times. Nil means local time.
	Location *time.Location
	// Now is the clock of the session view. Nil means time.Now.
	Now func() time.Time
	// TickInterval is the step of the animation clock that runs only while a
	// status, a countdown or a notice needs it. Zero means 80 ms; a negative
	// value turns the clock off.
	TickInterval time.Duration
}

// Model is the root model of the inline TUI.
type Model struct {
	slots       SlotSource
	theme       *Theme
	transcript  *Transcript
	focus       Focus
	settleDelay time.Duration

	// Terminal size. generation is the generation of the latest size event;
	// the renderer accepts a rebuild only for that generation.
	width, height int
	generation    uint64
	sized         bool
	settleGen     uint64 // generation that the pending settle wait belongs to
	settleWaiting bool

	// Output state, see render_inline.go.
	committing    bool
	replaying     bool
	rebuildWanted bool
	outbox        []string // committed rows that are not written yet

	// frameReady is false until the first frame had time to reach the screen.
	// A commit before that scrolls against a screen the renderer has not drawn
	// on yet and leaves the live region at the top.
	frameReady bool

	sv  *sessionView
	err error

	// quitting is true once the program is about to end. The final frame is
	// empty then, so the live region does not stay on the screen.
	quitting bool
}

var _ tea.Model = (*Model)(nil)

// New returns a Model.
func New(o Options) *Model {
	delay := o.SettleDelay
	if delay <= 0 {
		delay = defaultSettleDelay
	}
	m := &Model{
		slots:       o.Slots,
		theme:       NewTheme(o.Profile),
		transcript:  NewTranscript(),
		settleDelay: delay,
		frameReady:  true,
	}
	if o.Client != nil || o.Open != nil {
		m.sv = newSessionView(m, o)
		m.frameReady = false
		if m.slots == nil {
			m.slots = m.sv
		}
	}
	return m
}

// Transcript returns the transcript. Change it only from Update.
func (m *Model) Transcript() *Transcript { return m.transcript }

// Theme returns the current theme.
func (m *Model) Theme() *Theme { return m.theme }

// Size returns the terminal size that the model last saw.
func (m *Model) Size() (width, height int) { return m.width, m.height }

// Focus returns the component that has input focus.
func (m *Model) Focus() Focus { return m.focus }

// SetFocus moves input focus. Call it from Update.
func (m *Model) SetFocus(f Focus) { m.focus = f }

// Err returns the last error of a transcript change or a scrollback rebuild.
// A later component shows it in the status row; it is never dropped.
func (m *Model) Err() error { return m.err }

func (m *Model) setErr(err error) {
	if err != nil {
		m.err = err
	}
}

// StartError returns the error of Options.Open, or nil.
func (m *Model) StartError() error {
	if m.sv == nil {
		return nil
	}
	return m.sv.startErr
}

// ClientError returns the fatal error that a client reported, or nil. The
// program ends when the client reports one.
func (m *Model) ClientError() error {
	if m.sv == nil {
		return nil
	}
	return m.sv.fatal
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	if m.sv == nil {
		return nil
	}
	return m.sv.init()
}

// applyBlock applies one block message to the transcript.
func (m *Model) applyBlock(msg tea.Msg) {
	switch msg := msg.(type) {
	case AppendBlockMsg:
		m.setErr(m.transcript.Append(msg.ID, msg.Block, msg.Stable))
	case ReplaceBlockMsg:
		m.setErr(m.transcript.Replace(msg.ID, msg.Block))
	case FinishBlockMsg:
		m.setErr(m.transcript.Finish(msg.ID))
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd = m.onWindowSize(msg)
	case settleMsg:
		m.onSettle(msg)
	case tea.InlineReplayResult:
		m.onReplayResult(msg)
	case commitDoneMsg:
		m.committing = false
	case tea.ColorProfileMsg:
		m.theme = m.theme.WithProfile(msg.Profile)
	case RebuildMsg:
		m.rebuildWanted = true
	case AppendBlockMsg, ReplaceBlockMsg, FinishBlockMsg:
		m.applyBlock(msg)
	case firstFrameMsg:
		m.frameReady = true
	case StopMsg:
		m.quitting = true
		if m.sv != nil {
			// An open login is cancelled before the program ends. The buffers
			// of a key or a code are dropped.
			if cancel := m.sv.stopCmd(); cancel != nil {
				return m, tea.Sequence(cancel, tea.Quit)
			}
		}
		return m, tea.Quit
	}
	var viewCmd tea.Cmd
	if m.sv != nil {
		viewCmd = m.sv.update(msg)
	}
	return m, tea.Batch(cmd, viewCmd, m.pump())
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	if !m.sized || m.quitting {
		return tea.NewView("")
	}
	frame := m.frame()
	v := tea.NewView(strings.Join(frame.Rows, "\n"))
	if frame.Cursor != nil {
		v.Cursor = tea.NewCursor(frame.Cursor.Col, frame.Cursor.Row)
	}
	return v
}

// frame lays out the live region for the current state.
func (m *Model) frame() Frame {
	var slots Slots
	if m.slots != nil {
		slots = m.slots.Slots(m.width)
	}
	var tail []string
	if m.frameReady {
		// Until the first frame is on the screen the tail stays empty: rows that
		// are about to be committed would make the first frame taller than the
		// frames after the commit.
		tail = m.transcript.TailLines(m.renderContext())
	}
	if m.sv != nil && len(tail) > 0 {
		// A live reply is set off from the status below it by an empty row, as
		// the sketches show. The row is the last row of the tail.
		tail = append(tail, "")
	}
	return composeFrame(slots, tail, m.width, m.height, m.theme)
}
