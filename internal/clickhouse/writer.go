package clickhouse

import (
	"context"
	"poltergeist/internal/domain"
	"slices"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/rs/zerolog/log"
)

const (
	insertCSIQuery = "INSERT INTO csi_windows (observed_at, sensor_id, motion_score, packets, subcarriers, window_ms, uptime_ms)"
)

const (
	flushInterval  = 30 * time.Second
	flushBatchSize = 50
)

type Writer struct {
	conn     driver.Conn
	csiBatch []domain.CSIWindow
	mu       sync.Mutex
}

func NewWriter(conn driver.Conn) *Writer {
	return &Writer{
		conn:     conn,
		csiBatch: make([]domain.CSIWindow, 0),
	}
}
func (w *Writer) PublishCSI(ctx context.Context, csi domain.CSIWindow) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.csiBatch = append(w.csiBatch, csi)
	return nil
}
func (w *Writer) PublishDevices(ctx context.Context, d domain.DetectionBatch) error {
	return nil
}

func (w *Writer) flushCSI(ctx context.Context, force bool) error {
	w.mu.Lock()
	rows := w.csiBatch
	w.mu.Unlock()

	if force || len(rows) < flushBatchSize {
		return nil
	}
	if err := w.insertCSI(ctx, rows); err != nil {
		return err
	}
	w.mu.Lock()
	w.csiBatch = slices.Clone(w.csiBatch[len(rows):])
	w.mu.Unlock()
	return nil
}

func (w *Writer) insertCSI(ctx context.Context, rows []domain.CSIWindow) error {
	// Implement the actual insertion logic here
	batch, err := w.conn.PrepareBatch(ctx, insertCSIQuery)
	if err != nil {
		return err
	}
	defer batch.Close()

	for _, r := range rows {
		err := batch.Append(
			r.ObservedAt,
			r.SensorID,
			r.MotionScore,
			r.Packets,
			uint16(r.Subcarriers),
			uint32(r.Window.Milliseconds()),
			r.UptimeMs,
		)
		if err != nil {
			return err
		}
	}
	return batch.Send()
}

func (w *Writer) Run(ctx context.Context) error {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	log.Info().Msg("ClickHouse writer started")
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			w.flushCSI(fctx, true)
			return ctx.Err()
		case <-ticker.C:

			if err := w.flushCSI(ctx, false); err != nil {
				log.Error().Err(err).Msg("failed to write data to clickhouse")
			}

		}
	}
}
