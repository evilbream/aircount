package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
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

	hub := ws.NewHub()
	service := simplews.NewService(hub)
	wsHandler := httpapi.NewWsServer(service)
	mux := http.NewServeMux()
	mux.Handle("GET /ws", wsHandler)
	mux.HandleFunc("GET /healz", Healz)
	s := &http.Server{
		Handler:      mux,
		ReadTimeout:  50 * time.Second,
		WriteTimeout: 50 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ingestService := ingest.NewService(hub, nil)
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

	if err := g.Wait(); err != nil {
		log.Fatal().Err(err).Msg("application terminated with error")
	}

}
