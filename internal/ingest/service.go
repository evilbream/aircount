package ingest

import (
	"context"
	"errors"
	"poltergeist/internal/domain"
	"poltergeist/internal/sensorwire"
	"time"
)

type Publisher interface {
	PublishDevices(ctx context.Context, devices []domain.Device) error
	PublishCSI(ctx context.Context, csi domain.CSIWindow) error
}

type Clock interface {
	Now() time.Time
}

type Service struct {
	publisher Publisher
	clock     Clock
}

type systemClock struct{}

func (c *systemClock) Now() time.Time {
	return time.Now()
}

func NewService(publisher Publisher, clock Clock) *Service {
	if clock == nil {
		clock = &systemClock{}
	}
	return &Service{
		publisher: publisher,
		clock:     clock,
	}
}

func (s *Service) Ingest(ctx context.Context, b sensorwire.Batch) error {
	at := s.clock.Now()
	devices := make([]domain.Device, len(b.Devices))
	window := time.Duration(b.WindowMS) * time.Millisecond
	for i, d := range b.Devices {
		devices[i] = domain.Device{
			SensorID:   b.SensorID,
			MAC:        d.MAC,
			RSSI:       d.RSSI,
			Channel:    d.Channel,
			Frames:     d.Frames,
			RandomMAC:  d.Random,
			ObservedAt: at,
			Window:     window,
		}
	}

	csi := domain.CSIWindow{
		SensorID:    b.SensorID,
		Packets:     b.CSI.Packets,
		MotionScore: b.CSI.MotionScore,
		Subcarriers: b.CSI.Subcarriers,
		ObservedAt:  at,
		Window:      window,
	}

	errCSI := s.publisher.PublishCSI(ctx, csi)
	errDevices := s.publisher.PublishDevices(ctx, devices)
	return errors.Join(errCSI, errDevices)
}
