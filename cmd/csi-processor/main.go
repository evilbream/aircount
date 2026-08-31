package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	csiprocessor "poltergeist/internal/csi-processor"
	"poltergeist/internal/kafka"
	"poltergeist/internal/platform"
	"poltergeist/internal/system"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	if platform.Detect() == platform.Local {
		if err := system.EnsureEnvLoaded(".env.local"); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msg("failed to load .env.local")
		}
	}
	platform.SetupLogging()
	appCfg, err := csiprocessor.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx := context.Background()
	kClient, err := kgo.NewClient(kgo.SeedBrokers(appCfg.Kafka.Brokers...),
		kgo.ConsumeTopics(string(kafka.TopicRawCSI)),
		kgo.ConsumerGroup(appCfg.Kafka.GroupID),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka client")
	}
	defer kClient.Close()

	if err := kafka.EnsureTopics(ctx, kClient, kafka.TopicPresenceRF); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure Kafka topics")
	}

	enc, err := kafka.NewEncoder(ctx, appCfg.Kafka.SchemaRegistryURL, kafka.TopicPresenceRF)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka encoder")
	}

	publisher := kafka.NewProducer(kClient, enc)
	consumer := kafka.NewConsumer(kClient, enc)
	presenceService := csiprocessor.NewService(publisher, consumer)

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, os.Kill, syscall.SIGTERM)
	defer stop()

	if err := presenceService.Run(sigCtx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error().Err(err).Msg("csi-processor stopped with error")
	}

}
