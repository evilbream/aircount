package platform

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Platform string

const (
	Docker Platform = "docker"
	Local  Platform = "local"
)

func Detect() Platform {
	platform := os.Getenv("PLATFORM")
	if platform == string(Docker) {
		return Docker
	}
	return Local
}

func SetupLogging() {
	if Detect() == Local {
		// Log to console in development
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		return
	}
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}
