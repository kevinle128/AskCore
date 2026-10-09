package protocol

import "encoding/json"

// LeaderProtocolVersion is the outer version of the local leader socket. Both
// sides must send the same value. It is separate from the ACP wire version,
// the SDK version and the build identity.
const LeaderProtocolVersion = 1

// LeaderMaxFrame is the largest encoded frame on the leader socket.
const LeaderMaxFrame = 64 << 20

// Frame types of the leader socket.
const (
	LeaderFrameRegister     = "register"
	LeaderFrameRegistered   = "registered"
	LeaderFrameReady        = "leader_ready"
	LeaderFrameACP          = "acp"
	LeaderFrameControl      = "control"
	LeaderFrameControlReply = "control_result"
	LeaderFramePing         = "ping"
	LeaderFramePong         = "pong"
	LeaderFrameDisconnect   = "disconnect"
	LeaderFrameError        = "error"
)

// Control commands a client can send in a control frame.
const (
	LeaderControlStatus   = "status"
	LeaderControlShutdown = "shutdown"
)

// Error kinds of the leader.
const (
	LeaderErrVersionMismatch = "leader_version_mismatch"
	LeaderErrPeerRejected    = "leader_peer_rejected"
	LeaderErrFrameTooLarge   = "leader_frame_too_large"
	LeaderErrNotInitialized  = "leader_not_initialized"
	LeaderErrNotDriver       = "leader_not_driver"
	LeaderErrShuttingDown    = "leader_shutting_down"
	LeaderErrControlRefused  = "leader_control_refused"
)

// Side that must upgrade after a version mismatch.
const (
	LeaderUpgradeClient = "client"
	LeaderUpgradeLeader = "leader"
)

// LeaderFrame is the envelope of every message on the leader socket. An ACP
// payload stays raw nested JSON.
type LeaderFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// LeaderRegister is the first frame a client sends.
type LeaderRegister struct {
	ClientKind      string `json:"clientKind"`
	ProtocolVersion int    `json:"protocolVersion"`
	Build           string `json:"build"`
}

// LeaderRegistered answers a register frame.
type LeaderRegistered struct {
	ClientID        string   `json:"clientId"`
	InstanceID      string   `json:"instanceId"`
	Build           string   `json:"build"`
	ProtocolVersion int      `json:"protocolVersion"`
	Ready           bool     `json:"ready"`
	Controls        []string `json:"controls,omitempty"`
}

// LeaderError reports a refused request. Upgrade names the side that must
// change after a version mismatch.
type LeaderError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Upgrade string `json:"upgrade,omitempty"`
}

// LeaderControl is the payload of a control frame.
type LeaderControl struct {
	Command string `json:"command"`
	// InstanceID, when set, must match the leader instance or the command is refused.
	InstanceID string `json:"instanceId,omitempty"`
	// IfIdle makes a shutdown conditional: the leader stops only when nothing is active.
	IfIdle bool `json:"ifIdle,omitempty"`
}

// LeaderStatus is the payload of a status reply. It holds counts and identity
// only: no prompt, working directory, tool argument or credential.
type LeaderStatus struct {
	InstanceID      string `json:"instanceId"`
	Build           string `json:"build"`
	ProtocolVersion int    `json:"protocolVersion"`
	PID             int    `json:"pid"`
	Clients         int    `json:"clients"`
	Sessions        int    `json:"sessions"`
	ActiveRuns      int    `json:"activeRuns"`
	SpawnedByClient bool   `json:"spawnedByClient"`
}

// VersionInfo is the output of `ask version --json`. A client reads it before it
// starts a leader from that binary.
type VersionInfo struct {
	Name           string `json:"name"`
	Build          string `json:"build"`
	LeaderProtocol int    `json:"leaderProtocol"`
	ACPVersion     int    `json:"acpVersion"`
}
