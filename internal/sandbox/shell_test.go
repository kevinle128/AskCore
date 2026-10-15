package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellPrefersBinBash(t *testing.T) {
	argv := ShellArgv("echo hi")
	assert.Equal(t, []string{"/bin/bash", "-c", "echo hi"}, argv)
}
