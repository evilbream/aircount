package router

import (
	"poltergeist/internal/config"
	"poltergeist/internal/system"
)

type Config struct {
	Router     config.Router
	Kafka      config.Kafka
	ClickHouse config.ClickHouse
	HttpPort   int
}

func Load() (Config, error) {
	port := system.EnvInt("PORT-POLLER", int(system.PortRouterPoller))

	cfg := Config{
		Router:     config.LoadRouter(),
		Kafka:      config.LoadKafka("router-poller"),
		ClickHouse: config.LoadClickHouse(),
		HttpPort:   port,
	}

	if err := cfg.Router.Validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.Kafka.Validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.ClickHouse.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil

}
