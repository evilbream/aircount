package presenceprocessor

import (
	"context"
	"net"
	"poltergeist/internal/domain"
	"time"

	"github.com/rs/zerolog/log"
)

type DevicePublisher interface {
	PublishDevicePresence(ctx context.Context, devices ...domain.DevicePresence) error
}

type DeviceConsumer interface {
	ConsumeSnapshot(ctx context.Context, handler func(ctx context.Context, presence *domain.RouterSnapshot) error) error
}

type deviceState struct {
	last       domain.DevicePresence
	lastSeenAt time.Time
}

type DeviceService struct {
	publisher    DevicePublisher
	consumer     DeviceConsumer
	offlineAfter time.Duration
	devices      map[string]*deviceState
}

func NewDeviceService(publisher DevicePublisher, consumer DeviceConsumer, offlineAfter time.Duration) *DeviceService {
	return &DeviceService{
		publisher:    publisher,
		consumer:     consumer,
		offlineAfter: offlineAfter,
		devices:      make(map[string]*deviceState),
	}
}

func (s *DeviceService) handleSnapshot(ctx context.Context, snap *domain.RouterSnapshot) error {
	now := snap.ObservedAt
	var changes []domain.DevicePresence
	seen := make(map[string]struct{}, len(snap.Clients))

	for _, c := range snap.Clients {
		seen[c.MAC] = struct{}{}
		st := s.devices[c.MAC]
		if st == nil || st.last.PresenceState != domain.PresenceOnline {
			changes = append(changes, onlineRecord(c, now))
		}
	}
	for mac, st := range s.devices {
		if _, ok := seen[mac]; !ok &&
			st.last.PresenceState == domain.PresenceOnline &&
			now.Sub(st.lastSeenAt) >= s.offlineAfter {
			changes = append(changes, offlineRecord(st, now))
		}
	}

	if err := s.publisher.PublishDevicePresence(ctx, changes...); err != nil {
		return err
	}
	if len(changes) > 0 {
		for _, c := range changes {
			log.Debug().Str("device", c.MAC).Str("online", string(c.PresenceState)).Msg("status changed")
		}
	}
	s.apply(snap, changes)
	return nil
}

func (s *DeviceService) RunDevice(ctx context.Context) error {
	return s.consumer.ConsumeSnapshot(ctx, s.handleSnapshot)
}

func onlineRecord(c domain.RouterClient, now time.Time) domain.DevicePresence {
	return domain.DevicePresence{
		MAC:           c.MAC,
		PresenceState: domain.PresenceOnline,
		ChangedAt:     onlineSince(c, now),
		LastSeenAt:    now,
		ObservedAt:    now,
		Name:          c.Name,
		Kind:          c.Kind,
		IP:            c.IP,
		Guest:         c.Guest,
		RandomMAC:     isRandomMAC(c.MAC),
	}
}

func onlineSince(c domain.RouterClient, now time.Time) time.Time {
	if !c.ConnectedAt.IsZero() && !c.ConnectedAt.After(now) {
		return c.ConnectedAt
	}
	return now
}

func isRandomMAC(mac string) bool {
	hw, err := net.ParseMAC(mac)
	return err == nil && len(hw) > 0 && hw[0]&0x02 != 0
}

func (s *DeviceService) apply(snap *domain.RouterSnapshot, changes []domain.DevicePresence) {
	for _, c := range snap.Clients {
		st := s.devices[c.MAC]
		if st == nil {
			st = &deviceState{}
			s.devices[c.MAC] = st
		}
		st.lastSeenAt = snap.ObservedAt
	}
	for _, r := range changes {
		st := s.devices[r.MAC]
		if st == nil {
			st = &deviceState{lastSeenAt: r.LastSeenAt}
			s.devices[r.MAC] = st
		}
		st.last = r
	}
}

func offlineRecord(st *deviceState, now time.Time) domain.DevicePresence {
	r := st.last
	r.PresenceState = domain.PresenceOffline
	r.ChangedAt = st.lastSeenAt
	r.LastSeenAt = st.lastSeenAt
	r.ObservedAt = now
	return r
}
