package sandbox

import (
	"os"
	"os/exec"
	"sync"
)

const (
	binBash = "/bin/bash"
	binSh   = "/bin/sh"
)

// shellPath resolves the shell once: /bin/bash when it is an executable file,
// else bash on PATH, else /bin/sh.
var shellPath = sync.OnceValue(func() string {
	if info, err := os.Stat(binBash); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return binBash
	}
	if path, err := exec.LookPath("bash"); err == nil {
		return path
	}
	return binSh
})

// ShellArgv returns the argv that runs command with the shell.
func ShellArgv(command string) []string {
	return []string{shellPath(), "-c", command}
}
