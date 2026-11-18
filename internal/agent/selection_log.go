package agent

import (
	"slices"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// loggedSelection is the model and the thinking level that a log holds, from
// its ModelChange and ThinkingChange entries. An Agent keeps it in memory, so
// a run start compares against it without a scan of the log.
type loggedSelection struct {
	model     sessions.ModelRef
	haveModel bool
	level     protocol.ThinkingLevel
}

// selectionOf reads the newest ModelChange and ThinkingChange of entries. A log
// with no ThinkingChange holds the default level, which is the empty level.
func selectionOf(entries []sessions.Entry) loggedSelection {
	var sel loggedSelection
	for _, e := range entries {
		switch v := e.(type) {
		case sessions.ModelChange:
			sel.model, sel.haveModel = v.Model, true
		case sessions.ThinkingChange:
			sel.level = v.Level
		}
	}
	return sel
}

func sameModelRef(a, b sessions.ModelRef) bool {
	return a.Provider == b.Provider && a.API == b.API && a.ID == b.ID &&
		a.BaseURLHost == b.BaseURLHost && slices.Equal(a.HeaderNames, b.HeaderNames)
}

// logSelection commits the model and the thinking level of the run when they
// differ from the logged ones, so the log holds the selection before the first
// request. A run with no model logs no model.
func (a *Agent) logSelection(log sessions.Writer) error {
	var entries []sessions.Entry
	next := a.logged
	if a.cfg.Model.ID != "" {
		if ref := modelRef(a.cfg.Model); !next.haveModel || !sameModelRef(next.model, ref) {
			entries = append(entries, sessions.ModelChange{Model: ref})
			next.model, next.haveModel = ref, true
		}
	}
	if level := a.cfg.Options.Reasoning; level != next.level {
		entries = append(entries, sessions.ThinkingChange{Level: level})
		next.level = level
	}
	if len(entries) == 0 {
		return nil
	}
	if _, err := log.Append(entries...); err != nil {
		return logWriteError(err)
	}
	a.logged = next
	return nil
}

// LoggedSelection returns what a session log says about the selection of its
// last run, so a resumed session can continue on the same model and level. The
// model and the level come from the newest ModelChange and ThinkingChange; a
// log that has none of them gives the model and the reasoning level of the
// newest RequestDelta. The method is the login method of the newest settled
// attempt that had a credential binding and ran on the restored model (the
// same provider and id). A binding of another model says nothing about the
// restored one, for example a demo model that the user chose after an OAuth
// run. The log never holds a requested method, so a log with no such binding
// gives an empty one. ok is false when the log names no model.
func LoggedSelection(entries []sessions.Entry) (model sessions.ModelRef, method string, level protocol.ThinkingLevel, ok bool) {
	var (
		changedModel, changedLevel bool
		deltaModel                 sessions.ModelRef
		deltaLevel                 protocol.ThinkingLevel
		haveDelta                  bool
		attemptModel               = map[string]sessions.ModelRef{}
		bound                      []boundAttempt
	)
	for _, e := range entries {
		switch v := e.(type) {
		case sessions.ModelChange:
			model, changedModel = v.Model, true
		case sessions.ThinkingChange:
			level, changedLevel = v.Level, true
		case sessions.RequestDelta:
			deltaModel, deltaLevel, haveDelta = v.Model, v.Options.Reasoning, true
			attemptModel[v.AttemptID] = v.Model
		case sessions.AttemptSettled:
			if v.Binding != nil {
				bound = append(bound, boundAttempt{attemptID: v.AttemptID, binding: *v.Binding})
			}
		}
	}
	if !changedModel && haveDelta {
		model, changedModel = deltaModel, true
	}
	if !changedLevel && haveDelta {
		level = deltaLevel
	}
	if changedModel {
		for i := len(bound) - 1; i >= 0; i-- {
			if boundTo(bound[i], attemptModel, model) {
				method = bound[i].binding.Method
				break
			}
		}
	}
	return model, method, level, changedModel
}

// boundAttempt is a settled attempt that had a credential binding.
type boundAttempt struct {
	attemptID string
	binding   sessions.AuthBinding
}

// boundTo tells whether the attempt ran on model. An attempt whose request is
// not in the log is judged by the provider of its binding.
func boundTo(b boundAttempt, attemptModel map[string]sessions.ModelRef, model sessions.ModelRef) bool {
	if ref, ok := attemptModel[b.attemptID]; ok {
		return ref.Provider == model.Provider && ref.ID == model.ID
	}
	return b.binding.Provider == model.Provider
}
