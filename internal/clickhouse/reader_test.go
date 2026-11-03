package clickhouse

import (
	"poltergeist/internal/config"
	"poltergeist/internal/domain"
	"testing"
	"time"
)

func TestReader(t *testing.T) {
	conn, err := Open(t.Context(), config.ClickHouse{
		Host:     "192.168.0.61",
		Port:     9000,
		User:     "aircount",
		Password: "aircount",
		Database: "maindb",
	})
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	defer conn.Close()

	rows, err := conn.Query(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	now := time.Now()

	history, err := NewReader(conn).CSIWindows(t.Context(), domain.HistoryQuery{
		Sensors: []string{"kitchen", "living-room"},
		From:    now.Add(-time.Hour),
		To:      now,
	})
	if err != nil {
		t.Fatalf("failed to get history: %v", err)
	}
	if history == nil {
		t.Fatalf("history is nil")
	}
}
