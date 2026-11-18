package agent

// LogID returns the ID of the session log that the Agent writes now, or "" when
// the log has none (a log in memory). A reset opens a new log, so the ID changes
// then. It asks the log for an optional ID method, so the package needs no
// storage import.
func (a *Agent) LogID() string {
	a.mu.Lock()
	w := a.source.Writer
	a.mu.Unlock()
	if l, ok := w.(interface{ ID() string }); ok {
		return l.ID()
	}
	return ""
}
