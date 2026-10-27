package gateway

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"AskCore/proto"
)

func TestGreeterRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	proto.RegisterGreeterServer(server, NewGreeterService())
	go func() {
		if err := server.Serve(listener); err != nil {
			t.Errorf("serve gRPC: %v", err)
		}
	}()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })

	client := proto.NewGreeterClient(connection)

	response, err := client.SayHello(context.Background(), &proto.HelloRequest{Name: "Better Fullstack"})
	require.NoError(t, err)
	require.Equal(t, "Hello, Better Fullstack!", response.GetMessage())

	response, err = client.SayHello(context.Background(), &proto.HelloRequest{})
	require.NoError(t, err)
	require.Equal(t, "Hello, World!", response.GetMessage())
}

// cancelStream is a greeting stream whose client cancels after the first
// greeting.
type cancelStream struct {
	grpc.ServerStream
	ctx    context.Context
	cancel context.CancelFunc
	sent   int
}

func (s *cancelStream) Context() context.Context { return s.ctx }

func (s *cancelStream) Send(*proto.HelloReply) error {
	s.sent++
	s.cancel()
	return nil
}

func TestGreeterStreamStopsWhenClientCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &cancelStream{ctx: ctx, cancel: cancel}

	start := time.Now()
	err := NewGreeterService().SayHelloStream(&proto.HelloRequest{Name: "x"}, stream)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, stream.sent)
	require.Less(t, time.Since(start), greetingInterval, "the stream waited out its pace after the cancel")
}
