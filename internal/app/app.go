// Package app is the composition root. It builds every package with fx,
// binds implementations to interfaces, and starts and stops the servers.
package app

import (
	"context"
	"errors"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"AskCore/internal/config"
	"AskCore/internal/gateway"
	httpapi "AskCore/internal/http"
	"AskCore/internal/logs"
	"AskCore/internal/migrations"
	"AskCore/internal/store"
	"AskCore/internal/store/gormstore"
)

// Module is the full server application.
var Module = fx.Options(
	fx.Provide(
		config.Load,
		newLogger,
		newDB,
		gormstore.NewStores,
		func(s *store.Stores) store.UserStore { return s.Users },
		func(s *store.Stores) store.PostStore { return s.Posts },
		newGatewayConfig,
		asRoute(httpapi.NewHealthHandler),
		asRoute(httpapi.NewUserHandler),
		asRoute(httpapi.NewPostHandler),
		fx.Annotate(gateway.NewHTTPServer, fx.ParamTags(``, ``, `group:"routes"`)),
		gateway.NewGreeterService,
		gateway.NewGRPCServer,
	),
	fx.WithLogger(func(logger *zap.Logger) fxevent.Logger {
		return &fxevent.ZapLogger{Logger: logger}
	}),
	// Migrations run before the servers start.
	fx.Invoke(runMigrations, startServers),
)

// asRoute adds an HTTP handler constructor to the "routes" group.
func asRoute(constructor any) any {
	return fx.Annotate(
		constructor,
		fx.As(new(gateway.RouteRegistrar)),
		fx.ResultTags(`group:"routes"`),
	)
}

func newLogger(lc fx.Lifecycle, cfg *config.Config) (*zap.Logger, error) {
	logger, err := logs.New(cfg.LogLevel)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.StopHook(func() { _ = logger.Sync() }))
	return logger, nil
}

func newDB(lc fx.Lifecycle, cfg *config.Config) (*gorm.DB, error) {
	db, err := gormstore.Open(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	lc.Append(fx.StopHook(sqlDB.Close))
	return db, nil
}

func newGatewayConfig(cfg *config.Config) gateway.Config {
	return gateway.Config{
		Host:       cfg.Host,
		Port:       cfg.Port,
		GRPCPort:   cfg.GRPCPort,
		CORSOrigin: cfg.CORSOrigin,
	}
}

func runMigrations(cfg *config.Config) error {
	return migrations.Up(cfg.DatabaseURL)
}

func startServers(lc fx.Lifecycle, httpServer *gateway.HTTPServer, grpcServer *gateway.GRPCServer) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if err := grpcServer.Start(); err != nil {
				return err
			}
			return httpServer.Start()
		},
		OnStop: func(ctx context.Context) error {
			return errors.Join(httpServer.Stop(ctx), grpcServer.Stop(ctx))
		},
	})
}
