// Command server runs the Ask daemon: the HTTP and gRPC servers.
//
// Usage:
//
//	server            apply pending migrations, then start the servers
//	server -migrate   apply pending migrations and exit
package main

import (
	"flag"
	"log"

	"github.com/joho/godotenv"
	"go.uber.org/fx"

	"AskCore/internal/app"
	"AskCore/internal/config"
	"AskCore/internal/migrations"
)

func main() {
	migrateOnly := flag.Bool("migrate", false, "apply pending database migrations and exit")
	flag.Parse()

	// A missing .env file is fine; environment variables and defaults still apply.
	_ = godotenv.Load()

	if *migrateOnly {
		cfg, err := config.Load()
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		if err := migrations.Up(cfg.DatabaseURL); err != nil {
			log.Fatalf("apply migrations: %v", err)
		}
		return
	}

	fx.New(app.Module).Run()
}
