package tasks

// FireTimer runs the callback of a timer task as if the timer fired now. The
// test uses it to force the order of a fire and a stop.
func FireTimer(r *Registry, id string) {
	r.mu.Lock()
	t := r.byID[id]
	r.mu.Unlock()
	if t != nil {
		r.fireTimer(t)
	}
}
