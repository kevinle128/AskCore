package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The header row holds the logo and spaces, in every link state and at every
// width. The link shows in the status row and the notices, never here.
func TestHeaderRowHoldsTheLogoOnly(t *testing.T) {
	states := map[string]func(s *sessionView){
		"connected": func(s *sessionView) {},
		"link lost": func(s *sessionView) { s.conn = connDown },
		"in process": func(s *sessionView) {
			s.info = ConnInfo{Mode: ModeInProcess}
		},
		"observer": func(s *sessionView) {
			s.sess = SessionInfo{ID: "sess_3f9a00112233", Role: protocol.ACPRoleObserver}
		},
		"link not up": func(s *sessionView) { s.opening = true },
		"no model":    func(s *sessionView) { s.modelID, s.haveState = "", true },
	}
	for name, set := range states {
		for _, w := range []int{40, 80, 120} {
			m := footerModel(t, colorprofile.NoTTY)
			set(m.sv)
			got := ansi.Strip(m.sv.headerText(w))
			require.Equal(t, w, displayWidth(got), "%s at %d columns: the row fills the width", name, w)
			require.Equal(t, " ◢◤ ask", strings.TrimRight(got, " "), "%s at %d columns", name, w)
		}
	}
}

// The logo keeps its gradient in a truecolor terminal.
func TestHeaderLogoUsesTheLogoRole(t *testing.T) {
	m := footerModel(t, colorprofile.TrueColor)
	row := m.sv.headerText(80)
	require.Contains(t, row, m.theme.RenderLogo(logoText))
	require.Equal(t, 80, displayWidth(row))
}
