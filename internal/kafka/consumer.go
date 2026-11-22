package kafka

import (
	"context"
	"poltergeist/internal/domain"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Consumer struct {
	client *kgo.Client
	enc    *Encoder
}
type CSIWindowHandler = func(ctx context.Context, presence *domain.CSIWindow) error

type PresenceRFHandler = func(ctx context.Context, presence *domain.PresenceRF) error
type RouterSnapshotHandler = func(ctx context.Context, s *domain.RouterSnapshot) error

func NewConsumer(client *kgo.Client, encoder *Encoder) *Consumer {
	return &Consumer{client: client, enc: encoder}
}

func consume[T any](ctx context.Context, c *Consumer, decode func([]byte) (*T, error),
	handler func(context.Context, *T) error) error {

	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fetches.EachError(func(t string, p int32, err error) {
			log.Error().Err(err).Str("topic", t).Int32("partition", p).Msg("error fetching from topic")
		})

		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()

			w, err := decode(record.Value)
			if err != nil {
				log.Warn().Err(err).Str("topic", record.Topic).Int32("partition", record.Partition).Int64("offset", record.Offset).Msg("failed to decode CSI window")
			} else if err := handler(ctx, w); err != nil {
				return err // no commit, retry
			}
			if err := c.client.CommitRecords(ctx, record); err != nil {
				return err

			}
		}

	}
}

func (c *Consumer) ConsumeCSIWindow(ctx context.Context, handler CSIWindowHandler) error {
	return consume(ctx, c, c.enc.DecodeCSIWindow, handler)
}

func (c *Consumer) ConsumePresenceRF(ctx context.Context, handler PresenceRFHandler) error {
	return consume(ctx, c, c.enc.DecodePresenceRF, handler)
}

func (c *Consumer) ConsumeSnapshot(ctx context.Context, handler RouterSnapshotHandler) error {
	return consume(ctx, c, c.enc.DecodeRouterSnapshot, handler)
}
