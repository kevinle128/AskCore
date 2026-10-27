package acp_test

import (
	"fmt"
	"os"
	"testing"
)

// TestMain gives the tests a home of their own. An Agent without an explicit
// home reads ASK_HOME and then ~/.ask, so without this the tests would sweep
// and write the task output of the developer.
func TestMain(m *testing.M) { os.Exit(runIsolated(m)) }

// runIsolated runs the tests with ASK_HOME set to a fresh private directory.
// It restores the earlier value and removes the directory when the tests end.
func runIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "ask-acp-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "acp tests: make home: %v\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(home) }()
	// MkdirTemp gives 0700 already; the call makes the mode independent of it.
	if err := os.Chmod(home, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "acp tests: private home: %v\n", err)
		return 1
	}
	prev, had := os.LookupEnv("ASK_HOME")
	if err := os.Setenv("ASK_HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "acp tests: set ASK_HOME: %v\n", err)
		return 1
	}
	defer func() {
		if had {
			_ = os.Setenv("ASK_HOME", prev)
		} else {
			_ = os.Unsetenv("ASK_HOME")
		}
	}()
	return m.Run()
}
