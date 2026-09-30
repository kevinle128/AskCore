package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	t.Chdir(t.TempDir()) // no config.yaml
	for _, name := range []string{"HOST", "PORT", "GRPC_PORT", "DATABASE_URL", "LOG_LEVEL", "CORS_ORIGIN", "REDIS_URL", "REDIS_ADDR"} {
		t.Setenv(name, "")
	}

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0", cfg.Host)
	require.Equal(t, 8080, cfg.Port)
	require.Equal(t, 50051, cfg.GRPCPort)
	require.Equal(t, "app.db", cfg.DatabaseURL)
	require.Equal(t, "*", cfg.CORSOrigin)
}

func TestLoad_EnvNamesFromEnvExample(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "9000")
	t.Setenv("GRPC_PORT", "50052")
	t.Setenv("DATABASE_URL", "test.db")
	t.Setenv("LOG_LEVEL", "production")
	t.Setenv("CORS_ORIGIN", "http://localhost:3001")
	t.Setenv("REDIS_URL", "redis://127.0.0.1:6379")
	t.Setenv("REDIS_ADDR", "127.0.0.1:6379")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, Config{
		Host:        "127.0.0.1",
		Port:        9000,
		GRPCPort:    50052,
		DatabaseURL: "test.db",
		LogLevel:    "production",
		CORSOrigin:  "http://localhost:3001",
		RedisURL:    "redis://127.0.0.1:6379",
		RedisAddr:   "127.0.0.1:6379",
	}, *cfg)
}
