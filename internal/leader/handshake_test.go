package leader

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

type acceptResult struct {
	acc *Accepted
	err error
}

func serve(server net.Conn, cfg ServerConfig) <-chan acceptResult {
	out := make(chan acceptResult, 1)
	go func() {
		acc, err := Accept(server, cfg, "c1")
		out <- acceptResult{acc, err}
	}()
	return out
}

func baseConfig() ServerConfig {
	return ServerConfig{InstanceID: "inst-1", Build: "dev", Ready: true, Controls: []string{"status", "shutdown"}, RegisterTimeout: 2 * time.Second}
}

func hello(version int) protocol.LeaderRegister {
	return protocol.LeaderRegister{ClientKind: "connect", ProtocolVersion: version, Build: "dev"}
}

func TestHandshakeAccepts(t *testing.T) {
	server, client := socketPair(t)
	done := serve(server, baseConfig())

	reg, err := Register(client, hello(protocol.LeaderProtocolVersion), 2*time.Second)
	require.NoError(t, err)
	require.Equal(t, protocol.LeaderRegistered{
		ClientID: "c1", InstanceID: "inst-1", Build: "dev", ProtocolVersion: 1, Ready: true, Controls: []string{"status", "shutdown"},
	}, reg.Info)

	res := <-done
	require.NoError(t, res.err)
	require.False(t, res.acc.Management)
	require.Equal(t, "connect", res.acc.Hello.ClientKind)

	// The pair is ready for traffic in both directions.
	require.NoError(t, reg.Writer.Write(protocol.LeaderFrame{Type: protocol.LeaderFramePing}))
	f, err := res.acc.Reader.Next()
	require.NoError(t, err)
	require.Equal(t, protocol.LeaderFramePing, f.Type)
}

func TestHandshakeReadyArrivesLater(t *testing.T) {
	server, client := socketPair(t)
	cfg := baseConfig()
	cfg.Ready = false
	done := serve(server, cfg)

	reg, err := Register(client, hello(1), 2*time.Second)
	require.NoError(t, err)
	require.False(t, reg.Info.Ready)

	res := <-done
	require.NoError(t, res.err)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = res.acc.Writer.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameReady})
	}()
	require.NoError(t, reg.AwaitReady(2*time.Second))
}

func TestHandshakeAwaitReadyTimesOut(t *testing.T) {
	server, client := socketPair(t)
	cfg := baseConfig()
	cfg.Ready = false
	done := serve(server, cfg)
	reg, err := Register(client, hello(1), 2*time.Second)
	require.NoError(t, err)
	<-done
	err = reg.AwaitReady(50 * time.Millisecond)
	require.Error(t, err)
	var ne net.Error
	require.True(t, errors.As(err, &ne) && ne.Timeout())
}

func TestHandshakeRejectsVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		version int
		upgrade string
	}{
		"older client": {version: 0, upgrade: protocol.LeaderUpgradeClient},
		"newer client": {version: 2, upgrade: protocol.LeaderUpgradeLeader},
	} {
		t.Run(name, func(t *testing.T) {
			server, client := socketPair(t)
			done := serve(server, baseConfig())

			_, err := Register(client, hello(tc.version), 2*time.Second)
			var remote *RemoteError
			require.True(t, errors.As(err, &remote))
			require.Equal(t, protocol.LeaderErrVersionMismatch, remote.Kind)
			require.Equal(t, tc.upgrade, remote.Upgrade)
			require.NotEmpty(t, remote.Message)

			res := <-done
			require.ErrorIs(t, res.err, ErrVersionMismatch, "a caller cannot take a mismatch for success")
			require.True(t, res.acc.Management, "a mismatch leaves a management-only connection")

			// An ACP frame on that connection is refused and never delivered.
			w := NewFrameWriter(client, protocol.LeaderMaxFrame)
			require.NoError(t, w.Write(acpFrame(`{"jsonrpc":"2.0","id":1,"method":"session/new"}`)))
			_, err = res.acc.NextManagement(time.Second)
			require.ErrorIs(t, err, ErrFrameInvalid)
		})
	}
}

func TestHandshakeMismatchAllowsStatusAndShutdownOnly(t *testing.T) {
	server, client := socketPair(t)
	done := serve(server, baseConfig())
	_, err := Register(client, hello(9), 2*time.Second)
	require.Error(t, err)
	res := <-done
	require.ErrorIs(t, res.err, ErrVersionMismatch)

	payload, _ := json.Marshal(protocol.LeaderControl{Command: protocol.LeaderControlStatus})
	require.NoError(t, NewFrameWriter(client, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: payload}))
	ctl, err := res.acc.NextManagement(time.Second)
	require.NoError(t, err)
	require.Equal(t, protocol.LeaderControlStatus, ctl.Command)

	payload, _ = json.Marshal(protocol.LeaderControl{Command: "restart"})
	require.NoError(t, NewFrameWriter(client, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: payload}))
	_, err = res.acc.NextManagement(time.Second)
	require.ErrorIs(t, err, ErrFrameInvalid)
}

func TestHandshakeManagementIsBounded(t *testing.T) {
	server, client := socketPair(t)
	done := serve(server, baseConfig())
	_, err := Register(client, hello(9), 2*time.Second)
	require.Error(t, err)
	res := <-done
	require.ErrorIs(t, res.err, ErrVersionMismatch)
	_, err = res.acc.NextManagement(50 * time.Millisecond)
	var ne net.Error
	require.True(t, errors.As(err, &ne) && ne.Timeout(), "an idle management connection ends")
}

func TestHandshakeDeadline(t *testing.T) {
	server, client := socketPair(t)
	cfg := baseConfig()
	cfg.RegisterTimeout = 50 * time.Millisecond
	res := <-serve(server, cfg)
	require.Error(t, res.err)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := client.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF, "the silent client is closed")
}

func TestHandshakeRejectsOtherUIDBeforeReading(t *testing.T) {
	server, client := socketPair(t)
	other := uint32(os.Geteuid()) + 1
	cfg := baseConfig()
	cfg.OwnerUID = &other
	done := serve(server, cfg)

	_, err := Register(client, hello(1), 2*time.Second)
	var remote *RemoteError
	require.True(t, errors.As(err, &remote))
	require.Equal(t, protocol.LeaderErrPeerRejected, remote.Kind)

	res := <-done
	require.ErrorIs(t, res.err, ErrPeerRejected)
	require.Nil(t, res.acc)
}

func TestHandshakeFirstFrameMustBeRegister(t *testing.T) {
	server, client := socketPair(t)
	done := serve(server, baseConfig())
	require.NoError(t, NewFrameWriter(client, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFramePing}))
	res := <-done
	require.ErrorIs(t, res.err, ErrFrameInvalid)
}
