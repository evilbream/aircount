package clickhouse

import (
	"poltergeist/internal/domain"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type Reader struct {
	conn driver.Conn
}

func NewReader(conn driver.Conn) *Reader {
	return &Reader{
		conn: conn,
	}
}

func ReadRawCSI(from time.Time, to time.Time) ([]domain.CSIWindow, error) {
	return nil, nil
}
