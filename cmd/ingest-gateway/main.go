package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"poltergeist/internal/httpapi"
	"poltergeist/internal/ingest"
	"poltergeist/internal/kafka"
	"poltergeist/internal/mqtt"
	"poltergeist/internal/platform"
	"poltergeist/internal/system"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"
)

func main() {
	if platform.Detect() == platform.Local {
		if err := system.EnsureEnvLoaded(".env.local"); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msg("failed to load .env.local")
		}
	}
	platform.SetupLogging()
	appCfg, err := ingest.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx := context.Background()

	// init kafka, create publisher, ensure topics
	kClient, err := kgo.NewClient(kgo.SeedBrokers(appCfg.Kafka.Brokers...))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka client")
	}
	defer kClient.Close()

	if err := kafka.EnsureTopics(ctx, kClient, kafka.TopicRawDetections, kafka.TopicRawCSI); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure Kafka topics")
	}

	enc, err := kafka.NewEncoder(ctx, appCfg.Kafka.SchemaRegistryURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka encoder")
	}

	publisher := kafka.NewProducer(kClient, enc)
	ingestService := ingest.NewService(publisher, nil)
	mqttConsumer := mqtt.NewConsumer(appCfg.MQTT, ingestService)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", appCfg.HTTPPort),
		Handler:           httpapi.NewRouter(ingestService),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		//WriteTimeout:      10 * time.Second,
		//IdleTimeout:       60 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(sigCtx)
	g.Go(func() error {
		log.Info().Msgf("HTTP server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Warn().Err(err).Msg("failed to start HTTP server")
			return nil // server isnt critical, so we ignore the error and continue
		}
		return nil
	})

	g.Go(func() error {
		if err := mqttConsumer.Start(ctx); err != nil {
			log.Error().Err(err).Msg("failed to start MQTT consumer")
			return err
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()
		log.Info().Msg("shutting down services...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("failed to shutdown HTTP server")
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		log.Error().Err(err).Msg("error occurred during execution")
	}
	log.Info().Msg("application stopped")

}
