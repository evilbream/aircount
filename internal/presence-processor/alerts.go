package presenceprocessor

import (
	"context"
	"poltergeist/internal/domain"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// RFConsumer and DevicePresenceConsumer must be backed by separate Kafka clients:
// a shared client would hand each loop records of the other topic.
type RFConsumer interface {
	ConsumePresenceRF(ctx context.Context, handler func(ctx context.Context, presence *domain.PresenceRF) error) error
}

type DevicePresenceConsumer interface {
	ConsumeDevicePresence(ctx context.Context, handler func(ctx context.Context, presence *domain.DevicePresence) error) error
}

type Residents interface {
	GetResidents(ctx context.Context)
}

type ClickhousePublisher interface {
	PublishDeviceState(ctx context.Context, hs domain.DeviceState) error
}

const (
	roomTTL      = 2 * windowSpan // sensor silent longer than this → its motion no longer counts
	replayCutoff = time.Minute    // older events only rebuild state, no ClickHouse rows
)

type roomState struct {
	motion     bool
	score      float64
	observedAt time.Time
}

type Alert struct {
	rfConsumer     RFConsumer
	deviceConsumer DevicePresenceConsumer
	chPublisher    ClickhousePublisher

	mu      sync.Mutex                    // two consume loops write the same maps
	rooms   map[string]roomState          // sensorID → last RF verdict
	online  map[string]bool               // mac → Wi-Fi online
	written map[string]domain.DeviceState // mac → last row sent to ClickHouse
}

func NewAlert(rf RFConsumer, device DevicePresenceConsumer, ch ClickhousePublisher) *Alert {
	return &Alert{
		rfConsumer:     rf,
		deviceConsumer: device,
		chPublisher:    ch,
		rooms:          make(map[string]roomState),
		online:         make(map[string]bool),
		written:        make(map[string]domain.DeviceState),
	}
}

func (a *Alert) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return a.rfConsumer.ConsumePresenceRF(ctx, a.handlePresenceRF) })
	g.Go(func() error { return a.deviceConsumer.ConsumeDevicePresence(ctx, a.handleDevicePresence) })
	return g.Wait()
}

func (a *Alert) handlePresenceRF(ctx context.Context, p *domain.PresenceRF) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.rooms[p.SensorID] = roomState{motion: p.MotionDetected, score: p.Score, observedAt: p.ObservedAt}
	if time.Since(p.ObservedAt) > replayCutoff {
		return nil
	}

	rooms, score := a.motionLocked(p.ObservedAt)
	var rows []domain.DeviceState
	for mac, online := range a.online {
		if st, changed := a.nextLocked(mac, online, rooms, score, p.ObservedAt); changed {
			rows = append(rows, st)
		}
	}
	return a.publish(ctx, rows...)
}

func (a *Alert) handleDevicePresence(ctx context.Context, d *domain.DevicePresence) error {
	if d.Guest { // TODO: filter by Residents instead
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	online := d.PresenceState == domain.PresenceOnline
	a.online[d.MAC] = online
	if time.Since(d.ObservedAt) > replayCutoff {
		return nil
	}

	rooms, score := a.motionLocked(d.ObservedAt)
	if st, changed := a.nextLocked(d.MAC, online, rooms, score, d.ObservedAt); changed {
		return a.publish(ctx, st)
	}
	return nil
}

func (a *Alert) motionLocked(now time.Time) ([]string, float64) {
	var rooms []string
	var score float64
	for id, r := range a.rooms {
		if !r.motion || now.Sub(r.observedAt) > roomTTL {
			continue
		}
		rooms = append(rooms, id)
		score = max(score, r.score)
	}
	slices.Sort(rooms)
	return rooms, score
}

func (a *Alert) nextLocked(mac string, online bool, rooms []string, score float64, at time.Time) (domain.DeviceState, bool) {
	st := domain.DeviceState{
		MAC:              mac,
		ObservedAt:       at,
		Online:           online,
		PresenceDetected: len(rooms) > 0,
		MotionRooms:      rooms,
		Score:            score,
	}
	if prev, ok := a.written[mac]; ok &&
		prev.Online == st.Online && prev.PresenceDetected == st.PresenceDetected {
		return st, false
	}
	a.written[mac] = st
	return st, true
}

func (a *Alert) publish(ctx context.Context, rows ...domain.DeviceState) error {
	for _, r := range rows {
		if err := a.chPublisher.PublishDeviceState(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
