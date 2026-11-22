package tplink

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

type Band string

const (
	BandWired Band = "wired"
	Band24    Band = "2.4G"
	Band5     Band = "5G"
	Band6     Band = "6G"
)

type Device struct {
	MAC      string // lowercase aa:bb:cc:..., same as sniffer `relayed`
	IP       string
	Hostname string
	Band     Band
	Guest    bool
	TxPkts   uint64 // admin/wireless?form=statistics; 0 = unknown
	RxPkts   uint64
}

type Snapshot struct {
	WANMAC, LANMAC, WANIP, LANIP, Gateway string
	CPU, Mem                              float64 // 0..1
	Wifi24, Wifi5, Guest24, Guest5        bool
	Devices                               []Device
}

type rawDevice struct {
	MAC      string `json:"macaddr"`
	IP       string `json:"ipaddr"`
	Hostname string `json:"hostname"`
	WireType string `json:"wire_type"`
}

type rawSnapshot struct {
	WANMAC  string      `json:"wan_macaddr"`
	LANMAC  string      `json:"lan_macaddr"`
	WANIP   string      `json:"wan_ipv4_ipaddr"`
	LANIP   string      `json:"lan_ipv4_ipaddr"`
	Gateway string      `json:"wan_ipv4_gateway"`
	CPU     num         `json:"cpu_usage"`
	Mem     num         `json:"mem_usage"`
	Wifi24  string      `json:"wireless_2g_enable"` // "on" / "off"
	Wifi5   string      `json:"wireless_5g_enable"`
	Guest24 string      `json:"guest_2g_enable"`
	Guest5  string      `json:"guest_5g_enable"`
	Wired   []rawDevice `json:"access_devices_wired"`
	Host    []rawDevice `json:"access_devices_wireless_host"`
	Guest   []rawDevice `json:"access_devices_wireless_guest"`
}

type rawStat struct {
	MAC  string `json:"mac"`
	Type string `json:"type"`
	Tx   num    `json:"txpkts"`
	Rx   num    `json:"rxpkts"`
}

func addDevices(devices []Device, list []rawDevice, guest bool) []Device {
	for _, d := range list {
		devices = append(devices, Device{
			MAC: normMAC(d.MAC), IP: d.IP, Hostname: d.Hostname,
			Band: bandOf(d.WireType), Guest: guest,
		})
	}
	return devices
}

func (c *Client) GetSnapshot(ctx context.Context) (Snapshot, error) {
	var r rawSnapshot
	if err := c.call(ctx, "admin/status?form=all", "read", &r); err != nil {
		return Snapshot{}, err
	}
	ss := Snapshot{
		WANMAC: normMAC(r.WANMAC), LANMAC: normMAC(r.LANMAC),
		WANIP: r.WANIP, LANIP: r.LANIP, Gateway: r.Gateway,
		CPU: float64(r.CPU), Mem: float64(r.Mem),
		Wifi24: r.Wifi24 == "on", Wifi5: r.Wifi5 == "on",
		Guest24: r.Guest24 == "on", Guest5: r.Guest5 == "on",
	}

	ss.Devices = addDevices(ss.Devices, r.Wired, false)
	ss.Devices = addDevices(ss.Devices, r.Host, false)
	ss.Devices = addDevices(ss.Devices, r.Guest, true)

	var stats []rawStat
	if err := c.call(ctx, "admin/wireless?form=statistics", "load", &stats); err != nil {
		log.Debug().Err(err).Msg("tplink: wireless statistics unavailable")
	}

	idx := make(map[string]int, len(ss.Devices))
	for i, d := range ss.Devices {
		idx[d.MAC] = i
	}
	for _, s := range stats {
		mac := normMAC(s.MAC)
		i, ok := idx[mac]
		if !ok {
			ss.Devices = append(ss.Devices, Device{MAC: mac, Band: bandOf(s.Type)})
			i = len(ss.Devices) - 1
		}
		ss.Devices[i].TxPkts, ss.Devices[i].RxPkts = uint64(s.Tx), uint64(s.Rx)
	}
	return ss, nil
}

type Lease struct{ Mac, IP, Name, LeaseTime string }

func (c *Client) GetDHCPLeases(ctx context.Context) ([]Lease, error) {
	var raw []struct {
		MAC       string `json:"macaddr"`
		IP        string `json:"ipaddr"`
		Name      string `json:"name"`
		LeaseTime string `json:"leasetime"`
	}
	if err := c.call(ctx, "admin/dhcps?form=client", "load", &raw); err != nil {
		return nil, err
	}
	out := make([]Lease, 0, len(raw))
	for _, r := range raw {
		out = append(out, Lease{normMAC(r.MAC), r.IP, r.Name, r.LeaseTime})
	}
	return out, nil
}

// ClientInfo is one entry of the Game Accelerator device list
// (admin/smart_network?form=game_accelerator, loadDevice).
type ClientInfo struct {
	MAC  string // lowercase aa:bb:cc:..., same as Device.MAC
	IP   string
	Name string // DHCP hostname or the name set in the router UI
	Type string // UI icon category: pc, phone, other, ...
	Band Band

	Signal int           // RSSI at the router, dBm; 0 for wired
	Online time.Duration // since the device connected

	Traffic         uint64 // total bytes passed through the device
	UpBytesPerSec   uint64 // current upload speed (direction assumed client→router)
	DownBytesPerSec uint64 // current download speed

	TxMbps float64 // Wi-Fi PHY link rate router→client
	RxMbps float64 // Wi-Fi PHY link rate client→router

	Priority       bool          // QoS "game accelerator" priority enabled
	PriorityRemain time.Duration // left of a time-limited priority; 0 = off or unlimited
}

type rawClientInfo struct {
	MAC            string `json:"mac"`
	IP             string `json:"ip"`
	Name           string `json:"deviceName"`
	Type           string `json:"deviceType"`
	Tag            string `json:"deviceTag"` // "5G", "2.4G", "wired"
	Signal         num    `json:"signal"`
	OnlineTime     num    `json:"onlineTime"`    // seconds
	TrafficUsage   num    `json:"trafficUsage"`  // bytes
	UploadSpeed    num    `json:"uploadSpeed"`   // bytes/s
	DownloadSpeed  num    `json:"downloadSpeed"` // bytes/s
	TxRate         num    `json:"txrate"`        // kbit/s
	RxRate         num    `json:"rxrate"`        // kbit/s
	EnablePriority bool   `json:"enablePriority"`
	RemainTime     num    `json:"remainTime"` // seconds
}

func seconds(n num) time.Duration { return time.Duration(float64(n) * float64(time.Second)) }

func (c *Client) GetClientsInfo(ctx context.Context) ([]ClientInfo, error) {
	var raw []rawClientInfo
	if err := c.call(ctx, "admin/smart_network?form=game_accelerator", "loadDevice", &raw); err != nil {
		return nil, err
	}

	out := make([]ClientInfo, 0, len(raw))
	for _, r := range raw {
		if r.Tag == "offline" || r.IP == "0.0.0.0" {
			continue // skip offline devices
		}
		out = append(out, ClientInfo{
			MAC:             normMAC(r.MAC),
			IP:              r.IP,
			Name:            r.Name,
			Type:            r.Type,
			Band:            bandOf(r.Tag),
			Signal:          int(r.Signal),
			Online:          seconds(r.OnlineTime),
			Traffic:         uint64(r.TrafficUsage),
			UpBytesPerSec:   uint64(r.UploadSpeed),
			DownBytesPerSec: uint64(r.DownloadSpeed),
			TxMbps:          float64(r.TxRate) / 1000,
			RxMbps:          float64(r.RxRate) / 1000,
			Priority:        r.EnablePriority,
			PriorityRemain:  seconds(r.RemainTime),
		})
	}
	return out, nil
}
