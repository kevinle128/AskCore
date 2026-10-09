package leader

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// socketPair returns the server side and the client side of a real Unix socket.
func socketPair(t *testing.T) (server, client *net.UnixConn) {
	t.Helper()
	p := testPaths(t)
	ln, err := net.Listen("unix", filepath.Join(p.Home, "t.sock"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	type result struct {
		c   net.Conn
		err error
	}
	accepted := make(chan result, 1)
	go func() {
		c, err := ln.Accept()
		accepted <- result{c, err}
	}()
	cc, err := net.Dial("unix", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cc.Close() })
	r := <-accepted
	require.NoError(t, r.err)
	t.Cleanup(func() { _ = r.c.Close() })
	return r.c.(*net.UnixConn), cc.(*net.UnixConn)
}

func TestPeerUIDIsTheConnectingUser(t *testing.T) {
	server, _ := socketPair(t)
	uid, err := peerUID(server)
	require.NoError(t, err)
	require.Equal(t, uint32(os.Geteuid()), uid)
}

func TestPeerAcceptsOwner(t *testing.T) {
	server, _ := socketPair(t)
	require.NoError(t, CheckPeer(server, uint32(os.Geteuid())))
}

func TestPeerRejectsOtherUID(t *testing.T) {
	server, _ := socketPair(t)
	// The owner is injected as another user, so the real credential call runs and the mismatch is real.
	err := CheckPeer(server, uint32(os.Geteuid())+1)
	require.ErrorIs(t, err, ErrPeerRejected)
}

func TestPeerRequiresUnixConnection(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()
	require.ErrorIs(t, CheckPeer(a, uint32(os.Geteuid())), ErrPeerRejected)
}
