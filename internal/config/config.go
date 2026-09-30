// Package config loads application configuration with Viper.
//
// It reads config.yaml from the working directory (optional) and overlays
// environment variables. The environment variable names are the upper-case
// keys, the same names as in .env.example (for example PORT, GRPC_PORT,
// DATABASE_URL).
//
// Only internal/app and cmd/* import this package. Other packages receive
// typed values through their constructors.
package config

import (
	"errors"

	"github.com/spf13/viper"
)

// Config holds the application settings.
type Config struct {
	Host        string `mapstructure:"host"`
	Port        int    `mapstructure:"port"`
	GRPCPort    int    `mapstructure:"grpc_port"`
	DatabaseURL string `mapstructure:"database_url"`
	LogLevel    string `mapstructure:"log_level"`
	CORSOrigin  string `mapstructure:"cors_origin"`
	RedisURL    string `mapstructure:"redis_url"`
	RedisAddr   string `mapstructure:"redis_addr"`
}

// Load reads config.yaml (if present) and environment variables.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	// Key "grpc_port" reads the environment variable GRPC_PORT.
	v.AutomaticEnv()

	// Every key needs a default, so that Unmarshal sees environment values.
	v.SetDefault("host", "0.0.0.0")
	v.SetDefault("port", 8080)
	v.SetDefault("grpc_port", 50051)
	v.SetDefault("database_url", "app.db")
	v.SetDefault("log_level", "")
	v.SetDefault("cors_origin", "*")
	v.SetDefault("redis_url", "")
	v.SetDefault("redis_addr", "")

	if err := v.ReadInConfig(); err != nil {
		// A missing config file is fine; env vars and defaults still apply.
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
