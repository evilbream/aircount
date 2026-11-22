package presenceprocessor

import (
	"poltergeist/internal/config"
	"poltergeist/internal/system"
	"time"
)

type Config struct {
	Kafka        config.Kafka
	BatchSize    int
	OfflineAfter time.Duration
}

func Load() (Config, error) {
	batchSize := system.EnvInt("CSI_PROCESSOR_BATCH_SIZE", 100)
	cfg := Config{
		Kafka:        config.LoadKafka("csi-processor"),
		BatchSize:    batchSize,
		OfflineAfter: system.EnvDefaultDuration("PRESENCE_OFFLINE_AFTER", 3*time.Minute),
	}

	if err := cfg.Kafka.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

type AlertConfig struct {
	Kafka      config.Kafka
	ClickHouse config.ClickHouse
}

func LoadAlert() (AlertConfig, error) {
	cfg := AlertConfig{
		Kafka:      config.LoadKafka("alert"),
		ClickHouse: config.LoadClickHouse(),
	}

	if err := cfg.Kafka.Validate(); err != nil {
		return AlertConfig{}, err
	}
	if err := cfg.ClickHouse.Validate(); err != nil {
		return AlertConfig{}, err
	}

	return cfg, nil
}
