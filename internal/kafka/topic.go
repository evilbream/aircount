package kafka

import (
	"context"
	"errors"
	"fmt"

	_ "embed"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Topic string

const (
	TopicRawDetections Topic = "raw.detections"
	TopicRawCSI        Topic = "raw.csi"
	TopicPresenceRF    Topic = "presence.rf"
)

var (
	ErrUnknownTopic = errors.New("kafka: unknown topic")
)

type TopicSpec struct {
	Name       string
	Partitions int32
	Replicas   int16
	Configs    map[string]*string
}

func makeConfig(retentionMs, cleanupPolicy, segmentMs string) map[string]*string {
	return map[string]*string{
		"retention.ms":   new(retentionMs),
		"cleanup.policy": new(cleanupPolicy),
		"segment.ms":     new(segmentMs),
	}
}

var specs = map[Topic]TopicSpec{
	TopicRawDetections: {Name: string(TopicRawDetections), Partitions: 3, Replicas: 1,
		Configs: makeConfig("3600000", "delete", "1800000")}, // 1 hour retention, 30 minutes segment
	TopicRawCSI: {Name: string(TopicRawCSI), Partitions: 3, Replicas: 1,
		Configs: makeConfig("3600000", "delete", "1800000")}, // 1 hour retention, 30 minutes segment
	TopicPresenceRF: {Name: string(TopicPresenceRF), Partitions: 1, Replicas: 1,
		Configs: makeConfig("86400000", "delete", "21600000")}, // 24 hour retention, 6 hours segment
}

func EnsureTopics(ctx context.Context, cl *kgo.Client, topics ...Topic) error {
	if len(topics) == 0 {
		return nil
	}
	adm := kadm.NewClient(cl)
	for _, name := range topics {
		spec, ok := specs[name]
		if !ok {
			return ErrUnknownTopic
		}
		resps, err := adm.CreateTopics(ctx, spec.Partitions, spec.Replicas, spec.Configs, spec.Name)
		if err != nil {
			return err
		}
		for name, resp := range resps {
			switch {
			case resp.Err == nil:
				log.Info().Str("topic", name).Msg("created topic")
			case errors.Is(resp.Err, kerr.TopicAlreadyExists):
				log.Debug().Str("topic", name).Msg("topic already exists")
			default:
				return fmt.Errorf("failed to create topic %s, cause %w", name, resp.Err)
			}
		}

	}
	return nil
}
