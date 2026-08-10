package ingest

import (
	"context"
	"errors"
	"poltergeist/internal/domain"
	"poltergeist/internal/sensorwire"
	"time"
)

type Publisher interface {
	PublishDevices(ctx context.Context, devices domain.DetectionBatch) error
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
	var errDevices error
	var errCSI error

	at := s.clock.Now()
	window := time.Duration(b.WindowMS) * time.Millisecond
	if len(b.Devices) > 0 {
		devices := make([]domain.Device, len(b.Devices))
		for i, d := range b.Devices {
			devices[i] = domain.Device{
				MAC:       d.MAC,
				RSSI:      d.RSSI,
				Channel:   d.Channel,
				Frames:    d.Frames,
				RandomMAC: d.Random,
			}
		}
		deviceBatch := domain.DetectionBatch{
			SensorID:   b.SensorID,
			ObservedAt: at,
			Window:     window,
			UptimeMs:   b.UptimeMS,
			Devices:    devices,
		}
		errDevices = s.publisher.PublishDevices(ctx, deviceBatch)
	}

	if b.CSI.Packets == 0 {
		return errDevices
	}

	csi := domain.CSIWindow{
		SensorID:    b.SensorID,
		Packets:     b.CSI.Packets,
		MotionScore: b.CSI.MotionScore,
		Subcarriers: b.CSI.Subcarriers,
		ObservedAt:  at,
		Window:      window,
		UptimeMs:    b.UptimeMS,
	}

	errCSI = s.publisher.PublishCSI(ctx, csi)
	return errors.Join(errCSI, errDevices)
}
