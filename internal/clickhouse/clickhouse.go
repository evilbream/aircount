package clickhouse

import (
	"context"
	"poltergeist/internal/config"
	"strconv"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

func Open(ctx context.Context, cfg config.ClickHouse) (driver.Conn, error) {
	conn, err := ch.Open(&ch.Options{
		Addr: []string{cfg.Host + ":" + strconv.Itoa(cfg.Port)},
		Auth: ch.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
	})
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(ctx); err != nil {
		return nil, err
	}

	return conn, nil
}
