package kafka

import (
	"context"
	"poltergeist/internal/domain"

	"github.com/twmb/franz-go/pkg/kgo"
)

type Producer struct {
	client *kgo.Client
	enc    *Encoder
}

func NewProducer(client *kgo.Client, encoder *Encoder) *Producer {
	return &Producer{client: client, enc: encoder}
}

func (p *Producer) PublishDevices(ctx context.Context, devices domain.DetectionBatch) error {
	if len(devices.Devices) == 0 {
		return nil
	}
	value, err := p.enc.EncodeDetectionBatch(&devices)
	if err != nil {
		return err
	}
	record := &kgo.Record{
		Topic: string(TopicRawDetections),
		Key:   []byte(devices.SensorID),
		Value: value,
	}
	return p.client.ProduceSync(ctx, record).FirstErr()
}
func (p *Producer) PublishCSI(ctx context.Context, csi domain.CSIWindow) error {
	value, err := p.enc.EncodeCSIWindow(&csi)
	if err != nil {
		return err
	}
	record := &kgo.Record{
		Topic: string(TopicRawCSI),
		Key:   []byte(csi.SensorID),
		Value: value,
	}
	return p.client.ProduceSync(ctx, record).FirstErr()
}

func (p *Producer) PublishPresence(ctx context.Context, presence domain.PresenceRF) error {
	value, err := p.enc.EncodePresenceRF(&presence)
	if err != nil {
		return err
	}
	record := &kgo.Record{
		Topic: string(TopicPresenceRF),
		Key:   []byte(presence.SensorID),
		Value: value,
	}
	return p.client.ProduceSync(ctx, record).FirstErr()
}
