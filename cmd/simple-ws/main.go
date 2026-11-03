package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"poltergeist/internal/clickhouse"
	"poltergeist/internal/httpapi"
	"poltergeist/internal/ingest"
	"poltergeist/internal/mqtt"
	"poltergeist/internal/platform"
	simplews "poltergeist/internal/simple-ws"
	"poltergeist/internal/system"
	"poltergeist/internal/ws"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

func Healz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func main() {
	// вебсокет клиент на котрый можно подписаться и получать сообщеняи в реальном времени о подклчюении
	if platform.Detect() == platform.Local {
		if err := system.EnsureEnvLoaded(".env.local"); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msg("failed to load .env.local")
		}
	}
	platform.SetupLogging()

	appCfg, err := simplews.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load app config")
	}

	ctx := context.Background()

	l, err := net.Listen("tcp", ":8088")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to listen on port 8088")
	}
	log.Info().Str("addr", l.Addr().String()).Msg("listening on port 8088")
	defer l.Close()

	conn, err := clickhouse.Open(ctx, appCfg.ClickHouse)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to ClickHouse")
	}
	log.Info().Msg("connected to ClickHouse")

	writer := clickhouse.NewWriter(conn)
	reader := clickhouse.NewReader(conn)

	hub := ws.NewHub()
	service := simplews.NewService(hub)
	handler := httpapi.NewSimpleWSRouter(service, reader)
	s := &http.Server{
		Handler:      handler,
		ReadTimeout:  50 * time.Second,
		WriteTimeout: 50 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ingestService := ingest.NewService(ingest.Fanout(writer, hub), nil)
	mqttConsumer := mqtt.NewConsumer(appCfg.MQTT, ingestService)

	g, ctx := errgroup.WithContext(sigCtx)

	g.Go(func() error {
		if err := s.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("failed to serve websocket server")
			return err
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("failed to shutdown server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		if err := writer.Run(ctx); err != nil {
			log.Error().Err(err).Msg("failed to run ClickHouse writer")
			return err
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		log.Fatal().Err(err).Msg("application terminated with error")
	}

}
