package ingest

import (
	"context"
	"poltergeist/internal/sensorwire"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	batches        *prometheus.CounterVec
	ingestDuration *prometheus.HistogramVec
	lastBatch      *prometheus.GaugeVec
	uptime         *prometheus.GaugeVec
	devices        *prometheus.GaugeVec
	csiPackets     *prometheus.CounterVec
	decodeErrors   *prometheus.CounterVec
	motionScore    *prometheus.GaugeVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	return &Metrics{
		motionScore: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ingest_csi_motion_score",
			Help: "CSI motion score reported by the sensor in its last batch.",
		}, []string{"sensor_id"}),
		batches: f.NewCounterVec(prometheus.CounterOpts{
			Name: "ingest_batches_total",
			Help: "Batches processed, by sensor and result.",
		}, []string{"sensor_id", "result"}),

		ingestDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ingest_duration_seconds",
			Help:    "Full Ingest() time: mapping + all publishes.",
			Buckets: prometheus.DefBuckets,
		}, []string{"sensor_id"}),

		lastBatch: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ingest_last_batch_timestamp_seconds",
			Help: "Unix time of the last batch per sensor.",
		}, []string{"sensor_id"}),

		uptime: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ingest_sensor_uptime_seconds",
			Help: "Uptime reported by the sensor in its last batch.",
		}, []string{"sensor_id"}),

		devices: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ingest_batch_devices",
			Help: "Number of devices in the last batch per sensor.",
		}, []string{"sensor_id"}),

		csiPackets: f.NewCounterVec(prometheus.CounterOpts{
			Name: "ingest_csi_packets_total",
			Help: "CSI packets reported by sensors.",
		}, []string{"sensor_id"}),

		decodeErrors: f.NewCounterVec(prometheus.CounterOpts{
			Name: "ingest_decode_errors_total",
			Help: "Payloads that failed to decode or validate.",
		}, []string{"source"}), // mqtt | http

	}
}

func (m *Metrics) DecodeError(source string) {
	if m == nil {
		return
	}
	m.decodeErrors.WithLabelValues(source).Inc()
}

func result(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

// Decorator for publish calls to record metrics.

type Ingester interface {
	Ingest(ctx context.Context, b sensorwire.Batch) error
}

type instrumentedService struct {
	next Ingester
	m    *Metrics
}

func NewInstrumentedService(next Ingester, m *Metrics) Ingester {
	if m == nil {
		return next
	}
	return &instrumentedService{
		next: next,
		m:    m,
	}
}

func (s *instrumentedService) Ingest(ctx context.Context, b sensorwire.Batch) error {
	start := time.Now()
	err := s.next.Ingest(ctx, b)

	s.m.ingestDuration.WithLabelValues(b.SensorID).Observe(time.Since(start).Seconds())
	s.m.batches.WithLabelValues(b.SensorID, result(err)).Inc()
	s.m.lastBatch.WithLabelValues(b.SensorID).SetToCurrentTime()
	s.m.uptime.WithLabelValues(b.SensorID).Set(float64(b.UptimeMS) / 1000)
	s.m.devices.WithLabelValues(b.SensorID).Set(float64(len(b.Devices)))
	s.m.csiPackets.WithLabelValues(b.SensorID).Add(float64(b.CSI.Packets))
	s.m.motionScore.WithLabelValues(b.SensorID).Set(float64(b.CSI.MotionScore))

	return err

}
