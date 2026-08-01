package domain

import "time"

type Device struct {
	SensorID   string
	MAC        string
	RSSI       int
	Channel    int
	Frames     int
	RandomMAC  bool
	ObservedAt time.Time
	Window     time.Duration
}

type CSIWindow struct {
	SensorID    string
	Packets     int
	MotionScore float64
	Subcarriers int
	ObservedAt  time.Time
	Window      time.Duration
}
