package csiprocessor

import (
	"poltergeist/internal/config"
	"poltergeist/internal/system"
)

type Config struct {
	Kafka     config.Kafka
	BatchSize int
}

func Load() (Config, error) {
	batchSize := system.EnvInt("CSI_PROCESSOR_BATCH_SIZE", 100)
	cfg := Config{
		Kafka:     config.LoadKafka("csi-processor"),
		BatchSize: batchSize,
	}

	if err := cfg.Kafka.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
