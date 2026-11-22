package tplink

import (
	"context"
	"fmt"
	"time"

	"poltergeist/internal/domain"
)

type Adapter struct {
	client *Client
	now    func() time.Time
}

func NewAdapter(c *Client) *Adapter {
	return &Adapter{client: c, now: time.Now}
}

func (a *Adapter) Login(ctx context.Context) error {
	return a.client.Login(ctx)
}

// GetSnapshot merges admin/status (CPU, memory, WAN IP, guest flag) with the
// game accelerator device list (name, kind, signal, traffic, link rates).
func (a *Adapter) GetSnapshot(ctx context.Context) (domain.RouterSnapshot, error) {
	status, err := a.client.GetSnapshot(ctx)
	if err != nil {
		return domain.RouterSnapshot{}, fmt.Errorf("get status: %w", err)
	}
	infos, err := a.client.GetClientsInfo(ctx)
	if err != nil {
		return domain.RouterSnapshot{}, fmt.Errorf("get clients info: %w", err)
	}
	// Taken after the calls: onlineTime is as of the clients info response.
	return toDomain(a.now(), status, infos), nil
}

func toDomain(now time.Time, status Snapshot, infos []ClientInfo) domain.RouterSnapshot {
	devices := make(map[string]Device, len(status.Devices))
	for _, d := range status.Devices {
		devices[d.MAC] = d
	}

	clients := make([]domain.RouterClient, 0, max(len(infos), len(status.Devices)))
	seen := make(map[string]bool, len(infos))
	for _, ci := range infos {
		seen[ci.MAC] = true
		d := devices[ci.MAC] // zero Device if status doesn't list it
		c := domain.RouterClient{
			MAC:          ci.MAC,
			IP:           firstNonEmpty(ci.IP, d.IP),
			Name:         firstNonEmpty(ci.Name, d.Hostname),
			Kind:         ci.Type,
			Band:         toDomainBand(firstNonEmpty(ci.Band, d.Band)),
			Guest:        d.Guest, // only admin/status knows the guest network
			SignalDBm:    ci.Signal,
			TrafficBytes: ci.Traffic,
			UpBps:        ci.UpBytesPerSec,
			DownBps:      ci.DownBytesPerSec,
			TxMbps:       ci.TxMbps,
			RxMbps:       ci.RxMbps,
		}
		if ci.Online > 0 {
			c.ConnectedAt = now.Add(-ci.Online)
		}
		clients = append(clients, c)
	}

	// Connected devices the accelerator list left out: keep what status knows.
	for _, d := range status.Devices {
		if seen[d.MAC] {
			continue
		}
		seen[d.MAC] = true
		clients = append(clients, domain.RouterClient{
			MAC:   d.MAC,
			IP:    d.IP,
			Name:  d.Hostname,
			Band:  toDomainBand(d.Band),
			Guest: d.Guest,
		})
	}

	return domain.RouterSnapshot{
		ObservedAt: now,
		CPU:        status.CPU,
		Mem:        status.Mem,
		WANIP:      status.WANIP,
		Clients:    clients,
	}
}

func toDomainBand(b Band) domain.Band {
	switch b {
	case BandWired:
		return domain.BandWired
	case Band24:
		return domain.Band24
	case Band5:
		return domain.Band5
	case Band6:
		return domain.Band6
	default:
		return ""
	}
}

func firstNonEmpty[T ~string](a, b T) T {
	if a != "" {
		return a
	}
	return b
}
