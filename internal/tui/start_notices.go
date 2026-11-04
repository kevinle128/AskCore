package tui

import (
	"strings"

	"AskCore/pkg/protocol"
)

// Start notices.
//
// The block pane names no model: the footer row does. At the start of a
// transcript, and under the divider of a newly followed session, the pane can
// show the fallback notice when the saved default model was not usable. It
// needs the state of the session, so a placeholder holds its place until the
// state answer arrives. The TEA_TRACE warning is its own notice at the start of
// the transcript. Both are plain transcript blocks of the client, drawn again
// at each width.

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

// noticeBlock is a notice of the client in the pane: no glyph, indented by
// glyphIndent at every width and wrapped at spaces.
type noticeBlock struct {
	text string
	role Role
}

func (b noticeBlock) Lines(rc RenderContext) []string {
	pad := strings.Repeat(" ", glyphIndent)
	var out []string
	for _, r := range wrapLines(b.text, textWidth(rc, glyphIndent)) {
		out = append(out, pad+rc.Theme.Render(b.role, r))
	}
	return out
}

// traceWarningText is the warning of a set trace file.
func traceWarningText(file string) string {
	return "TEA_TRACE is on: typed and pasted text, keys included, is written to " + file + "."
}

// noRows is a placeholder in the transcript. It draws nothing until the state
// of the session is known, and then it is replaced.
type noRows struct{}

func (noRows) Lines(RenderContext) []string { return nil }

// restoredNote is the note after a reconnect that restored the session.
type restoredNote struct {
	text string
}

func (b restoredNote) Lines(rc RenderContext) []string {
	th := rc.Theme
	body := styleRows(th, RoleFg, wrapUntrusted(b.text, textWidth(rc, textIndent)))
	return leadRows(th.Render(RoleSuccess, GlyphRetry), body)
}

// fallbackReasonText words why the saved default was not used.
func fallbackReasonText(r protocol.ACPFallbackReason) string {
	switch r {
	case protocol.ACPFallbackNoCredential:
		return "no credential"
	case protocol.ACPFallbackNotInCatalog:
		return "not in the catalog"
	case protocol.ACPFallbackNotInAccount:
		return "not in account list"
	}
	return "not available"
}

// fallbackBlock is the fallback notice for the state the view knows now, or a
// block with no rows. Only the driver sees it: an observer did not start the
// session.
func (s *sessionView) fallbackBlock() Block {
	if s.fallback == nil || s.isObserver() {
		return noRows{}
	}
	def := parseModelID(s.fallback.SavedDefault)
	if def.empty() {
		return noRows{}
	}
	pair, api := s.pairText(def)
	if api != "" {
		pair += " (" + api + ")"
	}
	text := "Default model " + pair + " is not usable (" + fallbackReasonText(s.fallback.Reason) + ")."
	return noticeBlock{text: text, role: RoleMuted}
}

// currentRef is the reference of the model of the session.
func (s *sessionView) currentRef() modelRef { return s.refOf(s.modelID) }

// addFallbackSlot puts the placeholder of a fallback notice at the end of the
// transcript.
func (s *sessionView) addFallbackSlot() {
	id := s.nextBlockID()
	s.m.applyBlock(AppendBlockMsg{ID: id, Block: noRows{}, Stable: false})
	s.starts = append(s.starts, id)
}

// settleStartsIfKnown fills the placeholders when the state of the current
// session is already known. Otherwise the answer of the state read does it.
func (s *sessionView) settleStartsIfKnown() {
	if s.stateGen == s.gen && s.haveState {
		s.settleStarts()
	}
}

// settleStarts replaces the placeholders with the trust note, the settings
// problems and the fallback notice that the view knows, or with nothing, and
// marks them stable.
func (s *sessionView) settleStarts() {
	s.settleTrustSlots()
	for _, id := range s.starts {
		s.m.applyBlock(ReplaceBlockMsg{ID: id, Block: s.fallbackBlock()})
		s.m.applyBlock(FinishBlockMsg{ID: id})
	}
	s.starts = nil
}

// onModelUpdate takes the model of a model event of the leader. The footer row
// shows it; the pane draws nothing. An event at or below the baseline of the
// follow is already in the state that the view read, so it changes nothing.
func (s *sessionView) onModelUpdate(v SessionEventMsg, e *protocol.ModelUpdate) {
	if s.baseline.Epoch != "" && (v.Epoch != s.baseline.Epoch || v.Seq <= s.baseline.Seq) {
		return
	}
	s.modelID = s.modelRefID(modelRef{provider: e.Provider, id: e.ID, api: e.API})
	if s.modelID != "" {
		s.hadModel = true
	}
	s.modelSeq++
	s.haveState = true
}
