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
// transcript as block messages (see messages.go). The block pane draws the
// transcript; the model gives it the rows that the layout leaves for it.

// Options configure a Model.
type Options struct {
	// Slots supplies the header, editor, status, pending, menu and bar rows. When
	// it is nil and a Client or Open is set, the session view supplies them.
	// When all are nil only the block pane shows.
	Slots SlotSource
	// Profile is the color profile before the terminal reports its own.
	Profile colorprofile.Profile

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
	// Version is the build version. The header row does not show it.
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
	// Clipboard holds the machine parts of /copy. Zero fields use the real
	// machine.
	Clipboard ClipboardOptions
}

// Model is the root model of the TUI.
type Model struct {
	slots      SlotSource
	theme      *Theme
	transcript *Transcript
	pane       *Pane
	focus      Focus

	// Terminal size, known after the first size message.
	width, height int
	sized         bool

	// list is the selection of the block pane, see block_list.go.
	list blockList
	// viewer is the one-block viewer, see transcript_view.go.
	viewer viewerState
	// mouse is the state of the pointer, see mouse.go.
	mouse mouseState

	// geo is where the parts of the last frame were. A mouse position is mapped
	// with it, so a click lands on what the user saw.
	geo FrameGeo
	// markerApprox is true when the last frame counted the new lines of the
	// bottom marker with some stale row counts. settlePending is true while a
	// settle tick is on its way.
	markerApprox  bool
	settlePending bool

	sv  *sessionView
	err error

	// quitting is true once the program is about to end. The final frame is
	// empty then and leaves the alternate screen.
	quitting bool
	// resized keeps the selected block visible when the pane gets its new size.
	resized bool
}

var _ tea.Model = (*Model)(nil)

// New returns a Model.
func New(o Options) *Model {
	m := &Model{
		slots: o.Slots,
		theme: NewTheme(o.Profile),
	}
	m.resetTranscript()
	if o.Client != nil || o.Open != nil {
		m.sv = newSessionView(m, o)
		if m.slots == nil {
			m.slots = m.sv
		}
	}
	return m
}

// resetTranscript starts an empty transcript. The pane belongs to one
// transcript, so both change together; the pane follows the end again. The
// selection, the viewer and the block list focus belong to the old blocks and
// end with them.
func (m *Model) resetTranscript() {
	m.transcript = NewTranscript()
	m.list = blockList{}
	m.mouse = mouseState{}
	m.closeViewer()
	if m.focus == FocusBlocks {
		m.focus = FocusEditor
	}
	// A block that has no fold mode of its own draws the same rows in every
	// mode, so the default shows each block as it draws itself.
	m.pane = NewPane(m.transcript)
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

// Err returns the last error of a transcript change. A later component shows
// it in the status row; it is never dropped.
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
	var settle tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.onWindowSize(msg)
		settle = m.requestSettle()
	case tea.ColorProfileMsg:
		m.theme = m.theme.WithProfile(msg.Profile)
	case AppendBlockMsg, ReplaceBlockMsg, FinishBlockMsg:
		m.applyBlock(msg)
		settle = m.requestSettle()
	case markerSettleMsg:
		// The frame that followed the last message counted more of the blocks
		// below the window. Another tick follows while some count is stale.
		m.settlePending = false
		if m.markerApprox {
			settle = m.requestSettle()
		}
	case StopMsg:
		m.quitting = true
		m.closeViewer()
		if m.sv != nil {
			// An open login is cancelled before the program ends. The buffers
			// of a key or a code are dropped.
			if cancel := m.sv.stopCmd(); cancel != nil {
				return m, tea.Sequence(cancel, tea.Quit)
			}
		}
		return m, tea.Quit
	}
	var cmd tea.Cmd
	if m.sv != nil {
		cmd = m.sv.update(msg)
	}
	if _, isTick := msg.(markerSettleMsg); !isTick && settle == nil {
		// A key or a mouse message can change the row counts below the window,
		// as a fold command does. A settle tick follows every other message
		// while the marker shows a count. The tick of the settle itself must not
		// ask for one more, or the chain would never end.
		settle = m.requestSettle()
	}
	if cmd == nil {
		return m, settle
	}
	return m, tea.Batch(settle, cmd)
}

// markerSettleMsg asks for another frame while the count of the bottom marker
// still uses row counts from before a resize or a change of content.
type markerSettleMsg struct{}

// settleInterval is the time between the frames that settle the marker count.
const settleInterval = 16 * time.Millisecond

// requestSettle returns the tick that draws one more frame, when the bottom
// marker shows a count of new lines and no tick is on its way. A frame settles
// only as many blocks as its time budget allows, and the renderer draws only
// after a message, so the count would stay stale without the tick.
func (m *Model) requestSettle() tea.Cmd {
	if m.settlePending || !m.pane.NewOutput() {
		return nil
	}
	m.settlePending = true
	return tea.Tick(settleInterval, func(time.Time) tea.Msg { return markerSettleMsg{} })
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	if m.quitting {
		// An empty frame on the main screen: the renderer leaves the alternate
		// screen on it, so the terminal is restored when the program ends.
		return tea.NewView("")
	}
	if !m.sized {
		v := tea.NewView("")
		v.AltScreen = true
		return v
	}
	if m.viewer.open {
		return m.viewerFrame()
	}
	frame := m.frame()
	v := tea.NewView(strings.Join(frame.Rows, "\n"))
	v.AltScreen = true
	// Wheel, click, drag and motion reach Update. A message that no part
	// handles is ignored.
	v.MouseMode = tea.MouseModeAllMotion
	if frame.Cursor != nil {
		v.Cursor = tea.NewCursor(frame.Cursor.Col, frame.Cursor.Row)
	}
	return v
}

// renderContext returns the context for blocks at the current size.
func (m *Model) renderContext() RenderContext {
	return RenderContext{Width: m.width, Theme: m.theme}
}

// frame lays out the screen for the current state. The block pane keeps a
// cache of the rows that it drew; drawing it here is a change of that cache
// only, and the update loop is the only caller.
func (m *Model) frame() Frame {
	var slots Slots
	if m.slots != nil {
		slots = m.slots.Slots(m.width)
	}
	m.syncMarks()
	pane := func(rows int) []string {
		m.pane.SetSize(m.width, rows)
		m.pane.SetTheme(m.theme)
		if m.resized && !m.pane.Following() {
			m.revealSelected()
			if m.pane.Following() {
				m.pane.ai, m.pane.ar = m.pane.anchorFromEnd(m.pane.viewRows(false))
			}
			m.pane.follow, m.pane.keepScroll = false, true
		}
		m.resized = false
		f := m.pane.Frame()
		m.markerApprox = f.Marker.Show && f.Marker.Approx
		m.shadePane(f.Rows)
		if f.Marker.Show && len(f.Rows) >= 2 {
			f.Rows[len(f.Rows)-1] = m.markerRow(f.Marker)
		}
		return f.Rows
	}
	frame := composeFrame(slots, pane, m.width, m.height, m.theme)
	m.geo = frame.Geo
	return frame
}

// markerRow draws the bottom marker: bold, no background, centered by the
// pane. The count of new lines is the accent; "▼ End" is the tool role.
func (m *Model) markerRow(mk PaneMarker) string {
	role := RoleTool
	if mk.New {
		role = RoleAccent
	}
	return strings.Repeat(" ", mk.Pad) + m.theme.Style(role).Bold(true).Render(mk.Text)
}
