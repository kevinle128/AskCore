package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Model cycling.
//
// ctrl+p changes the session model to the next entry of the cycle list and
// ctrl+shift+p to the previous one. The cycle list is the scope of the session
// (--models or enabledModels) when the host sends one, else every row of the
// model list in the order of /model. A press changes only the session model;
// the saved default stays as it was. The list is the last model list of the
// current session; a press reads it first when the view has none.
//
// One press acts at a time: while the list read or the model change of a press
// waits for its answer, a press is ignored.

const (
	noOtherModelText  = "No other model to cycle to."
	modelProblemsHead = "Problems in models.json, read again when /model opened"
)

// cycleEntry is one entry of the cycle list. An empty level means that the
// entry states no level, so the session keeps its level.
type cycleEntry struct {
	id    string
	level protocol.ThinkingLevel
}

// cycleNotice is the result of a press on the status row. It keeps its parts,
// so that a resize draws it in the form of the new width.
type cycleNotice struct {
	ref        modelRef
	level      string
	pos, total int
	// dropped is the text of a level that the entry states and the model does
	// not use, or "".
	dropped string
}

type cycleModelsMsg struct {
	gen, token uint64
	back       bool
	res        protocol.ACPModelsResult
	err        error
}

type cycleSetMsg struct {
	gen, req   uint64
	entry      cycleEntry
	pos, total int
	res        protocol.ACPSetModelResult
	err        error
}

// modelCycleKey tells whether a key is a cycle key and its direction. A
// terminal with the kitty keyboard protocol can send the shifted key as "P".
func modelCycleKey(k tea.KeyPressMsg) (back, ok bool) {
	if k.Mod&tea.ModCtrl == 0 || k.Mod&(tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) != 0 {
		return false, false
	}
	switch k.Code {
	case 'p':
		return k.Mod&tea.ModShift != 0, true
	case 'P':
		return true, true
	}
	return false, false
}

func (s *sessionView) cycleModel(back bool) tea.Cmd {
	if s.cycling || !s.idleFor(modelBusyText) {
		return nil
	}
	if s.appliedFetch == 0 || s.modelListGen != s.gen {
		s.cycling = true
		s.fetchSeq++
		s.cycleFetch = s.fetchSeq
		c, gen, token := s.client, s.gen, s.fetchSeq
		return func() tea.Msg {
			ctx, cancel := callContext()
			defer cancel()
			res, err := c.Models(ctx)
			return cycleModelsMsg{gen: gen, token: token, back: back, res: res, err: err}
		}
	}
	return s.cycleStep(back)
}

func (s *sessionView) onCycleModels(msg cycleModelsMsg) tea.Cmd {
	if msg.token == s.cycleFetch {
		s.cycling = false
	}
	if msg.gen != s.gen {
		return nil
	}
	if msg.err != nil {
		s.setNotice("Could not read the model list: "+strings.TrimRight(errText(msg.err), ". ")+".", true)
		return nil
	}
	if !s.applyModelList(msg.token, msg.res) {
		msg.res = s.modelList
	}
	s.noteModelProblems(msg.res.Diagnostics)
	// The run can have started while the list was read.
	if !s.idleFor(modelBusyText) {
		return nil
	}
	return s.cycleStep(msg.back)
}

func (s *sessionView) cycleStep(back bool) tea.Cmd {
	list := s.cycleList()
	n := len(list)
	if n < 2 {
		s.setNotice(noOtherModelText, false)
		return nil
	}
	i := s.cyclePos(list)
	var j int
	switch {
	case i < 0 && back:
		j = n - 1
	case i < 0:
		j = 0
	case back:
		j = (i + n - 1) % n
	default:
		j = (i + 1) % n
	}
	s.modelReq++
	s.cycleReq, s.cycling = s.modelReq, true
	c, gen, req, e := s.client, s.gen, s.modelReq, list[j]
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.SetModel(ctx, e.id, e.level, false)
		return cycleSetMsg{gen: gen, req: req, entry: e, pos: j + 1, total: n, res: res, err: err}
	}
}

// cycleList is the scope of the session, or else the rows of /model in their
// order. A row whose endpoint left the user model file is not in it.
func (s *sessionView) cycleList() []cycleEntry {
	res := s.modelList
	var out []cycleEntry
	if len(res.Scope) > 0 {
		for _, e := range res.Scope {
			out = append(out, cycleEntry{id: e.ModelID, level: e.ThinkingLevel})
		}
		return out
	}
	changed := map[string]bool{}
	for _, m := range res.Models {
		if m.NotInConfig {
			changed[m.ModelID] = true
		}
	}
	for _, it := range newModelSelectorFor(res, "").items {
		if !it.header && !changed[it.id] {
			out = append(out, cycleEntry{id: it.id})
		}
	}
	return out
}

// cyclePos finds the current model in the list, or -1. A scope can hold one
// model twice with other levels; the entry with the level of the session wins.
func (s *sessionView) cyclePos(list []cycleEntry) int {
	first := -1
	for i, e := range list {
		if e.id != s.modelID {
			continue
		}
		if e.level == "" || e.level == s.thinking {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

// onCycleSet applies the answer of a press. A failure keeps the model and
// shows the reason on the rail; an open /model stays open either way.
func (s *sessionView) onCycleSet(msg cycleSetMsg) {
	if msg.req == s.cycleReq {
		s.cycling = false
	}
	if msg.gen != s.gen || msg.req != s.modelReq {
		return
	}
	if msg.err != nil {
		text, next := s.modelFailure(msg.entry.id, msg.err)
		s.setNotice(strings.TrimSpace(text+" "+next), true)
		return
	}
	s.applyModelResult(msg.entry.id, msg.res)
	n := cycleNotice{ref: s.currentRef(), pos: msg.pos, total: msg.total}
	if s.reasoning() && s.thinking != "" {
		n.level = string(s.thinking)
	}
	if e := msg.entry.level; e != "" && e != protocol.ThinkingDefault && e != protocol.ThinkingOff && msg.res.ThinkingLevel == protocol.ThinkingOff {
		n.dropped = protocol.ACPLevelDropped{ModelID: s.modelID, Level: e}.Text()
	}
	s.notice = noticeView{until: s.now().Add(noticeFor), ok: true, cycle: &n}
}

// cycleRows draws the result notice of a press. Under shortWidth columns the
// word "Model:" and the position go. When the row is too narrow the end of
// the model id is cut; the provider stays whole.
func (s *sessionView) cycleRows(n cycleNotice, width int) string {
	th := s.m.theme
	rail := th.Render(RoleDim, GlyphRailStatus) + " "
	prefix, tail := "", ""
	if n.level != "" {
		tail = " · " + n.level
	}
	if width >= shortWidth {
		prefix = "Model: "
		tail += fmt.Sprintf(" · %d of %d", n.pos, n.total)
	}
	room := width - displayWidth(GlyphRailStatus+" "+GlyphOK+" "+prefix+tail)
	model, ok := cutModelText(n.ref, room)
	if !ok {
		model = truncateTo(n.ref.provider+"/"+n.ref.id, max(1, room))
	}
	rows := []string{rail + th.Render(RoleSuccess, GlyphOK) + " " + th.Render(RoleDim, prefix+model+tail)}
	if n.dropped != "" {
		for _, line := range wrapLines(n.dropped, max(1, width-displayWidth(GlyphRailStatus+" "))) {
			rows = append(rows, rail+th.Render(RoleDim, line))
		}
	}
	return strings.Join(rows, "\n")
}

// noteModelProblems adds the block of the problems of the user model file
// that a model list holds. The block is added only when the set differs from
// the set that the pane showed last for the same files, so the same problems
// show once. The start block lists the problems of every settings file; only
// those of the files in the new set count.
func (s *sessionView) noteModelProblems(diags []protocol.ACPDiagnostic) {
	if len(diags) == 0 {
		s.shownProblems = nil
		return
	}
	paths := map[string]bool{}
	for _, d := range diags {
		paths[d.Path] = true
	}
	last := map[protocol.ACPDiagnostic]bool{}
	for _, d := range s.shownProblems {
		if paths[d.Path] {
			last[d] = true
		}
	}
	next := map[protocol.ACPDiagnostic]bool{}
	for _, d := range diags {
		next[d] = true
	}
	s.shownProblems = append([]protocol.ACPDiagnostic(nil), diags...)
	if len(last) == len(next) {
		same := true
		for d := range next {
			if !last[d] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	s.appendBlock(diagnosticsBlock{heading: modelProblemsHead, items: diags, home: s.home})
}

// levelDroppedBlock is the start notice of a thinking level that the start
// model does not use. Only the driver sees it, as the fallback notice.
func (s *sessionView) levelDroppedBlock() Block {
	if s.levelDropped == nil || s.isObserver() {
		return noRows{}
	}
	return noticeBlock{text: s.levelDropped.Text(), role: RoleMuted}
}
