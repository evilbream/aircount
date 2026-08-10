package kafka

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"poltergeist/internal/domain"
	aircountv1 "poltergeist/internal/pb/aircount/v1"
)

// Schema IDs are assigned by the registry at runtime; any two distinct values
// work here as long as encode and decode agree on them.
const (
	testDetectionSchemaID = 101
	testCSISchemaID       = 102
)

func testEncoder(t *testing.T) *Encoder {
	t.Helper()
	return newEncoder(testDetectionSchemaID, testCSISchemaID)
}

// Decoded timestamps come back as UTC without a monotonic reading, so the
// expected value needs the same treatment before a struct-wide comparison.
func normalizedTime(t time.Time) time.Time {
	return t.UTC()
}

func TestEncodeDecodeDetectionBatch(t *testing.T) {
	enc := testEncoder(t)
	observedAt := time.Now()

	tests := []struct {
		name  string
		batch domain.DetectionBatch
	}{
		{
			name: "empty detection batch",
			batch: domain.DetectionBatch{
				SensorID:   "sensor-1",
				ObservedAt: observedAt,
				Window:     5 * time.Second,
				UptimeMs:   1000,
				Devices:    []domain.Device{},
			},
		},
		{
			name: "detection batch with devices",
			batch: domain.DetectionBatch{
				SensorID:   "sensor-2",
				ObservedAt: observedAt,
				Window:     10 * time.Second,
				UptimeMs:   2000,
				Devices: []domain.Device{
					{MAC: "00:11:22:33:44:55", RSSI: -50, Channel: 6, Frames: 10, RandomMAC: false},
					{MAC: "66:77:88:99:AA:BB", RSSI: -60, Channel: 11, Frames: 5, RandomMAC: true},
				},
			},
		},
		{
			name: "empty sensor id round-trips",
			batch: domain.DetectionBatch{
				SensorID:   "",
				ObservedAt: observedAt,
				Window:     5 * time.Second,
				UptimeMs:   1000,
				Devices:    []domain.Device{},
			},
		},
		{
			// RSSI is sint32 on the wire; a sign flip would surface as a huge
			// positive number rather than as an error.
			name: "extreme rssi keeps its sign",
			batch: domain.DetectionBatch{
				SensorID:   "sensor-3",
				ObservedAt: observedAt,
				Window:     time.Second,
				UptimeMs:   0,
				Devices: []domain.Device{
					{MAC: "aa:bb:cc:dd:ee:ff", RSSI: -100, Channel: 1, Frames: 1},
					{MAC: "11:22:33:44:55:66", RSSI: -1, Channel: 165, Frames: 65535, RandomMAC: true},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := enc.EncodeDetectionBatch(&tt.batch)
			if err != nil {
				t.Fatalf("EncodeDetectionBatch() error = %v", err)
			}

			decoded, err := enc.DecodeDetectionBatch(data)
			if err != nil {
				t.Fatalf("DecodeDetectionBatch() error = %v", err)
			}

			want := tt.batch
			want.ObservedAt = normalizedTime(want.ObservedAt)
			if !reflect.DeepEqual(*decoded, want) {
				t.Errorf("round-trip mismatch\n got: %+v\nwant: %+v", *decoded, want)
			}
		})
	}
}

func TestEncodeDecodeCSIWindow(t *testing.T) {
	enc := testEncoder(t)
	observedAt := time.Now()

	tests := []struct {
		name string
		csi  domain.CSIWindow
	}{
		{
			name: "valid CSI window",
			csi: domain.CSIWindow{
				SensorID:    "sensor-1",
				Packets:     100,
				MotionScore: 0.75,
				Subcarriers: 30,
				ObservedAt:  observedAt,
				Window:      5 * time.Second,
				UptimeMs:    1000,
			},
		},
		{
			// proto3 omits zero values on the wire; they must still decode back
			// as zeroes rather than trip the missing-field checks.
			name: "zero counters",
			csi: domain.CSIWindow{
				SensorID:    "sensor-2",
				Packets:     0,
				MotionScore: 0,
				Subcarriers: 0,
				ObservedAt:  observedAt,
				Window:      time.Second,
				UptimeMs:    0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := enc.EncodeCSIWindow(&tt.csi)
			if err != nil {
				t.Fatalf("EncodeCSIWindow() error = %v", err)
			}

			decoded, err := enc.DecodeCSIWindow(data)
			if err != nil {
				t.Fatalf("DecodeCSIWindow() error = %v", err)
			}

			want := tt.csi
			want.ObservedAt = normalizedTime(want.ObservedAt)
			if !reflect.DeepEqual(*decoded, want) {
				t.Errorf("round-trip mismatch\n got: %+v\nwant: %+v", *decoded, want)
			}
		})
	}
}

// A nil timestamp decodes to the Unix epoch rather than a zero time, so the
// decoders reject it explicitly instead of passing a plausible date on.
func TestDecodeRejectsMissingTimeFields(t *testing.T) {
	enc := testEncoder(t)

	decodeBatch := func(b []byte) error {
		_, err := enc.DecodeDetectionBatch(b)
		return err
	}
	decodeCSI := func(b []byte) error {
		_, err := enc.DecodeCSIWindow(b)
		return err
	}

	tests := []struct {
		name    string
		msg     any
		decode  func([]byte) error
		wantErr error
	}{
		{
			name:    "detection batch without observed_at",
			msg:     &aircountv1.DetectionBatch{SensorId: "s", Window: durationpb.New(5 * time.Second)},
			decode:  decodeBatch,
			wantErr: ErrMissingObservedAt,
		},
		{
			name:    "detection batch without window",
			msg:     &aircountv1.DetectionBatch{SensorId: "s", ObservedAt: timestamppb.Now()},
			decode:  decodeBatch,
			wantErr: ErrMissingWindow,
		},
		{
			name:    "csi without observed_at",
			msg:     &aircountv1.CSI{SensorId: "s", Window: durationpb.New(5 * time.Second)},
			decode:  decodeCSI,
			wantErr: ErrMissingObservedAt,
		},
		{
			name:    "csi without window",
			msg:     &aircountv1.CSI{SensorId: "s", ObservedAt: timestamppb.Now()},
			decode:  decodeCSI,
			wantErr: ErrMissingWindow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := enc.serde.Encode(tt.msg)
			if err != nil {
				t.Fatalf("serde.Encode() error = %v", err)
			}
			if err := tt.decode(data); !errors.Is(err, tt.wantErr) {
				t.Errorf("decode error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// The Confluent wire header is magic byte, 4-byte big-endian schema ID, then
// the protobuf message indexes as zigzag varints ([0] collapses to one zero
// byte). A round-trip cannot catch a wrong index because encode and decode
// share it, but external consumers resolve the message type through it — so
// assert the bytes directly.
func TestEncodedHeader(t *testing.T) {
	enc := testEncoder(t)

	header := func(id int, index ...byte) []byte {
		h := []byte{0}
		h = binary.BigEndian.AppendUint32(h, uint32(id))
		return append(h, index...)
	}

	csi, err := enc.EncodeCSIWindow(&domain.CSIWindow{
		SensorID: "s", ObservedAt: time.Now(), Window: time.Second,
	})
	if err != nil {
		t.Fatalf("EncodeCSIWindow() error = %v", err)
	}
	// CSI is the only message in csi.proto: index 0.
	if want := header(testCSISchemaID, 0x00); !bytes.HasPrefix(csi, want) {
		t.Errorf("CSI header = % x, want prefix % x", csi[:min(len(csi), len(want))], want)
	}

	batch, err := enc.EncodeDetectionBatch(&domain.DetectionBatch{
		SensorID: "s", ObservedAt: time.Now(), Window: time.Second,
	})
	if err != nil {
		t.Fatalf("EncodeDetectionBatch() error = %v", err)
	}
	// DetectionBatch is the second message in detection.proto: index 1,
	// encoded as count=1 then index=1.
	if want := header(testDetectionSchemaID, 0x02, 0x02); !bytes.HasPrefix(batch, want) {
		t.Errorf("DetectionBatch header = % x, want prefix % x", batch[:min(len(batch), len(want))], want)
	}
}

// The schema ID is ignored on decode, but the message index still identifies
// the type — decoding a CSI record as a detection batch (and vice versa) must
// fail rather than silently yield a plausible-looking value.
func TestDecodeRejectsWrongMessageType(t *testing.T) {
	enc := testEncoder(t)

	csi, err := enc.EncodeCSIWindow(&domain.CSIWindow{
		SensorID:   "sensor-1",
		Packets:    1200,
		ObservedAt: time.Now(),
		Window:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("EncodeCSIWindow() error = %v", err)
	}
	if _, err := enc.DecodeDetectionBatch(csi); !errors.Is(err, ErrWrongMessageType) {
		t.Errorf("DecodeDetectionBatch() on a CSI record: error = %v, want ErrWrongMessageType", err)
	}

	batch, err := enc.EncodeDetectionBatch(&domain.DetectionBatch{
		SensorID:   "sensor-1",
		ObservedAt: time.Now(),
		Window:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("EncodeDetectionBatch() error = %v", err)
	}
	if _, err := enc.DecodeCSIWindow(batch); !errors.Is(err, ErrWrongMessageType) {
		t.Errorf("DecodeCSIWindow() on a detection batch: error = %v, want ErrWrongMessageType", err)
	}
}

// Schema IDs differ across registries and grow as the schema evolves, so the
// decoders must accept records stamped with an ID they have never seen.
func TestDecodeAcceptsForeignSchemaID(t *testing.T) {
	enc := testEncoder(t)

	other := newEncoder(testDetectionSchemaID+900, testCSISchemaID+900)
	want := domain.DetectionBatch{
		SensorID:   "sensor-1",
		ObservedAt: normalizedTime(time.Now()),
		Window:     5 * time.Second,
		Devices:    []domain.Device{},
	}
	data, err := other.EncodeDetectionBatch(&want)
	if err != nil {
		t.Fatalf("EncodeDetectionBatch() error = %v", err)
	}

	decoded, err := enc.DecodeDetectionBatch(data)
	if err != nil {
		t.Fatalf("DecodeDetectionBatch() with a foreign schema id: error = %v", err)
	}
	if !reflect.DeepEqual(*decoded, want) {
		t.Errorf("round-trip mismatch\n got: %+v\nwant: %+v", *decoded, want)
	}
}
