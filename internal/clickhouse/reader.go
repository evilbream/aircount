package clickhouse

import (
	"context"
	"poltergeist/internal/domain"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const readCSIQuery = `
SELECT sensor_id, observed_at, motion_score
FROM csi_windows
WHERE observed_at >= fromUnixTimestamp64Milli(?)
  AND observed_at <  fromUnixTimestamp64Milli(?)`

type Reader struct {
	conn driver.Conn
}

func NewReader(conn driver.Conn) *Reader {
	return &Reader{
		conn: conn,
	}
}

func (r *Reader) CSIWindows(ctx context.Context, q domain.HistoryQuery) ([]domain.CSIWindow, error) {
	query := readCSIQuery
	args := []any{q.From.UnixMilli(), q.To.UnixMilli()}
	if len(q.Sensors) > 0 {
		query += "\n AND sensor_id IN ?"
		args = append(args, q.Sensors)
	}
	query += "\nORDER BY observed_at"
	// Implement the actual ClickHouse query logic here
	rows, err := r.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ws []domain.CSIWindow
	for rows.Next() {
		var w domain.CSIWindow
		if err := rows.Scan(&w.SensorID, &w.ObservedAt, &w.MotionScore); err != nil {
			return nil, err
		}
		ws = append(ws, w)
	}
	return ws, rows.Err()

}
