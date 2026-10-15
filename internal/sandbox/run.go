//go:build unix

package sandbox

import "context"

// Run starts spec and waits for the end of the process and its group. When ctx
// ends first, Run kills the group (SIGTERM, then SIGKILL after the grace),
// waits for it, and returns ctx.Err() with the output so far. The error is
// otherwise a start error.
func Run(ctx context.Context, spec Spec) (Output, Result, error) {
	p, err := Start(spec)
	if err != nil {
		return Output{}, Result{}, err
	}
	select {
	case <-p.Done():
		return p.Snapshot(), p.Result(), nil
	case <-ctx.Done():
		p.Kill()
		<-p.Done()
		return p.Snapshot(), p.Result(), ctx.Err()
	}
}
