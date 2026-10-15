// Package logs builds the zap logger the composition root hands to other packages.
package logs

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New builds a zap logger from a LOG_LEVEL value.
// An empty level and "debug" use the development config.
// "production" uses the production config.
// Any other value is a zap level name on the development config.
func New(level string) (*zap.Logger, error) {
	switch level {
	case "", "debug":
		return zap.NewDevelopment()
	case "production":
		return zap.NewProduction()
	default:
		var lvl zapcore.Level
		if err := lvl.UnmarshalText([]byte(level)); err != nil {
			return nil, fmt.Errorf("log level %q: %w", level, err)
		}
		cfg := zap.NewDevelopmentConfig()
		cfg.Level = zap.NewAtomicLevelAt(lvl)
		return cfg.Build()
	}
}
