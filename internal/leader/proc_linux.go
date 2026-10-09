package leader

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

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

// signalLeaderProcess binds verification and signaling to one kernel process handle.
func signalLeaderProcess(paths Paths, owner Owner) error {
	fd, err := unix.PidfdOpen(owner.PID, 0)
	if err != nil {
		return fmt.Errorf("%w: stable process handle: %v", ErrNotLeaderProcess, err)
	}
	defer func() { _ = unix.Close(fd) }()
	if err := VerifyLeaderProcess(owner.PID, owner.Start); err != nil {
		return err
	}
	// If the original process exited during verification, the PID snapshot can
	// belong to a replacement. Never signal from that snapshot.
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if n, err := unix.Poll(poll, 0); err != nil || n != 0 {
		return fmt.Errorf("%w: process exited during verification", ErrNotLeaderProcess)
	}
	if err := verifySignalOwner(paths, owner); err != nil {
		return err
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return fmt.Errorf("leader: signal: %w", err)
	}
	return nil
}
