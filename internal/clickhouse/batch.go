package clickhouse

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/rs/zerolog/log"
)

type BatchWriter[T any] struct {
	conn           driver.Conn
	table          table[T]
	flushBatchSize int
	interval       time.Duration
	maxBufferSize  int
	mu             sync.Mutex
	buf            []T
	kick           chan struct{}
}

func NewBatchWriter[T any](conn driver.Conn, table table[T]) *BatchWriter[T] {
	return &BatchWriter[T]{
		conn:           conn,
		table:          table,
		flushBatchSize: 50,
		interval:       30 * time.Second,
		maxBufferSize:  10_000,
		kick:           make(chan struct{}, 1),
	}
}

func (b *BatchWriter[T]) Add(v ...T) {
	b.mu.Lock()
	b.buf = append(b.buf, v...)
	b.trimLocked()
	full := len(b.buf) >= b.flushBatchSize
	b.mu.Unlock()

	if full {
		select {
		case b.kick <- struct{}{}:
		default:
		}
	}
}

func (b *BatchWriter[T]) trimLocked() {
	if over := len(b.buf) - b.maxBufferSize; over > 0 {
		log.Warn().Str("query", b.table.query).Int("dropped", over).Msg("clickhouse buffer full, dropping oldest rows")
		b.buf = slices.Clone(b.buf[over:])
	}
}

func (b *BatchWriter[T]) flush(ctx context.Context) error {
	b.mu.Lock()
	rows := b.buf
	b.buf = nil
	b.mu.Unlock()

	if len(rows) == 0 {
		return nil
	}
	if err := b.insert(ctx, rows); err != nil {
		b.mu.Lock()
		b.buf = append(rows, b.buf...)
		b.trimLocked()
		b.mu.Unlock()
		return err
	}
	return nil
}

func (b *BatchWriter[T]) insert(ctx context.Context, rows []T) error {
	batch, err := b.conn.PrepareBatch(ctx, b.table.query)
	if err != nil {
		return err
	}
	defer batch.Close()

	for _, r := range rows {
		if err := b.table.appendRow(batch, r); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (b *BatchWriter[T]) Run(ctx context.Context) error {
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	log.Info().Msg("ClickHouse writer started")
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return b.flush(fctx)
		case <-ticker.C:
		case <-b.kick:
		}

		if err := b.flush(ctx); err != nil {
			log.Error().Err(err).Msg("failed to write data to clickhouse")
		}

	}
}
