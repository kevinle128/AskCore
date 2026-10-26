package leader

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultZombieAfter = 30 * time.Second
	defaultEvictWait   = 8 * time.Second
)

// evictResult tells which signals evictLeaderProcess sent.
type evictResult struct {
	term bool // SIGTERM was sent
	kill bool // SIGKILL was sent
}

func (r evictResult) String() string {
	switch {
	case r.kill:
		return "SIGTERM and SIGKILL"
	case r.term:
		return "SIGTERM"
	}
	return "no signal"
}

// zombieOwner tells whether an owner record is that of a leader that served for
// at least `after`. Such a leader that has no socket lost it. A record without a
// state (an older build), a stopping leader and a young leader are not evicted.
func zombieOwner(o Owner, now time.Time, after time.Duration) bool {
	return o.State == OwnerServing && o.PID > 1 && o.Start > 0 && o.Instance != "" &&
		now.Sub(time.Unix(o.Start, 0)) >= after
}

// evictZombie stops a leader that holds the lock but has no socket, when the
// lock file shows a serving leader older than ZombieAfter and the process passes
// verification. It sends SIGTERM and then, if the process lives on and still has
// the same record, SIGKILL, each followed by a bounded wait. It never evicts one
// record twice in a call. The caller goes on with the normal wait and start.
func (c *connector) evictZombie(ctx context.Context) {
	owner, err := ReadOwner(c.cfg.Paths)
	if err != nil || c.handled[owner] || !zombieOwner(owner, time.Now(), c.cfg.ZombieAfter) {
		return
	}
	if c.handled == nil {
		c.handled = map[Owner]bool{}
	}
	c.handled[owner] = true
	//nolint:staticcheck // The stub of a platform without process handles always fails; Linux does not.
	res, err := evictLeaderProcess(ctx, c.cfg.Paths, owner, c.cfg.EvictWait)
	//nolint:staticcheck // The stub of a platform without process handles always fails; Linux does not.
	switch {
	case errors.Is(err, ErrNotLeaderProcess) && !res.term:
		// The process cannot be signalled safely here: wait, as for any lock holder.
	case err != nil && !res.term:
		c.evictNote = fmt.Sprintf("could not signal leader pid %d that holds the lock without a socket: %v", owner.PID, err)
	case res.term && err != nil:
		// The leader still lived after the signals, or began to stop.
		c.evictNote = fmt.Sprintf("sent %s to leader pid %d that holds the lock without a socket; try again", res, owner.PID)
	}
}
