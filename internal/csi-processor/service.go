package csiprocessor

import (
	"context"
	"poltergeist/internal/domain"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	windowSpan = 30 * time.Second
	maxSamples = 256
)

type Clock interface {
	Now() time.Time
}

type Publisher interface {
	PublishPresence(ctx context.Context, devices domain.PresenceRF) error
}

type Consumer interface {
	ConsumeCSIWindow(ctx context.Context, handler func(ctx context.Context, presence *domain.CSIWindow) error) error
}

type Service struct {
	EnableHTTP bool // enables the HTTP server for the CSI processor service
	publisher  Publisher
	consumer   Consumer
	WindowSpan time.Duration
	MaxSamples int
	clock      Clock
	csiQueue   map[string][]*domain.CSIWindow
}

// NewService creates a new Service instance with the provided Publisher and Consumer. It initializes the WindowSpan and MaxSamples to default values.
// windowSpan is set to 30 seconds and maxSamples is set to 256. These values can be adjusted as needed for different use cases.
func NewService(publisher Publisher, consumer Consumer) *Service {
	return &Service{
		publisher:  publisher,
		consumer:   consumer,
		WindowSpan: windowSpan,
		MaxSamples: maxSamples,
		csiQueue:   make(map[string][]*domain.CSIWindow),
	}
}

func (s *Service) handleConsumedCSIWindow(ctx context.Context, csi *domain.CSIWindow) error {
	if csi == nil {
		return nil
	}

	csiSensorID := csi.SensorID
	queue := append(s.csiQueue[csiSensorID], csi)
	s.csiQueue[csiSensorID] = queue

	if len(queue) <= 1 {
		return nil
	}

	if csi.ObservedAt.Sub(queue[0].ObservedAt) < s.WindowSpan && len(queue) < s.MaxSamples {
		s.csiQueue[csiSensorID] = queue
		return nil
	}

	s.csiQueue[csiSensorID] = nil

	presence := domain.CalcualtePresenceRF(queue)
	log.Debug().Str("sensor_id", presence.SensorID).Bool("motion_detected", presence.MotionDetected).Msg("presence")

	return s.publisher.PublishPresence(ctx, presence)
}

func (s *Service) Run(ctx context.Context) error {
	return s.consumer.ConsumeCSIWindow(ctx, s.handleConsumedCSIWindow)
}

func (svc *Service) ListLastPresence(ctx context.Context, sensorID string, limit int) ([]domain.PresenceRF, error) {
	return nil, nil
}
