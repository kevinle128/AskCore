package gateway

import (
	"context"
	"errors"
	"net"
	"strconv"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"AskCore/proto"
)

// GRPCServer serves the gRPC services.
type GRPCServer struct {
	server *grpc.Server
	addr   string
	logger *zap.Logger
}

// NewGRPCServer builds the gRPC server and registers the services.
func NewGRPCServer(cfg Config, logger *zap.Logger, greeter *GreeterService) *GRPCServer {
	server := grpc.NewServer()
	proto.RegisterGreeterServer(server, greeter)
	return &GRPCServer{
		server: server,
		addr:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.GRPCPort)),
		logger: logger,
	}
}

// Start listens on the address and serves in the background. It returns an
// error at once when the address is not available.
func (s *GRPCServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.logger.Info("Starting gRPC server", zap.String("address", s.addr))
	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			s.logger.Error("gRPC server stopped", zap.Error(err))
		}
	}()
	return nil
}

// Stop stops the server gracefully. When ctx ends first, it stops at once.
func (s *GRPCServer) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.server.Stop()
		return ctx.Err()
	}
}
