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
	presenceRFMsgIndex     = 0
)

var confluentHeader sr.ConfluentHeader

type schemaSpec struct {
	proto   string
	index   int
	message proto.Message
}

var topicSchemas = map[Topic]schemaSpec{
	TopicRawDetections: {proto: aircountv1.DetectionProto, index: detectionBatchMsgIndex, message: &aircountv1.DetectionBatch{}},
	TopicRawCSI:        {proto: aircountv1.CSIProto, index: csiMsgIndex, message: &aircountv1.CSI{}},
	TopicPresenceRF:    {proto: aircountv1.PresenceRFProto, index: presenceRFMsgIndex, message: &aircountv1.PresenceRF{}},
}

type Encoder struct {
	serde *sr.Serde
}

// NewEncoder create encoder and registers schemas
func NewEncoder(ctx context.Context, schemaRegistryURL string, topic ...Topic) (*Encoder, error) {
	if len(topic) == 0 {
		return nil, fmt.Errorf("no topics provided for schema registration")
	}
	rcl, err := sr.NewClient(sr.URLs(schemaRegistryURL))
	if err != nil {
		return nil, fmt.Errorf("failed to create schema registry client: %w", err)
	}

	topicIds := make(map[Topic]int, len(topic))

	for _, t := range topic {
		spec, ok := topicSchemas[t]
		if !ok {
			return nil, fmt.Errorf("no schema defined for topic %s", t)
		}
		schemaID, err := createSchemaForTopic(ctx, rcl, t, spec.proto)
		if err != nil {
			return nil, fmt.Errorf("failed to create schema for topic %s: %w", t, err)
		}
		topicIds[t] = schemaID
	}

	return newEncoder(topicIds)
}

func newEncoder(topicIds map[Topic]int) (*Encoder, error) {
	// Register the schemas for the provided topics
	var s sr.Serde

	for t, id := range topicIds {
		spec, ok := topicSchemas[t]
		if !ok {
			return nil, fmt.Errorf("no schema defined for topic %s", t)
		}
		registerSchema(&s, id, spec.index, spec.message)
	}

	return &Encoder{serde: &s}, nil
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

func (e *Encoder) EncodePresenceRF(p *domain.PresenceRF) ([]byte, error) {
	pb := &aircountv1.PresenceRF{
		SensorId:       p.SensorID,
		MotionDetected: p.MotionDetected,
		Score:          p.Score,
		ObservedAt:     timestamppb.New(p.ObservedAt),
		Window:         durationpb.New(p.Window),
		Packets:        p.Packets,
	}
	return e.serde.Encode(pb)
}

func (e *Encoder) DecodePresenceRF(data []byte) (*domain.PresenceRF, error) {
	payload, err := stripWireFormat(data, presenceRFMsgIndex)
	if err != nil {
		return nil, err
	}
	var m aircountv1.PresenceRF
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
	return &domain.PresenceRF{
		SensorID:       m.GetSensorId(),
		MotionDetected: m.GetMotionDetected(),
		Score:          m.GetScore(),
		ObservedAt:     observedAt.AsTime(),
		Window:         window.AsDuration(),
		Packets:        m.GetPackets(),
	}, nil
}

func registerSchema(s *sr.Serde, id, index int, msg proto.Message) {
	s.Register(id, msg,
		sr.EncodeFn(func(a any) ([]byte, error) {
			m, ok := a.(proto.Message)
			if !ok {
				return nil, errors.New("invalid type for encoding")
			}
			return proto.Marshal(m)
		}),
		sr.DecodeFn(func(data []byte, a any) error {
			m, ok := a.(proto.Message)
			if !ok {
				return errors.New("invalid type for decoding")
			}
			return proto.Unmarshal(data, m)

		}),
		sr.Index(index),
	)
}

func createSchemaForTopic(ctx context.Context, rcl *sr.Client, topicName Topic, schema string) (id int, err error) {
	topicValue := fmt.Sprintf("%s-value", topicName)
	ss, err := rcl.CreateSchema(ctx, topicValue, sr.Schema{
		Type:   sr.TypeProtobuf,
		Schema: schema,
	})
	if err != nil {
		return 0, err
	}
	log.Info().Int("id", ss.ID).Str("name", topicValue).Msg("registered schema")
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
