package leader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// processInfo reads the user, the start time and the command line of a process
// from /proc.
func processInfo(pid int) (uid uint32, start int64, cmdline string, err error) {
	dir := "/proc/" + strconv.Itoa(pid)
	info, err := os.Stat(dir)
	if err != nil {
		return 0, 0, "", err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, "", fmt.Errorf("proc: no owner")
	}
	raw, err := os.ReadFile(dir + "/cmdline")
	if err != nil {
		return 0, 0, "", err
	}
	cmdline = strings.TrimSpace(strings.ReplaceAll(string(raw), "\x00", " "))
	stat, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return 0, 0, "", err
	}
	// The command name is in parentheses and can hold spaces. Field 22 is the
	// start time in clock ticks after boot.
	text := string(stat)
	i := strings.LastIndex(text, ")")
	if i < 0 {
		return 0, 0, "", fmt.Errorf("proc: bad stat")
	}
	rest := strings.Fields(text[i+1:])
	if len(rest) < 20 {
		return 0, 0, "", fmt.Errorf("proc: bad stat")
	}
	ticks, err := strconv.ParseInt(rest[19], 10, 64)
	if err != nil {
		return 0, 0, "", err
	}
	boot, err := bootTime()
	if err != nil {
		return 0, 0, "", err
	}
	const clockTicks = 100 // USER_HZ on every Linux ABI that Go supports
	return st.Uid, boot + ticks/clockTicks, cmdline, nil
}

func bootTime() (int64, error) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, "btime "); ok {
			return strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
		}
	}
	return 0, fmt.Errorf("proc: no btime")
}

// verifiedPidfd opens a handle on the owner process and checks, through that
// handle, that the process is the leader of the record. The caller closes it.
func verifiedPidfd(paths Paths, owner Owner) (int, error) {
	fd, err := unix.PidfdOpen(owner.PID, 0)
	if err != nil {
		return -1, fmt.Errorf("%w: stable process handle: %v", ErrNotLeaderProcess, err)
	}
	if err := VerifyLeaderProcess(owner); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	// If the original process exited during verification, the PID snapshot can
	// belong to a replacement. Never signal from that snapshot.
	if exited, err := pidfdExited(context.Background(), fd, 0); err != nil || exited {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("%w: process exited during verification", ErrNotLeaderProcess)
	}
	if err := verifySignalOwner(paths, owner); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

// pidfdExited reports whether the process of fd ended within wait. A pidfd
// becomes readable when its process ends.
func pidfdExited(ctx context.Context, fd int, wait time.Duration) (bool, error) {
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	end := time.Now().Add(wait)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		left := min(max(time.Until(end), 0), pollInterval)
		// Round up to a millisecond so a timeout does not end the wait early.
		n, err := unix.Poll(poll, int((left+time.Millisecond-1)/time.Millisecond))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if n > 0 || err != nil || !time.Now().Before(end) {
			return n > 0, err
		}
	}
}

// signalLeaderProcess binds verification and signaling to one kernel process handle.
func signalLeaderProcess(paths Paths, owner Owner) error {
	fd, err := verifiedPidfd(paths, owner)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return fmt.Errorf("leader: signal: %w", err)
	}
	return nil
}

// evictLeaderProcess stops a leader process through one kernel handle: SIGTERM,
// then, if the process still runs and the lock file still holds the same
// record, SIGKILL. A PID that the system reuses cannot be reached through the
// handle. Each wait lasts at most `wait` and ends when ctx ends. It returns nil
// when the process ended.
func evictLeaderProcess(ctx context.Context, paths Paths, owner Owner, wait time.Duration) (evictResult, error) {
	var res evictResult
	fd, err := verifiedPidfd(paths, owner)
	if err != nil {
		return res, err
	}
	defer func() { _ = unix.Close(fd) }()
	budget := func() time.Duration {
		d := wait
		if deadline, ok := ctx.Deadline(); ok {
			d = min(d, time.Until(deadline))
		}
		return max(d, time.Millisecond)
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return res, context.DeadlineExceeded
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return res, nil
		}
		return res, fmt.Errorf("leader: signal: %w", err)
	}
	res.term = true
	if exited, err := pidfdExited(ctx, fd, budget()); err != nil || exited {
		return res, err
	}
	// The leader began to stop, or another process took over the record, if it
	// changed: then no SIGKILL.
	if err := verifySignalOwner(paths, owner); err != nil {
		return res, err
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return res, context.DeadlineExceeded
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return res, nil
		}
		return res, fmt.Errorf("leader: signal: %w", err)
	}
	res.kill = true
	if exited, err := pidfdExited(ctx, fd, budget()); err != nil || exited {
		return res, err
	}
	return res, fmt.Errorf("leader: pid %d is still running after SIGKILL", owner.PID)
}
