package leader

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"AskCore/pkg/protocol"
)

// ErrVersionMismatch is returned with a management-only connection after the
// client sent another protocol version. A caller must not route ACP for it.
var ErrVersionMismatch = errors.New("leader: protocol version mismatch")

// defaultRegisterTimeout bounds the wait for the first frame of a new client.
const defaultRegisterTimeout = 5 * time.Second

// errorWriteTimeout bounds the write of a refusal to a peer that does not read.
const errorWriteTimeout = time.Second

// ServerConfig is the typed configuration of the handshake on the leader side.
type ServerConfig struct {
	// ProtocolVersion is the outer version this leader speaks. Zero means the current version.
	ProtocolVersion int
	// OwnerUID is the only user that may connect. Nil means the current user.
	OwnerUID *uint32
	// InstanceID and Build identify this leader process.
	InstanceID string
	Build      string
	// Ready tells the client whether startup work is done. When false the leader
	// sends a leader_ready frame later on the returned writer.
	Ready    bool
	Controls []string
	// RegisterTimeout bounds the wait for the register frame. Zero means 5 seconds.
	RegisterTimeout time.Duration
	// MaxFrame is the largest frame. Zero means protocol.LeaderMaxFrame.
	MaxFrame int
}

func (c ServerConfig) withDefaults() ServerConfig {
	if c.ProtocolVersion == 0 {
		c.ProtocolVersion = protocol.LeaderProtocolVersion
	}
	if c.OwnerUID == nil {
		uid := uint32(os.Geteuid())
		c.OwnerUID = &uid
	}
	if c.RegisterTimeout == 0 {
		c.RegisterTimeout = defaultRegisterTimeout
	}
	if c.MaxFrame == 0 {
		c.MaxFrame = protocol.LeaderMaxFrame
	}
	return c
}

// Accepted is a client that passed the handshake. The caller owns the reader
// and the writer. Only one goroutine may read and only one may write.
type Accepted struct {
	// ClientID is the id that registered gave the client.
	ClientID string
	Hello    protocol.LeaderRegister
	Reader   *FrameReader
	Writer   *FrameWriter
	// Management is true after a version mismatch. The connection may then send
	// only status and shutdown control frames, never ACP.
	Management bool
	conn       net.Conn
}

// Conn returns the connection, so the caller can set deadlines and close it.
func (a *Accepted) Conn() net.Conn { return a.conn }

// Accept runs the leader side of the handshake. The order is fixed: it checks the
// peer user first, then reads one register frame under a deadline, then checks
// the version, then writes the registered frame. It reads no ACP frame.
// On an error it closes the connection, with one exception: after a version
// mismatch it returns both the management-only connection and ErrVersionMismatch.
func Accept(conn net.Conn, cfg ServerConfig, clientID string) (*Accepted, error) {
	cfg = cfg.withDefaults()
	w := NewFrameWriter(conn, cfg.MaxFrame)
	fail := func(kind, message string, err error) (*Accepted, error) {
		refuse(conn, w, protocol.LeaderError{Kind: kind, Message: message})
		return nil, err
	}

	if err := CheckPeer(conn, *cfg.OwnerUID); err != nil {
		if errors.Is(err, ErrPeerCheckUnsupported) {
			_ = conn.Close()
			return nil, err
		}
		return fail(protocol.LeaderErrPeerRejected, "the peer is not the owner of this leader", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(cfg.RegisterTimeout))
	r := NewFrameReader(conn, cfg.MaxFrame)
	frame, err := r.Next()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("leader: register frame: %w", err)
	}
	var hello protocol.LeaderRegister
	if frame.Type != protocol.LeaderFrameRegister || json.Unmarshal(frame.Payload, &hello) != nil {
		return fail("leader_register_invalid", "the first frame must be register", fmt.Errorf("%w: first frame is not register", ErrFrameInvalid))
	}
	_ = conn.SetReadDeadline(time.Time{})

	acc := &Accepted{ClientID: clientID, Hello: hello, Reader: r, Writer: w, conn: conn}
	if hello.ProtocolVersion != cfg.ProtocolVersion {
		upgrade := protocol.LeaderUpgradeClient
		if hello.ProtocolVersion > cfg.ProtocolVersion {
			upgrade = protocol.LeaderUpgradeLeader
		}
		msg := fmt.Sprintf("leader protocol %d, client protocol %d; upgrade the %s", cfg.ProtocolVersion, hello.ProtocolVersion, upgrade)
		if err := writeError(conn, w, protocol.LeaderError{Kind: protocol.LeaderErrVersionMismatch, Message: msg, Upgrade: upgrade}); err != nil {
			_ = conn.Close()
			return nil, err
		}
		acc.Management = true
		return acc, ErrVersionMismatch
	}

	payload, err := json.Marshal(protocol.LeaderRegistered{
		ClientID: clientID, InstanceID: cfg.InstanceID, Build: cfg.Build,
		ProtocolVersion: cfg.ProtocolVersion, Ready: cfg.Ready, Controls: cfg.Controls,
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(errorWriteTimeout))
	err = w.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameRegistered, Payload: payload})
	_ = conn.SetWriteDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return acc, nil
}

// NextManagement reads one frame from a management-only connection. It accepts
// a status or shutdown control frame and refuses everything else, ACP included.
func (a *Accepted) NextManagement(timeout time.Duration) (protocol.LeaderControl, error) {
	_ = a.conn.SetReadDeadline(time.Now().Add(timeout))
	defer func() { _ = a.conn.SetReadDeadline(time.Time{}) }()
	frame, err := a.Reader.Next()
	if err != nil {
		return protocol.LeaderControl{}, err
	}
	var ctl protocol.LeaderControl
	if frame.Type != protocol.LeaderFrameControl || json.Unmarshal(frame.Payload, &ctl) != nil {
		return protocol.LeaderControl{}, fmt.Errorf("%w: only control frames are allowed after a version mismatch", ErrFrameInvalid)
	}
	if ctl.Command != protocol.LeaderControlStatus && ctl.Command != protocol.LeaderControlShutdown {
		return protocol.LeaderControl{}, fmt.Errorf("%w: control %q is not allowed", ErrFrameInvalid, ctl.Command)
	}
	return ctl, nil
}

// refuse writes an error frame on a best-effort basis and closes the connection.
func refuse(conn net.Conn, w *FrameWriter, e protocol.LeaderError) {
	_ = writeError(conn, w, e)
	_ = conn.Close()
}

func writeError(conn net.Conn, w *FrameWriter, e protocol.LeaderError) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(errorWriteTimeout))
	defer func() { _ = conn.SetWriteDeadline(time.Time{}) }()
	return w.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameError, Payload: payload})
}

// RemoteError is an error frame that the leader sent.
type RemoteError struct{ protocol.LeaderError }

func (e *RemoteError) Error() string { return "leader refused: " + e.Kind + ": " + e.Message }

// Registered is a client connection that completed the handshake.
type Registered struct {
	Info   protocol.LeaderRegistered
	Reader *FrameReader
	Writer *FrameWriter
	conn   net.Conn
}

// Register runs the client side of the handshake. It returns a RemoteError when
// the leader refuses the client.
func Register(conn net.Conn, hello protocol.LeaderRegister, timeout time.Duration) (*Registered, error) {
	_ = conn.SetDeadline(time.Now().Add(timeout))
	defer func() { _ = conn.SetDeadline(time.Time{}) }()
	w := NewFrameWriter(conn, protocol.LeaderMaxFrame)
	r := NewFrameReader(conn, protocol.LeaderMaxFrame)
	payload, err := json.Marshal(hello)
	if err != nil {
		return nil, err
	}
	if err := w.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameRegister, Payload: payload}); err != nil {
		return nil, err
	}
	frame, err := r.Next()
	if err != nil {
		return nil, err
	}
	switch frame.Type {
	case protocol.LeaderFrameRegistered:
		var info protocol.LeaderRegistered
		if err := json.Unmarshal(frame.Payload, &info); err != nil {
			return nil, fmt.Errorf("%w: registered payload: %v", ErrFrameInvalid, err)
		}
		return &Registered{Info: info, Reader: r, Writer: w, conn: conn}, nil
	case protocol.LeaderFrameError:
		var e protocol.LeaderError
		if err := json.Unmarshal(frame.Payload, &e); err != nil {
			return nil, fmt.Errorf("%w: error payload: %v", ErrFrameInvalid, err)
		}
		return nil, &RemoteError{e}
	default:
		return nil, fmt.Errorf("%w: unexpected %q frame during register", ErrFrameInvalid, frame.Type)
	}
}

// AwaitReady waits for the leader_ready frame when registered said not ready.
func (r *Registered) AwaitReady(timeout time.Duration) error {
	_ = r.conn.SetReadDeadline(time.Now().Add(timeout))
	defer func() { _ = r.conn.SetReadDeadline(time.Time{}) }()
	frame, err := r.Reader.Next()
	if err != nil {
		return err
	}
	if frame.Type != protocol.LeaderFrameReady {
		return fmt.Errorf("%w: expected leader_ready, got %q", ErrFrameInvalid, frame.Type)
	}
	r.Info.Ready = true
	return nil
}
