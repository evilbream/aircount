package domain

import "time"

type Band string

const (
	BandWired Band = "wired"
	Band24    Band = "2.4G"
	Band5     Band = "5G"
	Band6     Band = "6G"
)

// observed_at, cpu_usage, mem_usage, wan_ip, clients_online, clients_guest
type RouterStatus struct {
	ObservedAt                  time.Time
	CPU, Mem                    float64 // 0..1
	WANIP                       string
	ClientsOnline, ClientsGuest uint16
}

type RouterSnapshot struct {
	ObservedAt time.Time
	CPU, Mem   float64 // 0..1
	WANIP      string
	Clients    []RouterClient // connected only
}

type RouterClient struct {
	MAC   string // aa:bb:cc:dd:ee:ff, same as Device.MAC on sniffer
	IP    string
	Name  string
	Kind  string // phone, pc, other... (deviceType)
	Band  Band
	Guest bool

	SignalDBm   int       // 0 for linked wired clients
	ConnectedAt time.Time // ObservedAt - onlineTime

	TrafficBytes   uint64  // ttal traffic since connected, 0 for wired
	UpBps, DownBps uint64  // bytes/c
	TxMbps, RxMbps float64 // wifi-link Mbps, 0 for wired
}

type ClientEventKind string

const (
	ClientJoined ClientEventKind = "joined"
	ClientLeft   ClientEventKind = "left"
)

type ClientEvent struct {
	Kind   ClientEventKind
	Client RouterClient
	At     time.Time
}

func DiffClients(prev, cur RouterSnapshot) []ClientEvent {
	was := make(map[string]RouterClient, len(prev.Clients))
	for _, c := range prev.Clients {
		was[c.MAC] = c
	}
	var ev []ClientEvent
	for _, c := range cur.Clients {
		if _, ok := was[c.MAC]; !ok {
			ev = append(ev, ClientEvent{ClientJoined, c, cur.ObservedAt})
		}
		delete(was, c.MAC)
	}
	for _, c := range was {
		ev = append(ev, ClientEvent{ClientLeft, c, cur.ObservedAt})
	}
	return ev
}
