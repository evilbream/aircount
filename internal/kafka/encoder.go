package kafka

import (
	"context"
	"errors"
	"fmt"
	"poltergeist/internal/domain"
	aircountv1 "poltergeist/internal/pb/aircount/v1"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrMissingObservedAt = errors.New("missing observed_at timestamp")
	ErrMissingWindow     = errors.New("missing window duration")
	ErrWrongMessageType  = errors.New("record carries a different message type")
)

// Positions of the messages inside their .proto files; part of the Confluent
// wire format and the only type marker that survives schema re-registration.
const (
	csiMsgIndex            = 0
	detectionBatchMsgIndex = 1
)

var confluentHeader sr.ConfluentHeader

type Encoder struct {
	serde *sr.Serde
}

// NewEncoder create encoder and registers schemas
func NewEncoder(ctx context.Context, schemaRegistryURL string) (*Encoder, error) {
	rcl, err := sr.NewClient(sr.URLs(schemaRegistryURL))
	if err != nil {
		return nil, fmt.Errorf("failed to create schema registry client: %w", err)
	}
	detectionBatchSchemaID, err := createSchemaForTopic(ctx, rcl, TopicRawDetections, aircountv1.DetectionProto)
	if err != nil {
		return nil, fmt.Errorf("failed to create schema for topic %s: %w", TopicRawDetections, err)
	}
	csiSchemaID, err := createSchemaForTopic(ctx, rcl, TopicRawCSI, aircountv1.CSIProto)
	if err != nil {
		return nil, fmt.Errorf("failed to create schema for topic %s: %w", TopicRawCSI, err)
	}

	return newEncoder(detectionBatchSchemaID, csiSchemaID), nil
}

// newEncoder wires the serde for already-known schema IDs. Split out of
// NewEncoder so tests can build an encoder without a live registry.
func newEncoder(detectionBatchSchemaID, csiSchemaID int) *Encoder {
	var s sr.Serde
	registerSchema(&s, detectionBatchSchemaID, detectionBatchMsgIndex, &aircountv1.DetectionBatch{})
	registerSchema(&s, csiSchemaID, csiMsgIndex, &aircountv1.CSI{})

	return &Encoder{serde: &s}
}

func (e *Encoder) EncodeCSIWindow(csi *domain.CSIWindow) ([]byte, error) {
	pb := &aircountv1.CSI{
		SensorId:    csi.SensorID,
		ObservedAt:  timestamppb.New(csi.ObservedAt),
		Window:      durationpb.New(csi.Window),
		Packets:     csi.Packets,
		MotionScore: csi.MotionScore,
		Subcarriers: csi.Subcarriers,
		UptimeMs:    csi.UptimeMs,
	}
	return e.serde.Encode(pb)
}

func (e *Encoder) DecodeCSIWindow(data []byte) (*domain.CSIWindow, error) {
	var m aircountv1.CSI
	payload, err := stripWireFormat(data, csiMsgIndex)
	if err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	observedAt := m.GetObservedAt()
	if observedAt == nil {
		return nil, ErrMissingObservedAt
	}
	window := m.GetWindow()
	if window == nil {
		return nil, ErrMissingWindow
	}
	return &domain.CSIWindow{
		SensorID:    m.GetSensorId(),
		Packets:     m.GetPackets(),
		MotionScore: m.GetMotionScore(),
		Subcarriers: m.GetSubcarriers(),
		ObservedAt:  observedAt.AsTime(),
		Window:      window.AsDuration(),
		UptimeMs:    m.GetUptimeMs(),
	}, nil
}

func (e *Encoder) EncodeDetectionBatch(b *domain.DetectionBatch) ([]byte, error) {
	devices := make([]*aircountv1.Device, len(b.Devices))
	for i, d := range b.Devices {
		devices[i] = &aircountv1.Device{
			Mac:       d.MAC,
			Rssi:      d.RSSI,
			Channel:   d.Channel,
			Frames:    d.Frames,
			RandomMac: d.RandomMAC,
		}
	}
	pb := &aircountv1.DetectionBatch{
		SensorId:   b.SensorID,
		ObservedAt: timestamppb.New(b.ObservedAt),
		Window:     durationpb.New(b.Window),
		UptimeMs:   b.UptimeMs,
		Devices:    devices,
	}
	return e.serde.Encode(pb)
}

func (e *Encoder) DecodeDetectionBatch(data []byte) (*domain.DetectionBatch, error) {
	payload, err := stripWireFormat(data, detectionBatchMsgIndex)
	if err != nil {
		return nil, err
	}
	var m aircountv1.DetectionBatch
	if err := proto.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	devices := make([]domain.Device, len(m.Devices))
	for i, d := range m.Devices {
		devices[i] = domain.Device{
			MAC:       d.GetMac(),
			RSSI:      d.GetRssi(),
			Channel:   d.GetChannel(),
			Frames:    d.GetFrames(),
			RandomMAC: d.GetRandomMac(),
		}
	}
	observedAt := m.GetObservedAt()
	if observedAt == nil {
		return nil, ErrMissingObservedAt
	}
	window := m.GetWindow()
	if window == nil {
		return nil, ErrMissingWindow
	}
	return &domain.DetectionBatch{
		SensorID:   m.GetSensorId(),
		ObservedAt: observedAt.AsTime(),
		Window:     window.AsDuration(),
		UptimeMs:   m.GetUptimeMs(),
		Devices:    devices,
	}, nil

}

func registerSchema[M proto.Message](s *sr.Serde, id, index int, msg M) {
	s.Register(id, msg,
		sr.EncodeFn(func(a any) ([]byte, error) {
			m, ok := a.(M)
			if !ok {
				return nil, errors.New("invalid type for encoding")
			}
			return proto.Marshal(m)
		}),
		sr.DecodeFn(func(data []byte, a any) error {
			m, ok := a.(M)
			if !ok {
				return errors.New("invalid type for decoding")
			}
			return proto.Unmarshal(data, m)

		}),
		sr.Index(index),
	)
}

func createSchemaForTopic(ctx context.Context, rcl *sr.Client, topicName Topic, schema string) (id int, err error) {
	rawDetectionTopicValue := fmt.Sprintf("%s-value", topicName)
	ss, err := rcl.CreateSchema(ctx, rawDetectionTopicValue, sr.Schema{
		Type:   sr.TypeProtobuf,
		Schema: schema,
	})
	if err != nil {
		return 0, err
	}
	log.Info().Int("id", ss.ID).Str("name", rawDetectionTopicValue).Msg("registered schema")
	return ss.ID, nil

}

// stripWireFormat drops the Confluent header (magic byte, schema ID, message
// index) and returns the bare protobuf payload. The schema ID is deliberately
// ignored — it differs across registries and schema versions — but the message
// index is checked so a record of another type fails loudly instead of
// unmarshaling into garbage.
func stripWireFormat(data []byte, wantIndex int) ([]byte, error) {
	_, payload, err := confluentHeader.DecodeID(data)
	if err != nil {
		return nil, err
	}
	index, payload, err := confluentHeader.DecodeIndex(payload, 1)
	if err != nil {
		return nil, err
	}
	if len(index) != 1 || index[0] != wantIndex {
		return nil, fmt.Errorf("%w: got index %v, want [%d]", ErrWrongMessageType, index, wantIndex)
	}
	return payload, nil
}
