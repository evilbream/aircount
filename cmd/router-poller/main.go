package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"poltergeist/internal/clickhouse"
	"poltergeist/internal/httpapi"
	"poltergeist/internal/kafka"
	"poltergeist/internal/metrics"
	"poltergeist/internal/platform"
	"poltergeist/internal/router"
	"poltergeist/internal/system"
	"poltergeist/internal/tplink"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"
)

func main() {
	if platform.Detect() == platform.Local {
		if err := system.LoadLocalEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msg("failed to load .env.local")
		}
	}
	platform.SetupLogging()
	appCfg, err := router.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	kClient, err := kgo.NewClient(kgo.SeedBrokers(appCfg.Kafka.Brokers...))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka client")
	}
	defer kClient.Close()

	if err := kafka.EnsureTopics(ctx, kClient, kafka.TopicRouterSnapshot); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure Kafka topics")
	}

	enc, err := kafka.NewEncoder(ctx, appCfg.Kafka.SchemaRegistryURL, kafka.TopicRouterSnapshot)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka encoder")
	}

	conn, err := clickhouse.Open(ctx, appCfg.ClickHouse)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to ClickHouse")
	}
	log.Info().Msg("connected to ClickHouse")

	writer := clickhouse.NewWriter(conn)
	writer.SwitchRouterSnapshot(true)
	publisher := kafka.NewProducer(kClient, enc)

	routerClient := tplink.New(appCfg.Router.Host, appCfg.Router.Username, appCfg.Router.Password)
	if err := routerClient.Login(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to login to router")
	}
	log.Info().Str("Host", appCfg.Router.Host).Msg("logged in to router")

	reg := metrics.NewRegistry()
	httpMetrics := metrics.NewHTTP(reg)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", appCfg.HttpPort),
		Handler:           httpapi.NewWifiRouter(httpMetrics, metrics.Handler(reg)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		//WriteTimeout:      10 * time.Second,
		//IdleTimeout:       60 * time.Second,
	}

	routerPoller, err := router.New(nil, tplink.NewAdapter(routerClient), router.NewMetrics(reg), publisher, writer)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create router poller")
	}

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		routerPoller.Run(ctx)
		return nil
	})

	g.Go(func() error {
		if err := writer.Run(ctx); err != nil {
			log.Error().Err(err).Msg("failed to run ClickHouse writer")
			return err
		}
		return nil
	})

	g.Go(func() error {
		log.Info().Msgf("HTTP server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Warn().Err(err).Msg("failed to start HTTP server")
			return nil // server isnt critical, so we ignore the error and continue
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()
		log.Info().Msg("shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("failed to shutdown HTTP server")
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		log.Fatal().Err(err).Msg("router poller exited with error")
	}
	log.Info().Msg("router poller stopped")
}
