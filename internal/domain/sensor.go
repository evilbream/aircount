package domain

import (
	"time"
)

type DetectionBatch struct {
	SensorID   string
	ObservedAt time.Time
	Window     time.Duration
	UptimeMs   uint64
	Devices    []Device
}

type Device struct {
	MAC       string
	RSSI      int32
	Channel   uint32
	Frames    uint32
	RandomMAC bool
}

type CSIWindow struct {
	SensorID    string
	Packets     uint32
	MotionScore float64
	Subcarriers uint32
	ObservedAt  time.Time
	Window      time.Duration
	UptimeMs    uint64
}
