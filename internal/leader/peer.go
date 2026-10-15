package leader

import (
	"errors"
	"fmt"
	"net"
)

var (
	// ErrPeerRejected means the process on the other end of a socket is not the owner.
	ErrPeerRejected = errors.New("leader: peer is not the owner")
	// ErrPeerCheckUnsupported means this platform cannot read the peer credentials.
	ErrPeerCheckUnsupported = errors.New("leader: peer credential check is not supported on this platform")
)

// CheckPeer accepts a connection only when the peer runs as ownerUID. It reads
// the credentials from the kernel and never from the peer. A platform without
// this check returns ErrPeerCheckUnsupported; the check is never skipped.
func CheckPeer(conn net.Conn, ownerUID uint32) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("%w: not a Unix socket", ErrPeerRejected)
	}
	uid, err := peerUID(uc)
	if err != nil {
		if errors.Is(err, ErrPeerCheckUnsupported) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrPeerRejected, err)
	}
	if uid != ownerUID {
		return ErrPeerRejected
	}
	return nil
}

// controlFd runs fn with the descriptor of conn.
func controlFd(conn *net.UnixConn, fn func(fd int) error) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := raw.Control(func(fd uintptr) { inner = fn(int(fd)) }); err != nil {
		return err
	}
	return inner
}
