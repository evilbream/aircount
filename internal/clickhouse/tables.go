package clickhouse

import (
	"time"

	"poltergeist/internal/domain"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type table[T any] struct {
	query     string
	appendRow func(driver.Batch, T) error
}

var csiTable = table[domain.CSIWindow]{
	query: "INSERT INTO maindb.csi_windows (observed_at, sensor_id, motion_score, packets, subcarriers, window_ms, uptime_ms)",
	appendRow: func(b driver.Batch, r domain.CSIWindow) error {
		return b.Append(r.ObservedAt, r.SensorID, r.MotionScore, r.Packets,
			uint16(r.Subcarriers), uint32(r.Window.Milliseconds()), r.UptimeMs)
	},
}

var routerStatusTable = table[domain.RouterSnapshot]{
	query: "INSERT INTO maindb.router_status (observed_at, cpu_usage, mem_usage, wan_ip, clients_online, clients_guest)",
	appendRow: func(b driver.Batch, s domain.RouterSnapshot) error {
		var guests uint16
		for _, c := range s.Clients {
			if c.Guest {
				guests++
			}
		}
		return b.Append(s.ObservedAt, float32(s.CPU), float32(s.Mem), ipOrZero(s.WANIP),
			uint16(len(s.Clients)), guests)
	},
}

var deviceStateTable = table[domain.DeviceState]{
	query: "INSERT INTO maindb.device_state (mac, observed_at, online, presence_detected, motion_rooms, score)",
	appendRow: func(b driver.Batch, s domain.DeviceState) error {
		rooms := s.MotionRooms
		if rooms == nil {
			rooms = []string{}
		}
		return b.Append(s.MAC, s.ObservedAt, s.Online, s.PresenceDetected, rooms, float32(s.Score))
	},
}

type clientRow struct {
	ObservedAt time.Time
	domain.RouterClient
}

var routerClientsTable = table[clientRow]{
	query: "INSERT INTO maindb.router_clients (observed_at, mac, ip, name, kind, band, guest, signal_dbm, connected_at, traffic_bytes)",
	appendRow: func(b driver.Batch, r clientRow) error {
		return b.Append(r.ObservedAt, r.MAC, ipOrZero(r.IP), r.Name, r.Kind, string(r.Band),
			r.Guest, int8(r.SignalDBm), r.ConnectedAt, r.TrafficBytes)
	},
}

func ipOrZero(ip string) string {
	if ip == "" {
		return "0.0.0.0"
	}
	return ip
}
