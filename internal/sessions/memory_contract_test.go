package sessions_test

import (
	"testing"

	"AskCore/internal/sessions"
	"AskCore/internal/store/storetest"
)

func TestMemoryLogWriterContract(t *testing.T) {
	storetest.RunSessionWriter(t, func(*testing.T) sessions.Writer { return &sessions.MemoryLog{} })
}
