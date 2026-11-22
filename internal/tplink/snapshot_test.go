package tplink

import (
	"poltergeist/internal/config"
	"poltergeist/internal/system"
	"testing"

	"github.com/rs/zerolog/log"
)

func TestSnapshot(t *testing.T) {
	system.LoadLocalEnv()
	cfg := config.LoadRouter()
	client := New(cfg.Host, cfg.Username, cfg.Password)

	if err := client.Login(t.Context()); err != nil {
		log.Fatal().Err(err).Msg("failed to login to tplink router")
	}

	clients, err := client.GetClientsInfo(t.Context())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get clients")
	}

	log.Info().Interface("clients", clients).Msg("clients received")
	log.Info().Str("router", cfg.Host).Msg("Successfully logged into router")

}
