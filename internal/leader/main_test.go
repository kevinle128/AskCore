package leader

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) != "" {
		runHelperLeader() // a process of its own: it never returns
	}
	goleak.VerifyTestMain(m)
}
