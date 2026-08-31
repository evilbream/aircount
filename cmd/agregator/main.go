package main

import (
	"errors"
	"os"
	"poltergeist/internal/platform"
	"poltergeist/internal/system"

	"github.com/rs/zerolog/log"
)

func main() {
	if platform.Detect() == platform.Local {
		if err := system.EnsureEnvLoaded(".env.local"); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msg("failed to load .env.local")
		}
	}

}
