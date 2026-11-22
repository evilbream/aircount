package router

import (
	"context"
	"fmt"
	"poltergeist/internal/domain"
	"poltergeist/internal/system"
	"time"

	"github.com/rs/zerolog/log"
)

type Publisher interface {
	PublishRouterSnapshot(ctx context.Context, snapshot domain.RouterSnapshot) error
}

type Router interface {
	GetSnapshot(ctx context.Context) (domain.RouterSnapshot, error)
	Login(ctx context.Context) error
}

type Clock interface {
	Now() time.Time
}

type Service struct {
	router       Router
	poolDuration time.Duration
	publishers   []Publisher
	clock        Clock
}

func New(clock Clock, router Router, publishers ...Publisher) (*Service, error) {
	if len(publishers) == 0 {
		return nil, fmt.Errorf("router service created with no publishers")
	}
	if router == nil {
		return nil, fmt.Errorf("router service created with no router")
	}

	if clock == nil {
		clock = &system.SystemClock{}
	}
	return &Service{
		clock:        clock,
		publishers:   publishers,
		router:       router,
		poolDuration: 20 * time.Second,
	}, nil
}

func (s *Service) publishSnapshot(ctx context.Context, snapshot domain.RouterSnapshot) error {
	at := s.clock.Now()
	snapshot.ObservedAt = at

	for _, p := range s.publishers {
		if err := p.PublishRouterSnapshot(ctx, snapshot); err != nil {
			log.Error().Err(err).Msg("failed to publish router snapshot")
		}
	}
	return nil
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(s.poolDuration)
	defer ticker.Stop()
	for {
		snapshot, err := s.router.GetSnapshot(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to get router snapshot")
		} else {
			log.Debug().Int("Clients", len(snapshot.Clients)).Msg("got router snapshot")

			if err := s.publishSnapshot(ctx, snapshot); err != nil {
				log.Error().Err(err).Msg("failed to publish router snapshot")
			}
		}
		select {
		case <-ctx.Done():
			log.Info().Msg("router service shutting down")
			return
		case <-ticker.C:
		}
	}

}
