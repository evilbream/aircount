package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"poltergeist/internal/kafka"
	"poltergeist/internal/platform"
	presenceprocessor "poltergeist/internal/presence-processor"
	"poltergeist/internal/system"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"
)

func main() {
	if platform.Detect() == platform.Local {
		if err := system.LoadLocalEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msg("failed to load local env")
		}
	}
	platform.SetupLogging()
	appCfg, err := presenceprocessor.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx := context.Background()
	csiClient, err := kgo.NewClient(
		kgo.SeedBrokers(appCfg.Kafka.Brokers...),
		kgo.ConsumeTopics(string(kafka.TopicRawCSI)),
		kgo.ConsumerGroup(appCfg.Kafka.GroupID+"-csi"),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create csi Kafka client")
	}
	defer csiClient.Close()

	routerClient, err := kgo.NewClient(
		kgo.SeedBrokers(appCfg.Kafka.Brokers...),
		kgo.ConsumeTopics(string(kafka.TopicRouterSnapshot)),
		kgo.ConsumerGroup(appCfg.Kafka.GroupID+"-device"),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create router Kafka client")
	}
	defer routerClient.Close()

	if err := kafka.EnsureTopics(ctx, csiClient, kafka.TopicPresenceRF, kafka.TopicPresenceDevice); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure Kafka topics")
	}

	encoder, err := kafka.NewEncoder(ctx, appCfg.Kafka.SchemaRegistryURL, kafka.TopicPresenceRF, kafka.TopicPresenceDevice)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka encoder")
	}

	producer := kafka.NewProducer(csiClient, encoder)

	presenceService := presenceprocessor.NewService(
		producer,
		kafka.NewConsumer(csiClient, encoder),
	)

	deviceService := presenceprocessor.NewDeviceService(
		producer,
		kafka.NewConsumer(routerClient, encoder),
		appCfg.OfflineAfter)

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, os.Kill, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(sigCtx)
	g.Go(func() error {
		if err := presenceService.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error().Err(err).Msg("csi-processor stopped with error")
			return err
		}
		return nil
	})

	g.Go(func() error {
		if err := deviceService.RunDevice(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error().Err(err).Msg("run device consume error")
			return err
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		log.Fatal().Err(err).Msg("something went wrong")
	}

}
