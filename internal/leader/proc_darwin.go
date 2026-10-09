package leader

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// processInfo reads the user, the start time and the command line of a process
// from ps. The start time of ps has a resolution of one second.
func processInfo(pid int) (uid uint32, start int64, cmdline string, err error) {
	cmd := exec.Command("ps", "-o", "uid=", "-o", "lstart=", "-o", "command=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, "", fmt.Errorf("ps: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 7 {
		return 0, 0, "", fmt.Errorf("ps: unexpected output")
	}
	u, err := strconv.ParseUint(fields[0], 10, 32)
	if err != nil {
		return 0, 0, "", err
	}
	t, err := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(fields[1:6], " "), time.Local)
	if err != nil {
		return 0, 0, "", err
	}
	return uint32(u), t.Unix(), strings.Join(fields[6:], " "), nil
}

// There is no stable process handle for this fallback on this platform.
func signalLeaderProcess(Paths, Owner) error {
	return fmt.Errorf("%w: stable process signaling is not available; use the socket shutdown control", ErrNotLeaderProcess)
}
