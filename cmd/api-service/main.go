package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	csiprocessor "poltergeist/internal/csi-processor"
	"poltergeist/internal/domain"
	"poltergeist/internal/httpapi"
	"poltergeist/internal/kafka"
	"poltergeist/internal/platform"
	"poltergeist/internal/system"
	"poltergeist/internal/uiwire"
	"poltergeist/internal/ws"
	"time"

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

	enc, err := kafka.NewEncoder(ctx, appCfg.Kafka.SchemaRegistryURL, kafka.TopicRawCSI)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create Kafka encoder")
	}
	consumer := kafka.NewConsumer(kClient, enc)
	l, err := net.Listen("tcp", ":8088")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to listen on port 8088")
	}
	defer l.Close()

	hub := ws.NewHub()
	wsHandler := httpapi.NewWSHandler(hub)
	mux := http.NewServeMux()
	mux.Handle("GET /ws", wsHandler)
	s := &http.Server{
		Handler:      mux,
		ReadTimeout:  50 * time.Second,
		WriteTimeout: 50 * time.Second,
	}
	go consumer.ConsumeCSIWindow(ctx, func(ctx context.Context, csi *domain.CSIWindow) error {

		uicsi := uiwire.FromCsiWindow(*csi)
		data, err := json.Marshal(uicsi)
		if err != nil {
			log.Error().Err(err).Msg("failed to marshal CSI window")
			return err
		}
		hub.Broadcast(ctx, data)
		return nil
	})

	if err := s.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("failed to serve websocket server")
	}

}
