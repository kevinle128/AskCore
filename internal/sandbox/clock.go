package sandbox

import "time"

// Clock is the time source of the runner. A test passes a fake clock so that
// the kill grace and the drain bound need no real wait.
type Clock interface {
	Now() time.Time
	// AfterFunc runs f in its own goroutine after d. The returned stop function
	// cancels the call and reports whether it did so before f started.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

// SystemClock is the Clock of the real time.
var SystemClock Clock = systemClock{}
