package sensorwire

import (
	"encoding/json"
	"errors"
)

var (
	ErrInvalidSensorID = errors.New("sensorwire: empty sensor id")
	ErrInvalidWindowMS = errors.New("sensorwire: window_ms must be > 0")
	ErrInvalidUptimeMS = errors.New("sensorwire: uptime_ms must be >= 0")
)

type Device struct {
	MAC     string `json:"mac"`
	RSSI    int    `json:"rssi"`    // strongest signal level from the device this window
	Channel int    `json:"channel"` // wifi channel it was heard on
	Frames  int    `json:"frames"`  // how many frames from this MAC this window // 1-2 probably just passing by
	Random  bool   `json:"random"`  // whether MAC randomization is in use
}

type CSI struct {
	Packets     int     `json:"packets"`
	MotionScore float64 `json:"motion_score"`
	Subcarriers int     `json:"subcarriers"`
}

type Batch struct {
	SensorID string   `json:"sensor_id"`
	UptimeMS int64    `json:"uptime_ms"`
	WindowMS int      `json:"window_ms"`
	Devices  []Device `json:"devices"`
	CSI      CSI      `json:"csi"`
}

func Decode(data []byte) (Batch, error) {
	var batch Batch
	if err := json.Unmarshal(data, &batch); err != nil {
		return Batch{}, err
	}
	if err := batch.Validate(); err != nil {
		return Batch{}, err
	}
	return batch, nil
}

func (b *Batch) Validate() error {
	if b.SensorID == "" {
		return ErrInvalidSensorID
	}
	if b.WindowMS <= 0 {
		return ErrInvalidWindowMS
	}
	if b.UptimeMS < 0 {
		return ErrInvalidUptimeMS
	}
	return nil
}
