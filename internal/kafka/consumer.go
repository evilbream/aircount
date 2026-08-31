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

func NewConsumer(client *kgo.Client, encoder *Encoder) *Consumer {
	return &Consumer{client: client, enc: encoder}
}

func (c *Consumer) ConsumeCSIWindow(ctx context.Context, handler CSIWindowHandler) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fetches.EachError(func(s string, i int32, err error) {
			log.Error().Err(err).Str("topic", s).Int32("partition", i).Msg("error fetching from topic")
		})

		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()

			w, err := c.enc.DecodeCSIWindow(record.Value)
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

func (c *Consumer) ConsumePresenceRF(ctx context.Context, handler PresenceRFHandler) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fetches.EachError(func(s string, i int32, err error) {
			log.Error().Err(err).Str("topic", s).Int32("partition", i).Msg("error fetching from topic")
		})

		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()

			w, err := c.enc.DecodePresenceRF(record.Value)
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
