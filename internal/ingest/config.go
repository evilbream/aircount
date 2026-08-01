package ingest

import (
	"poltergeist/internal/config"
	"poltergeist/internal/platform"
	"poltergeist/internal/system"
)

type Config struct {
	MQTT     config.MQTT
	Kafka    config.Kafka
	HTTPPort int
}

func Load() (Config, error) {
	port := system.EnvInt("PORT", 8080)

	if platform.Detect() == platform.Local {
		port = int(system.PortIngestGateway)
	}

	cfg := Config{
		MQTT:     config.LoadMQTT(),
		Kafka:    config.LoadKafka(""),
		HTTPPort: port,
	}

	if err := cfg.MQTT.Validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.Kafka.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil

}
