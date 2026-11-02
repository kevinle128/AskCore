package testsupport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingTB keeps the errors of the guard instead of failing the test that
// checks the guard.
type recordingTB struct {
	testing.TB
	mu     sync.Mutex
	errors []string
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func TestLoopbackOnlyAllowsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	t.Cleanup(srv.Close)
	tb := &recordingTB{TB: t}
	resp, err := LoopbackOnly(tb).Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, tb.errors)
}

func TestLoopbackOnlyRefusesOtherHosts(t *testing.T) {
	for _, url := range []string{"https://models.dev/api.json", "https://api.openai.com/v1/models", "http://10.0.0.1/"} {
		tb := &recordingTB{TB: t}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		require.NoError(t, err)
		resp, err := LoopbackOnly(tb).Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		require.ErrorIs(t, err, ErrNetworkGuard, url)
		require.Len(t, tb.errors, 1, url)
		require.True(t, strings.Contains(tb.errors[0], "network guard"), tb.errors[0])
	}
}
