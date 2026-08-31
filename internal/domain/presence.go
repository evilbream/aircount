package domain

import (
	"time"

	"github.com/rs/zerolog/log"
)

const (
	presenceMotionThreshold       = 0.06
	presenceMotionMinPacketSample = 20
)

type PresenceRF struct {
	SensorID       string
	MotionDetected bool
	Packets        uint32
	Score          float64
	ObservedAt     time.Time
	Window         time.Duration
}

func CalcualtePresenceRF(csiWindows []*CSIWindow) PresenceRF {
	var sensorID string
	var weightedSum float64
	var packetCount uint32
	var totalWindow time.Duration
	var observedAt time.Time

	for _, csiWindow := range csiWindows {
		if sensorID == "" {
			sensorID = csiWindow.SensorID
		}

		weightedSum += csiWindow.MotionScore * float64(csiWindow.Packets)
		packetCount += csiWindow.Packets
		totalWindow += csiWindow.Window

		if observedAt.IsZero() || csiWindow.ObservedAt.After(observedAt) {
			observedAt = csiWindow.ObservedAt
		}

	}

	avgScore := 0.0
	if packetCount > 0 {
		avgScore = weightedSum / float64(packetCount)
	}
	motionDetected := packetCount >= presenceMotionMinPacketSample && avgScore >= presenceMotionThreshold
	log.Debug().Str("sensor_id", sensorID).Bool("motion_detected", motionDetected)
	return PresenceRF{
		SensorID:       sensorID,
		MotionDetected: motionDetected,
		Packets:        packetCount,
		Score:          avgScore,
		ObservedAt:     observedAt,
		Window:         totalWindow,
	}
}
