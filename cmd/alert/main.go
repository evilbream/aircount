package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"poltergeist/internal/clickhouse"
	"poltergeist/internal/kafka"
	"poltergeist/internal/platform"
	presenceprocessor "poltergeist/internal/presence-processor"
	"poltergeist/internal/system"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"
)

// alert fuses Wi-Fi device presence with RF motion and logs per-device state to ClickHouse.
func main() {
	if platform.Detect() == platform.Local {
		if err := system.LoadLocalEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msg("failed to load local env")
		}
	}
	platform.SetupLogging()
	appCfg, err := presenceprocessor.LoadAlert()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	rfClient, err := kgo.NewClient(
		kgo.SeedBrokers(appCfg.Kafka.Brokers...),
		kgo.ConsumeTopics(string(kafka.TopicPresenceRF)),
		kgo.ConsumerGroup(appCfg.Kafka.GroupID+"-rf"),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create rf Kafka client")
	}
	defer rfClient.Close()

	// presence.device is compacted: no consumer group, the full table is
	// re-read from the start on every boot to rebuild device state.
	deviceClient, err := kgo.NewClient(
		kgo.SeedBrokers(appCfg.Kafka.Brokers...),
		kgo.ConsumeTopics(string(kafka.TopicPresenceDevice)),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create device Kafka client")
	}
	defer deviceClient.Close()

	if err := kafka.EnsureTopics(ctx, rfClient, kafka.TopicPresenceRF, kafka.TopicPresenceDevice); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure Kafka topics")
	}

	enc, err := kafka.NewEncoder(ctx, appCfg.Kafka.SchemaRegistryURL, kafka.TopicPresenceRF, kafka.TopicPresenceDevice)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka encoder")
	}

	conn, err := clickhouse.Open(ctx, appCfg.ClickHouse)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to ClickHouse")
	}
	defer conn.Close()
	log.Info().Msg("connected to ClickHouse")

	writer := clickhouse.NewWriter(conn)
	writer.SwitchDeviceState(true)

	alert := presenceprocessor.NewAlert(
		kafka.NewConsumer(rfClient, enc),
		kafka.NewConsumer(deviceClient, enc),
		writer,
	)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := writer.Run(ctx); err != nil {
			log.Error().Err(err).Msg("ClickHouse writer stopped with error")
			return err
		}
		return nil
	})
	g.Go(func() error {
		if err := alert.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error().Err(err).Msg("alert stopped with error")
			return err
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		log.Fatal().Err(err).Msg("something went wrong")
	}
}
