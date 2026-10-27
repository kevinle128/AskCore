package gateway

import (
	"context"
	"fmt"
	"time"

	"AskCore/proto"
)

// GreeterService implements the Greeter gRPC service from proto/greeter.proto.
type GreeterService struct {
	proto.UnimplementedGreeterServer
}

// NewGreeterService returns a GreeterService.
func NewGreeterService() *GreeterService {
	return &GreeterService{}
}

// SayHello implements the Greeter.SayHello RPC.
func (s *GreeterService) SayHello(ctx context.Context, req *proto.HelloRequest) (*proto.HelloReply, error) {
	return &proto.HelloReply{Message: fmt.Sprintf("Hello, %s!", greetName(req))}, nil
}

// greetingInterval paces the greetings of SayHelloStream.
const greetingInterval = 500 * time.Millisecond

// SayHelloStream implements the Greeter.SayHelloStream RPC. It sends five
// greetings with a short delay and stops when the client cancels the stream.
func (s *GreeterService) SayHelloStream(req *proto.HelloRequest, stream proto.Greeter_SayHelloStreamServer) error {
	name := greetName(req)
	pace := time.NewTimer(greetingInterval)
	defer pace.Stop()
	for i := 1; i <= 5; i++ {
		if err := stream.Send(&proto.HelloReply{Message: fmt.Sprintf("Hello #%d, %s!", i, name)}); err != nil {
			return err
		}
		if i == 5 {
			break
		}
		pace.Reset(greetingInterval)
		select {
		case <-pace.C:
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
	return nil
}

func greetName(req *proto.HelloRequest) string {
	if name := req.GetName(); name != "" {
		return name
	}
	return "World"
}
