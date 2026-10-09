package leader

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"AskCore/pkg/protocol"

	"go.uber.org/zap"
)

const (
	defaultLinkTimeout = 10 * time.Second
	// flushGrace is how long Close lets a client socket take its last frames.
	flushGrace = time.Second
	// managementWait bounds a connection that the version gate left management-only.
	managementWait = 5 * time.Second
	// maxManagementFrames bounds the frames a management-only connection may send.
	maxManagementFrames = 8
)

// Config is the typed configuration of a leader server.
type Config struct {
	// Server is the handshake configuration. Ready is always true: the server
	// accepts clients only after the link initialize has finished.
	Server ServerConfig
	// Agent is the ACP stream to the agent, one JSON message for each line.
	// The server owns it and closes it in Close. Closing it must also end the read side.
	Agent io.ReadWriteCloser
	// Logger may be nil.
	Logger *zap.Logger
	// PID and SpawnedByClient go into the status reply.
	PID             int
	SpawnedByClient bool
	// ActiveRuns reports the runs that are active now. It may be nil.
	ActiveRuns func() int
	// QuiesceIfIdle closes admission and reports true, but only when nothing is
	// active. It may be nil, which means the leader never reports idle.
	QuiesceIfIdle func() bool
	// LinkTimeout bounds the initialize call on the agent link. Zero means 10 seconds.
	LinkTimeout time.Duration
}

// Server is the leader: it routes many clients to one agent stream.
type Server struct {
	cfg Config
	log *zap.Logger
	r   *router

	wg      sync.WaitGroup
	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	lns     []net.Listener
	nextID  uint64
	started bool
	once    sync.Once
	closed  chan struct{}
}

// NewServer returns a server that is not running.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Agent == nil {
		return nil, errors.New("leader: no agent stream")
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	cfg.Server.Ready = true
	if cfg.LinkTimeout == 0 {
		cfg.LinkTimeout = defaultLinkTimeout
	}
	s := &Server{cfg: cfg, log: cfg.Logger, conns: map[net.Conn]struct{}{}, closed: make(chan struct{})}
	s.r = newRouter(cfg, newQueue[[]byte]())
	return s, nil
}

// Start runs the router and does the link initialize. It returns when the agent
// has answered, so the server is ready for clients.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("leader: server already started")
	}
	s.started = true
	s.mu.Unlock()

	go s.r.run()
	s.goAgentReader()
	s.goAgentWriter()

	failed := make(chan error, 1)
	if !s.r.call(func() {
		s.r.internalRequest("initialize", map[string]any{
			"protocolVersion":    1,
			"clientCapabilities": map[string]any{},
			"clientInfo":         map[string]any{"name": "ask-leader", "version": s.cfg.Server.Build},
		}, func(m *rpcMessage) {
			if m.fields["error"] != nil || m.fields["result"] == nil {
				failed <- errors.New("leader: the agent refused initialize")
				return
			}
			s.r.initResult = m.fields["result"]
			close(s.r.linkReady)
		})
	}) {
		return errors.New("leader: router stopped")
	}
	select {
	case <-s.r.linkReady:
		return nil
	case err := <-failed:
		_ = s.Close()
		return err
	case <-time.After(s.cfg.LinkTimeout):
		_ = s.Close()
		return errors.New("leader: the agent did not answer initialize in time")
	case <-s.r.done:
		return fmt.Errorf("leader: the agent link closed during initialize: %w", s.r.linkErr)
	}
}

// Done is closed when the router has stopped, after Close or when the agent link failed.
func (s *Server) Done() <-chan struct{} { return s.r.done }

// Err returns why the router stopped, when the agent link failed.
func (s *Server) Err() error {
	select {
	case <-s.r.done:
		return s.r.linkErr
	default:
		return nil
	}
}

func (s *Server) goAgentReader() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		lr := NewLineReader(s.cfg.Agent, MaxLine)
		for {
			line, err := lr.Next()
			if err != nil {
				select {
				case <-s.closed:
				default:
					if errors.Is(err, io.EOF) {
						err = errors.New("the agent closed its stream")
					}
					s.r.post(agentGoneEvent{err: err})
				}
				return
			}
			if !s.r.post(agentLineEvent{line: line}) {
				return
			}
		}
	}()
}

func (s *Server) goAgentWriter() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		lw := NewLineWriter(s.cfg.Agent, MaxLine)
		for {
			msg, ok := s.r.agentQ.pop()
			if !ok {
				return
			}
			err := lw.Write(msg)
			switch {
			case err == nil:
			case errors.Is(err, ErrLineTooLong) || errors.Is(err, ErrFrameInvalid):
				// One bad message is not a broken link.
				s.log.Error("a message for the agent was dropped", zap.Error(err))
			default:
				select {
				case <-s.closed:
				default:
					s.r.post(agentGoneEvent{err: err})
				}
				return
			}
		}
	}()
}

// Serve accepts clients on ln until ln is closed or the server stops.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	select {
	case <-s.closed:
		s.mu.Unlock()
		_ = ln.Close()
		return net.ErrClosed
	default:
	}
	s.lns = append(s.lns, ln)
	s.mu.Unlock()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if !s.track(conn) {
			_ = conn.Close()
			return nil
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return false
	default:
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func (s *Server) nextClientID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return "c" + strconv.FormatUint(s.nextID, 10)
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.untrack(conn)
	acc, err := Accept(conn, s.cfg.Server, s.nextClientID())
	if errors.Is(err, ErrVersionMismatch) {
		s.serveManagement(acc)
		_ = conn.Close()
		return
	}
	if err != nil {
		s.log.Debug("client refused", zap.Error(err))
		return
	}
	c := newClient(acc)
	if !s.r.post(addClientEvent{c: c}) {
		_ = conn.Close()
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.writeLoop(c)
	}()
	s.readLoop(c)
}

// readLoop is the only reader of a client socket.
func (s *Server) readLoop(c *client) {
	defer func() {
		_ = c.acc.Conn().Close()
		// The writer ends with the reader. The router may never have seen this
		// client (it stopped first), and then nobody else would close the queue.
		c.q.close()
		s.r.post(clientGoneEvent{c: c})
	}()
	for {
		f, err := c.acc.Reader.Next()
		if err != nil {
			return
		}
		switch f.Type {
		case protocol.LeaderFrameACP:
			if !s.r.post(clientMsgEvent{c: c, raw: f.Payload}) {
				return
			}
		case protocol.LeaderFramePing:
			c.q.push(outItem{frame: protocol.LeaderFrame{Type: protocol.LeaderFramePong}})
		case protocol.LeaderFrameDisconnect:
			return
		case protocol.LeaderFrameControl:
			var ctl protocol.LeaderControl
			if json.Unmarshal(f.Payload, &ctl) != nil {
				return
			}
			c.q.push(outItem{frame: s.controlFrame(ctl, false)})
		default:
			return // a frame that only the leader sends
		}
	}
}

// writeLoop is the only writer of a client socket. It sends the frames in order.
func (s *Server) writeLoop(c *client) {
	defer func() {
		_ = c.acc.Conn().Close()
		s.r.post(clientGoneEvent{c: c})
	}()
	for {
		it, ok := c.q.pop()
		if !ok || it.close {
			return
		}
		if err := c.acc.Writer.Write(it.frame); err != nil {
			// Only this client is lost. The agent link and the other clients are not touched.
			s.log.Debug("client write failed", zap.String("client", c.id), zap.Error(err))
			return
		}
	}
}

// serveManagement serves a connection that the version gate refused. It allows
// a few status or conditional shutdown requests and never ACP.
func (s *Server) serveManagement(acc *Accepted) {
	for range maxManagementFrames {
		ctl, err := acc.NextManagement(managementWait)
		if err != nil {
			return
		}
		frame := s.controlFrame(ctl, true)
		_ = acc.Conn().SetWriteDeadline(time.Now().Add(errorWriteTimeout))
		err = acc.Writer.Write(frame)
		_ = acc.Conn().SetWriteDeadline(time.Time{})
		if err != nil || frame.Type == protocol.LeaderFrameError {
			return
		}
	}
}

// controlFrame answers a control request. A management-only connection may stop
// the leader only when it is idle.
func (s *Server) controlFrame(ctl protocol.LeaderControl, management bool) protocol.LeaderFrame {
	refuse := func(msg string) protocol.LeaderFrame {
		return protocol.LeaderFrame{Type: protocol.LeaderFrameError, Payload: marshalPlain(protocol.LeaderError{Kind: protocol.LeaderErrControlRefused, Message: msg})}
	}
	if ctl.InstanceID != "" && ctl.InstanceID != s.cfg.Server.InstanceID {
		return refuse("the instance id does not match this leader")
	}
	switch ctl.Command {
	case protocol.LeaderControlStatus:
		var st protocol.LeaderStatus
		if !s.r.call(func() { st = s.r.status() }) {
			return refuse("the leader is stopping")
		}
		return protocol.LeaderFrame{Type: protocol.LeaderFrameControlReply, Payload: marshalPlain(st)}
	case protocol.LeaderControlShutdown:
		if management && !ctl.IfIdle {
			return refuse("a shutdown from another version needs ifIdle")
		}
		if ctl.IfIdle && !s.cfg.SpawnedByClient {
			return refuse("the leader runs under a supervisor and is not replaced by a client")
		}
		if ctl.IfIdle {
			quiet := false
			if !s.r.call(func() { quiet = s.r.quiesce() }) || !quiet {
				return refuse("the leader is not idle")
			}
		}
		go func() { _ = s.Close() }()
		return protocol.LeaderFrame{Type: protocol.LeaderFrameControlReply, Payload: marshalPlain(map[string]any{"ok": true})}
	}
	return refuse("unknown control command")
}

// Close stops the server. It tells the clients, lets their sockets take the
// last frames for a short time, closes them and closes the agent stream.
func (s *Server) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		close(s.closed)
		lns := s.lns
		started := s.started
		s.mu.Unlock()
		for _, ln := range lns {
			_ = ln.Close()
		}
		if started {
			close(s.r.stop)
			<-s.r.done
		}
		s.r.agentQ.close()
		_ = s.cfg.Agent.Close()

		finished := make(chan struct{})
		go func() { s.wg.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(flushGrace):
			s.mu.Lock()
			for conn := range s.conns {
				_ = conn.Close()
			}
			s.mu.Unlock()
			<-finished
		}
	})
	return nil
}

// ---- client ----

type outItem struct {
	frame   protocol.LeaderFrame
	session string
	gen     uint64
	close   bool
}

// client is one connected client. The router goroutine owns the state fields.
// The queue is safe to use from any goroutine.
type client struct {
	id  string
	acc *Accepted
	q   *queue[outItem]

	initialized bool
	caps        json.RawMessage
	members     map[string]member
	closed      bool
}

func newClient(acc *Accepted) *client {
	return &client{id: acc.ClientID, acc: acc, q: newQueue[outItem](), members: map[string]member{}}
}

func (c *client) sendACP(raw json.RawMessage, session string, gen uint64) {
	c.q.push(outItem{frame: protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: raw}, session: session, gen: gen})
}

func (c *client) sendError(e protocol.LeaderError) {
	c.q.push(outItem{frame: protocol.LeaderFrame{Type: protocol.LeaderFrameError, Payload: marshalPlain(e)}})
}

// finish closes the client socket after the frames that are already queued.
func (c *client) finish() { c.q.push(outItem{close: true}) }

// purge drops the queued frames of one membership.
func (c *client) purge(session string, gen uint64) {
	c.q.purge(func(it outItem) bool { return it.session == session && it.gen == gen })
}
