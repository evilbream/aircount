package domain

import (
	"errors"
	"time"
)

type HistoryQuery struct {
	Sensors  []string
	From, To time.Time
}

func NewHistoryQuery(sensors []string, from, to time.Time) (HistoryQuery, error) {
	if from.After(to) {
		return HistoryQuery{}, errors.New("'from' cannot be after 'to'")
	}
	return HistoryQuery{
		Sensors: sensors,
		From:    from,
		To:      to,
	}, nil
}
