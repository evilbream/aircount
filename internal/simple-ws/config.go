package simplews

import (
	"poltergeist/internal/config"
	"poltergeist/internal/platform"
	"poltergeist/internal/system"
)

type Config struct {
	MQTT     config.MQTT
	HTTPPort int
}

func Load() (Config, error) {
	port := system.EnvInt("PORT", 8080)

	if platform.Detect() == platform.Local {
		port = int(system.PortSimpleWS)
	}

	cfg := Config{
		MQTT:     config.LoadMQTT(),
		HTTPPort: port,
	}

	if err := cfg.MQTT.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil

}
