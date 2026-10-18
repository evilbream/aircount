package uiwire

import (
	"poltergeist/internal/domain"
	"time"
)

type SubProtocol string

const (
	WsAirCountV1 SubProtocol = "aircount.v1"
)

type MsgType string

const (
	TypeHello   MsgType = "hello"
	TypeAck     MsgType = "ack"
	TypeError   MsgType = "error"
	TypeEvent   MsgType = "event"
	TypeDropped MsgType = "dropped"
	TypePong    MsgType = "pong"
	TypeBye     MsgType = "bye"
)

type Op string

const (
	OpSubscribe   Op = "subscribe"
	OpUnsubscribe Op = "unsubscribe"
	OpPing        Op = "ping"
)

// client methods for subscribing and unsubscribing to channels
type Subscribe struct {
	Op       Op       `json:"op"`
	ID       string   `json:"id"`
	Channels []string `json:"channels"`
}

type Unsubscribe struct {
	Op       Op       `json:"op"`
	ID       string   `json:"id"`
	Channels []string `json:"channels"`
}

type Ping struct {
	Op Op     `json:"op"`
	ID string `json:"id"`
}

// server responses
type Limits struct {
	MaxChannels int `json:"max_channels"`
	MaxMsgBytes int `json:"max_msg_bytes"`
}

type Hello struct {
	Type        MsgType     `json:"type"`
	Protocol    SubProtocol `json:"protocol"`
	ConnID      string      `json:"conn_id"`
	ServerTS    int64       `json:"server_ts"`
	HeartbeatMS int64       `json:"heartbeat_ms"`
	Limits      Limits      `json:"limits"`
}

func NewHello(connID string, now time.Time, heartbeat time.Duration, l Limits) Hello {
	return Hello{
		Type: TypeHello, Protocol: WsAirCountV1, ConnID: connID,
		ServerTS: now.UnixMilli(), HeartbeatMS: heartbeat.Milliseconds(), Limits: l,
	}
}

type Ack struct {
	Type      MsgType  `json:"type"`
	ID        string   `json:"id"`
	Op        Op       `json:"op"`
	Subscibed []string `json:"subscribed"`
}

func NewAck(id string, op Op, subscribed []string) Ack {
	return Ack{
		Type:      TypeAck,
		ID:        id,
		Op:        op,
		Subscibed: subscribed,
	}
}

type Error struct {
	Type    MsgType `json:"type"`
	ID      string  `json:"id"`
	Code    int     `json:"code"`
	Message string  `json:"message"`
}

type Event struct {
	Type    MsgType `json:"type"`
	Channel string  `json:"channel"`
	Seq     int64   `json:"seq"`
	TS      int64   `json:"ts"`
	Payload any     `json:"payload"`
}

func NewEvent(channel string, seq int64, payload any) Event {
	return Event{
		Type:    TypeEvent,
		Channel: channel,
		Seq:     seq,
		TS:      time.Now().UnixMilli(),
		Payload: payload,
	}
}

type Dropped struct {
	Type    MsgType `json:"type"`
	Channel string  `json:"channel"`
	Count   int     `json:"count"`
}

type Pong struct {
	Type     MsgType `json:"type"`
	ID       string  `json:"id"`
	ServerTS int64   `json:"server_ts"`
}

type Bye struct {
	Type           MsgType `json:"type"`
	Reason         string  `json:"reason"`
	ReconnectAfter int64   `json:"reconnect_after"`
}

type UIWireEv struct {
	SensorID    string  `json:"sensor_id"`
	Timestamp   int64   `json:"timestamp"`
	MotionScore float64 `json:"motion_score"`
}

func FromCsiWindow(rawCSI domain.CSIWindow) UIWireEv {
	return UIWireEv{
		SensorID:    rawCSI.SensorID,
		Timestamp:   rawCSI.ObservedAt.UnixMilli(),
		MotionScore: rawCSI.MotionScore,
	}
}
