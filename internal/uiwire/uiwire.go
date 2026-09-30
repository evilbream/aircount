package uiwire

import "poltergeist/internal/domain"

type UIWireEv struct {
	SensorID    string  `json:"sensor_id"`
	Timestamp   int64   `json:"timestamp"`
	MotionScore float64 `json:"motion_score"`
}

func FromCsiWindow(rawCSI domain.CSIWindow) UIWireEv {
	return UIWireEv{
		SensorID:    rawCSI.SensorID,
		Timestamp:   rawCSI.ObservedAt.UnixMilli(),
		MotionScore: rawCSI.MotionScore,
	}
}
