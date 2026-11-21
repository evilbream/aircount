package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"poltergeist/internal/config"
	"poltergeist/internal/platform"
	"poltergeist/internal/system"
	"poltergeist/internal/tplink"
	"syscall"

	"github.com/rs/zerolog/log"
)

func main() {
	if platform.Detect() == platform.Local {
		if err := system.EnsureEnvLoaded(".env.local"); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msg("failed to load .env.local")
		}
	}

	platform.SetupLogging()

	cfg := config.LoadRouter()
	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("invalid router configuration")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := tplink.New(cfg.Host, cfg.Username, cfg.Password)

	if err := client.Login(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to login to tplink router")
	}
	log.Info().Str("router", cfg.Host).Msg("Successfully logged into router")

}
