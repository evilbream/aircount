package csiprocessor

import (
	"context"
	"poltergeist/internal/domain"
	"time"
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
	publisher  Publisher
	consumer   Consumer
	WindowSpan time.Duration
	MaxSamples int
	clock      Clock
}

// NewService creates a new Service instance with the provided Publisher and Consumer. It initializes the WindowSpan and MaxSamples to default values.
// windowSpan is set to 30 seconds and maxSamples is set to 256. These values can be adjusted as needed for different use cases.
func NewService(publisher Publisher, consumer Consumer) *Service {
	return &Service{
		publisher:  publisher,
		consumer:   consumer,
		WindowSpan: windowSpan,
		MaxSamples: maxSamples,
	}
}

func (s *Service) handleConsumedCSIWindow(ctx context.Context, csi *domain.CSIWindow) error {

	return nil
}

func (s *Service) Run(ctx context.Context) error {
	return s.consumer.ConsumeCSIWindow(ctx, s.handleConsumedCSIWindow)
}
