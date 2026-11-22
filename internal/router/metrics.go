package router

import (
	"context"
	"poltergeist/internal/domain"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	clientsOnline prometheus.Gauge
	clientsByBand *prometheus.GaugeVec
	lastSnapshot  prometheus.Gauge
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	return &Metrics{
		clientsOnline: f.NewGauge(prometheus.GaugeOpts{
			Name: "router_clients_online",
			Help: "Clients connected to the router in the last snapshot.",
		}),
		clientsByBand: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "router_clients_online_by_band",
			Help: "Connected clients per band in the last snapshot.",
		}, []string{"band"}),
		lastSnapshot: f.NewGauge(prometheus.GaugeOpts{
			Name: "router_last_snapshot_timestamp_seconds",
			Help: "Unix time of the last successful router snapshot.",
		}),
	}
}

func (m *Metrics) PublishRouterSnapshot(_ context.Context, s domain.RouterSnapshot) error {
	m.clientsOnline.Set(float64(len(s.Clients)))

	byBand := map[domain.Band]int{domain.BandWired: 0, domain.Band24: 0, domain.Band5: 0, domain.Band6: 0, "unknown": 0}
	for _, c := range s.Clients {
		b := c.Band
		if b == "" {
			b = "unknown"
		}
		byBand[b]++
	}

	for band, n := range byBand {
		m.clientsByBand.WithLabelValues(string(band)).Set(float64(n))
	}

	m.lastSnapshot.Set(float64(s.ObservedAt.Unix()))
	return nil
}
