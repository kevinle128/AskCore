//go:build !darwin && !linux

package leader

import "net"

// peerUID cannot read the peer credentials on this platform. The leader refuses to start.
func peerUID(*net.UnixConn) (uint32, error) { return 0, ErrPeerCheckUnsupported }
