package acp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPendingResponsesDuplicateIDsNeedOneResponseEach(t *testing.T) {
	p := &pendingResponses{ids: map[string]int{}}
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{}}`)
	resp := []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	p.request(req)
	p.request(req)

	failed, closed := make(chan struct{}), make(chan struct{})
	waitDone := make(chan struct{})
	go func() {
		p.closeAndWait(failed, closed)
		close(waitDone)
	}()
	armed := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.idle != nil
	}
	require.Eventually(t, armed, stdioWaitInternal, time.Millisecond)

	p.written(resp)
	require.True(t, armed(), "the barrier stays shut while a second request with the same id waits")
	p.written(resp)
	select {
	case <-waitDone:
	case <-time.After(stdioWaitInternal):
		t.Fatal("the barrier did not open after both responses")
	}
}

const stdioWaitInternal = 5 * time.Second
