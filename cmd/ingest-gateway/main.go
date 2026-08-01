package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"poltergeist/internal/domain"
	"poltergeist/internal/httpapi"
	"poltergeist/internal/ingest"
	"poltergeist/internal/mqtt"
	"poltergeist/internal/platform"
	"poltergeist/internal/system"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

type NaivePublisher struct{}

func (p *NaivePublisher) PublishDevices(ctx context.Context, devices []domain.Device) error {
	zerolog.Ctx(ctx).Info().Msgf("Publishing %d devices", len(devices))
	return nil
}

func (p *NaivePublisher) PublishCSI(ctx context.Context, csi domain.CSIWindow) error {
	zerolog.Ctx(ctx).Info().Msgf("Publishing CSI data for sensor %s", csi.SensorID)
	return nil
}

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

	ingestService := ingest.NewService(&NaivePublisher{}, nil)
	consumer := mqtt.NewConsumer(appCfg.MQTT, ingestService)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", appCfg.HTTPPort),
		Handler:           httpapi.NewRouter(ingestService),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		//WriteTimeout:      10 * time.Second,
		//IdleTimeout:       60 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(sigCtx)
	g.Go(func() error {
		log.Info().Msgf("HTTP server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("failed to start HTTP server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		return consumer.Start(ctx)
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
