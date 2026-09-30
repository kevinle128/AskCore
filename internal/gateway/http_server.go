package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

// RouteRegistrar adds routes to the HTTP server. Handlers in internal/http
// implement it; internal/app collects them in the fx group "routes".
type RouteRegistrar interface {
	Register(e *echo.Echo)
}

// HTTPServer is the echo server with middleware and all registered routes.
type HTTPServer struct {
	echo   *echo.Echo
	addr   string
	logger *zap.Logger
}

// NewHTTPServer builds the echo server and registers the routes.
func NewHTTPServer(cfg Config, logger *zap.Logger, routes []RouteRegistrar) *HTTPServer {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:   true,
		LogURI:      true,
		LogStatus:   true,
		LogLatency:  true,
		LogError:    true,
		HandleError: true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			logger.Info("http request",
				zap.String("method", v.Method),
				zap.String("uri", v.URI),
				zap.Int("status", v.Status),
				zap.Duration("latency", v.Latency),
				zap.Error(v.Error),
			)
			return nil
		},
	}))
	e.Use(middleware.Recover())
	// CORS: pinned to CORS_ORIGIN when set, permissive otherwise.
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{cfg.CORSOrigin},
	}))

	for _, r := range routes {
		r.Register(e)
	}

	return &HTTPServer{
		echo:   e,
		addr:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		logger: logger,
	}
}

// Handler returns the HTTP handler, for tests.
func (s *HTTPServer) Handler() http.Handler {
	return s.echo
}

// Start listens on the address and serves in the background. It returns an
// error at once when the address is not available.
func (s *HTTPServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.echo.Listener = listener
	s.logger.Info("Starting HTTP server", zap.String("address", s.addr))
	go func() {
		if err := s.echo.Start(s.addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("HTTP server stopped", zap.Error(err))
		}
	}()
	return nil
}

// Stop shuts the server down gracefully.
func (s *HTTPServer) Stop(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}
