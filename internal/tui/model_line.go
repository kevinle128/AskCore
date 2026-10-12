package tui

import (
	"strings"

	"AskCore/pkg/protocol"
)

// Model line.
//
// A session names its provider and model in the block pane: a start line at
// the top, after a divider or after a restore note, and a change line when the
// leader reports a new model. The lines are plain transcript blocks of the
// client, drawn again at each width.

// modelLineLabelMin is the width from which the line has its "model" label.
const modelLineLabelMin = shortWidth

// modelRef names a model by the ids of the catalog. The zero value means no
// model.
type modelRef struct{ provider, id, api string }

func (r modelRef) empty() bool { return r.provider == "" && r.id == "" }

// parseModelID reads a row id of the form "provider/id@api".
func parseModelID(modelID string) modelRef {
	if modelID == "" {
		return modelRef{}
	}
	var r modelRef
	rest := modelID
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest, r.api = rest[:i], rest[i+1:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		r.provider, r.id = rest[:i], rest[i+1:]
	} else {
		r.id = rest
	}
	return r
}

// refOf gives the model reference of a row id. A catalog row wins over the
// text of the id.
func (s *sessionView) refOf(modelID string) modelRef {
	if modelID == "" {
		return modelRef{}
	}
	if row, ok := s.modelRow(modelID); ok && row.ID != "" {
		return modelRef{provider: row.Provider, id: row.ID, api: row.API}
	}
	return parseModelID(modelID)
}

// modelRefID builds the row id of a model that an event names.
func (s *sessionView) modelRefID(r modelRef) string {
	if r.empty() {
		return ""
	}
	for _, row := range s.models {
		if row.Provider == r.provider && row.ID == r.id && (r.api == "" || row.API == r.api) {
			return row.ModelID
		}
	}
	id := r.provider + "/" + r.id
	if r.api != "" {
		id += "@" + r.api
	}
	return id
}

// pairText writes "<provider>/<id>" and the wire format of the model. The
// format is set only when the catalog lists the same provider and id on two
// formats, as the model list does.
func (s *sessionView) pairText(r modelRef) (pair, api string) {
	if r.empty() {
		return "", ""
	}
	pair = r.id
	if r.provider != "" {
		pair = r.provider + "/" + r.id
	}
	n := 0
	seen := map[string]bool{}
	for _, row := range s.models {
		if row.Provider == r.provider && row.ID == r.id && !seen[row.ModelID] {
			seen[row.ModelID] = true
			n++
		}
	}
	if n > 1 && r.api != "" {
		api = r.api
	}
	return pair, api
}

// modelLineBlock is the content at the top of the pane, after a divider or
// after a restore note: the pair, an optional "was" part, a fallback reason and
// the trace warning.
type modelLineBlock struct {
	pair string
	api  string
	was  string
	// fallback is the reason row under the pair, already worded.
	fallback string
	// trace is the trace file name, or empty when no warning shows.
	trace string
}

// traceWarningText is the warning of a set trace file.
func traceWarningText(file string) string {
	return "TEA_TRACE is on: typed and pasted text, keys included, is written to " + file + "."
}

// rows draws the block without the empty row that follows it.
func (b modelLineBlock) rows(rc RenderContext) []string {
	th := rc.Theme
	width := max(1, rc.Width)
	var out []string
	prefix := strings.Repeat(" ", glyphIndent)
	label := ""
	if width >= modelLineLabelMin {
		label = "model"
		prefix = "  model  "
	}
	pad := strings.Repeat(" ", displayWidth(prefix))
	lead := func(first bool) string {
		if first && label != "" {
			return "  " + th.Render(RoleMuted, label) + "  "
		}
		return pad
	}
	if b.pair != "" {
		pair, api := b.pair, b.api
		room := max(1, width-displayWidth(prefix))
		rowsOfPair := []string{pair}
		if displayWidth(pair) > room {
			// A pair wider than the terminal wraps after the slash.
			rowsOfPair = splitPair(pair, room)
		}
		for i, r := range rowsOfPair {
			out = append(out, lead(i == 0)+th.Render(RoleFg, r))
		}
		last := len(out) - 1
		if api != "" {
			apiText := " (" + api + ")"
			if displayWidth(prefix)+displayWidth(rowsOfPair[len(rowsOfPair)-1])+displayWidth(apiText) <= width {
				out[last] += th.Render(RoleFg, apiText)
			} else {
				out = append(out, pad+th.Render(RoleFg, truncateTo("("+api+")", room)))
				last = len(out) - 1
			}
		}
		if b.was != "" && width >= modelLineLabelMin && len(rowsOfPair) == 1 {
			was := " · was " + b.was
			plainRow := displayWidth(prefix) + displayWidth(pair) + displayWidth(was)
			if api != "" {
				plainRow += displayWidth(" (" + api + ")")
			}
			if plainRow <= width && last == 0 {
				out[last] += th.Render(RoleDim, was)
			}
		}
	}
	if b.fallback != "" {
		for _, r := range wrapLines(b.fallback, max(1, width-displayWidth(pad))) {
			out = append(out, pad+th.Render(RoleMuted, r))
		}
	}
	if b.trace != "" {
		for _, r := range wrapLines(traceWarningText(b.trace), max(1, width-glyphIndent)) {
			out = append(out, strings.Repeat(" ", glyphIndent)+th.Render(RoleWarning, r))
		}
	}
	return out
}

// splitPair breaks a pair after its slash, and cuts a part that is still too
// wide.
func splitPair(pair string, room int) []string {
	if i := strings.Index(pair, "/"); i >= 0 {
		return []string{truncateTo(pair[:i+1], room), truncateTo(pair[i+1:], room)}
	}
	return []string{truncateTo(pair, room)}
}

// Lines implements Block.
func (b modelLineBlock) Lines(rc RenderContext) []string {
	rows := b.rows(rc)
	if len(rows) == 0 {
		return nil
	}
	return append(rows, "")
}

// noRows is a placeholder in the transcript. It draws nothing until the state
// of the session is known, and then it is replaced.
type noRows struct{}

func (noRows) Lines(RenderContext) []string { return nil }

// startKind tells which blocks a start slot may carry.
type startKind int

const (
	// startFirst is the start line at the top of a transcript.
	startFirst startKind = iota
	startDivider
	startRestore
)

// startSlot is a placeholder in the transcript that becomes the start line.
type startSlot struct {
	id   string
	kind startKind
	// note is the text of the restore note that the line joins.
	note string
}

// restoredNote is the note after a reconnect with the start line directly
// below it.
type restoredNote struct {
	text  string
	start modelLineBlock
}

func (b restoredNote) Lines(rc RenderContext) []string {
	th := rc.Theme
	body := styleRows(th, RoleFg, wrapUntrusted(b.text, bodyWidth(rc)))
	out := leadRows(th.Render(RoleSuccess, GlyphRetry), body)
	out = append(out, b.start.rows(rc)...)
	return append(out, "")
}

// fallbackReasonText words why the saved default was not used.
func fallbackReasonText(r protocol.ACPFallbackReason) string {
	switch r {
	case protocol.ACPFallbackNoCredential:
		return "no credential"
	case protocol.ACPFallbackNotInCatalog:
		return "not in the catalog"
	}
	return "not available"
}

// startBlockFor builds the start line for the state the view knows now.
func (s *sessionView) startBlockFor(kind startKind) modelLineBlock {
	var b modelLineBlock
	if ref := s.currentRef(); !ref.empty() {
		b.pair, b.api = s.pairText(ref)
	}
	if kind != startRestore && s.fallback != nil && !s.isObserver() {
		if def := parseModelID(s.fallback.SavedDefault); !def.empty() {
			pair, api := s.pairText(def)
			if api != "" {
				pair += " (" + api + ")"
			}
			b.fallback = "Default model " + pair + " is not usable (" + fallbackReasonText(s.fallback.Reason) + ")."
		}
	}
	if kind == startFirst {
		b.trace = s.trace
	}
	return b
}

// currentRef is the reference of the model of the session.
func (s *sessionView) currentRef() modelRef { return s.refOf(s.modelID) }

// addStartSlot puts a placeholder at the end of the transcript. A restore
// slot is the note itself, which then carries the line.
func (s *sessionView) addStartSlot(kind startKind, note string) {
	id := s.nextBlockID()
	var b Block = noRows{}
	if kind == startRestore {
		b = restoredNote{text: note}
	}
	s.m.applyBlock(AppendBlockMsg{ID: id, Block: b, Stable: false})
	s.starts = append(s.starts, startSlot{id: id, kind: kind, note: note})
}

// settleStartsIfKnown fills the placeholders when the state of the current
// session is already known. Otherwise the answer of the state read does it.
func (s *sessionView) settleStartsIfKnown() {
	if s.stateGen == s.gen && s.haveState {
		s.settleStarts()
	}
}

// settleStarts replaces the placeholders with the start line of the model that
// the view knows and marks them stable.
func (s *sessionView) settleStarts() {
	if len(s.starts) == 0 {
		return
	}
	for _, st := range s.starts {
		b := s.startBlockFor(st.kind)
		switch st.kind {
		case startRestore:
			s.m.applyBlock(ReplaceBlockMsg{ID: st.id, Block: restoredNote{text: st.note, start: b}})
		default:
			s.m.applyBlock(ReplaceBlockMsg{ID: st.id, Block: b})
		}
		s.m.applyBlock(FinishBlockMsg{ID: st.id})
	}
	s.starts = nil
	s.lastRef = s.currentRef()
}

// onModelUpdate draws the change line of a model event of the leader. An event
// at or below the baseline of the follow is already in the state that the view
// read, so it draws nothing.
func (s *sessionView) onModelUpdate(v SessionEventMsg, e *protocol.ModelUpdate) {
	if s.baseline.Epoch != "" && (v.Epoch != s.baseline.Epoch || v.Seq <= s.baseline.Seq) {
		return
	}
	ref := modelRef{provider: e.Provider, id: e.ID, api: e.API}
	s.modelID = s.modelRefID(ref)
	if s.modelID != "" {
		s.hadModel = true
	}
	s.modelSeq++
	s.haveState = true
	if len(s.starts) > 0 {
		// The start line that waits for the state will show this model.
		return
	}
	if ref.empty() {
		s.lastRef = modelRef{}
		return
	}
	if ref == s.lastRef {
		return
	}
	b := modelLineBlock{}
	b.pair, b.api = s.pairText(ref)
	if !s.lastRef.empty() {
		was, wasAPI := s.pairText(s.lastRef)
		if wasAPI != "" {
			was += " (" + wasAPI + ")"
		}
		b.was = was
	}
	s.lastRef = ref
	s.appendBlock(b)
}
