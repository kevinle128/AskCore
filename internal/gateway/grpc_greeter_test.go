package gateway

import (
	"context"
	"net"
	"testing"

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
