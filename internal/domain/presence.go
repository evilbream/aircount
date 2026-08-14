package domain

import "time"

type PresenceRF struct {
	SensorID       string
	MotionDetected bool
	Packets        uint32
	Score          float64
	ObservedAt     time.Time
	Window         time.Duration
}
