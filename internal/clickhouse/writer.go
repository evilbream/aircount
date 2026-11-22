package clickhouse

import (
	"context"
	"poltergeist/internal/domain"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

type Writer struct {
	csi         *BatchWriter[domain.CSIWindow]
	snapshot    *BatchWriter[domain.RouterSnapshot]
	clients     *BatchWriter[clientRow]
	runCSI      bool
	runSnapshot bool
}

func (w *Writer) PublishCSI(ctx context.Context, csi domain.CSIWindow) error {
	w.csi.Add(csi)
	return nil
}
func (w *Writer) PublishDevices(ctx context.Context, d domain.DetectionBatch) error {
	return nil
}

func (w *Writer) PublishRouterSnapshot(ctx context.Context, s domain.RouterSnapshot) error {
	w.snapshot.Add(s)
	clients := make([]clientRow, len(s.Clients))
	for i, c := range s.Clients {
		clients[i] = clientRow{
			ObservedAt:   s.ObservedAt,
			RouterClient: c,
		}
	}
	w.clients.Add(clients...)
	return nil
}

func NewWriter(conn driver.Conn) *Writer {
	return &Writer{
		csi:         NewBatchWriter(conn, csiTable),
		snapshot:    NewBatchWriter(conn, routerStatusTable),
		clients:     NewBatchWriter(conn, routerClientsTable),
		runCSI:      false,
		runSnapshot: false,
	}
}

func (w *Writer) SwitchCSI(enable bool) {
	w.runCSI = enable
}

func (w *Writer) SwitchRouterSnapshot(enable bool) {
	w.runSnapshot = enable
}

func (w *Writer) Run(ctx context.Context) error {
	if !w.runCSI && !w.runSnapshot {
		log.Info().Msg("clickhouse writer: no writers enabled, exiting")
		return nil
	}

	g, ctx := errgroup.WithContext(ctx)
	if w.runCSI {
		g.Go(func() error { return w.csi.Run(ctx) })
	}
	if w.runSnapshot {
		g.Go(func() error { return w.snapshot.Run(ctx) })
		g.Go(func() error { return w.clients.Run(ctx) })
	}

	return g.Wait()
}
