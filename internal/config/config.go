// Package config loads application configuration with Viper.
//
// Reads config.yaml from the working directory (optional) and overlays
// environment variables, so APP_SERVER_PORT overrides server.port.
package config

import (
	"strings"

	"github.com/spf13/viper"
)

// Config holds the application settings.
type Config struct {
	Server struct {
		Host string `mapstructure:"host"`
		Port int    `mapstructure:"port"`
	} `mapstructure:"server"`
}

// Load reads config.yaml (if present) and environment variables.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 3000)

	if err := v.ReadInConfig(); err != nil {
		// A missing config file is fine; env vars and defaults still apply.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
