package logs

import "testing"

func TestNew(t *testing.T) {
	for _, level := range []string{"", "debug", "production", "info", "warn", "error"} {
		logger, err := New(level)
		if err != nil {
			t.Fatalf("New(%q): %v", level, err)
		}
		_ = logger.Sync()
	}
	if _, err := New("nope"); err == nil {
		t.Fatal("New(nope) succeeded")
	}
}
