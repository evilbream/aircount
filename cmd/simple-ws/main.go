package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"poltergeist/internal/httpapi"
	"poltergeist/internal/platform"
	simplews "poltergeist/internal/simple-ws"
	"poltergeist/internal/system"
	"poltergeist/internal/ws"
	"time"

	"github.com/rs/zerolog/log"
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

	go func() {
		for t := range time.Tick(5 * time.Second) {
			msg, _ := json.Marshal(map[string]any{"type": "event", "channel": "channel_name", "ts": t.UnixMilli()})
			hub.Publish(context.Background(), "channel_name", msg)
		}
	}()

	if err := s.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("failed to serve websocket server")
	}

}
