package simplews

import (
	"context"
	"encoding/json"
	"poltergeist/internal/uiwire"
	"poltergeist/internal/ws"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	pingPeriod = 25 * time.Second // и heartbeat_ms в hello
	pongWait   = 10 * time.Second // таймаут на один conn.Ping
	writeWait  = 10 * time.Second
)

type MessageHead struct {
	Op string `json:"op"`
	ID string `json:"id"`
}

type Service struct {
	hub Hub
}

type Hub interface {
	Subscribe(s ws.Session, channels ...string) []string
	Unsubscribe(s ws.Session, channels ...string) []string
	RemoveClient(s ws.Session)
}

func NewService(hub Hub) *Service {
	return &Service{
		hub: hub,
	}
}

func (ser *Service) Handle(ctx context.Context, s ws.Session, message []byte) error {
	log.Info().Str("session_id", s.ID()).Bytes("message", message).Msg("received message")
	head := &MessageHead{}
	if err := json.Unmarshal(message, head); err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to unmarshal message head")
		return err
	}

	switch head.Op {
	case string(uiwire.OpPing):
		return ser.OnPing(ctx, s, head.ID)
	case string(uiwire.OpSubscribe):
		subMsg := &uiwire.Subscribe{}
		if err := json.Unmarshal(message, subMsg); err != nil {
			log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to unmarshal subscribe message")
			return err
		}
		return ser.OnSubscribe(ctx, s, *subMsg)
	case string(uiwire.OpUnsubscribe):
		unSubMsg := &uiwire.Unsubscribe{}
		if err := json.Unmarshal(message, unSubMsg); err != nil {
			log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to unmarshal subscribe message")
			return err
		}
		return ser.OnUnsubscribe(ctx, s, *unSubMsg)
	}
	return nil
}

func (ser *Service) OnSubscribe(ctx context.Context, s ws.Session, msg uiwire.Subscribe) error {

	channels := ser.hub.Subscribe(s, msg.Channels...)
	log.Info().Str("session_id", s.ID()).Any("channels", channels).Msg("subscribed to channel")

	ack := uiwire.NewAck(msg.ID, uiwire.OpSubscribe, channels)
	data, err := json.Marshal(ack)
	if err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to marshal subscribe ack")
		return err
	}
	if err := s.Send(ctx, data); err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to send subscribe ack")
		return err
	}
	return nil
}

func (ser *Service) OnUnsubscribe(ctx context.Context, s ws.Session, msg uiwire.Unsubscribe) error {
	channels := ser.hub.Unsubscribe(s, msg.Channels...)
	log.Info().Str("session_id", s.ID()).Any("remains", channels).Msg("unsubscribed from channels, remaining")

	ack := uiwire.NewAck(msg.ID, uiwire.OpUnsubscribe, channels)
	data, err := json.Marshal(ack)
	if err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to marshal subscribe ack")
		return err
	}
	if err := s.Send(ctx, data); err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to send unsubscribe ack")
		return err
	}
	return nil
}

func (ser *Service) OnError(ctx context.Context, s ws.Session, err error) error {
	log.Error().Err(err).Str("session_id", s.ID()).Msg("session encountered an error")
	return nil
}

func (ser *Service) OnPing(ctx context.Context, s ws.Session, id string) error {
	log.Info().Str("session_id", s.ID()).Msg("received ping")
	pongMsg := uiwire.Pong{
		Type:     uiwire.TypePong,
		ID:       id,
		ServerTS: time.Now().UnixMilli(),
	}
	data, err := json.Marshal(pongMsg)
	if err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to marshal pong message")
		return err
	}

	if err := s.Send(ctx, data); err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to send pong")
		return err
	}
	return nil
}

func (ser *Service) OnConnect(ctx context.Context, s ws.Session) error {
	helloMsg := uiwire.NewHello(
		s.ID(), time.Now(), pingPeriod, uiwire.Limits{
			MaxChannels: 10,
			MaxMsgBytes: 1024,
		},
	)
	data, err := json.Marshal(helloMsg)
	if err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to marshal hello message")
		return err
	}
	if err := s.Send(ctx, data); err != nil {
		log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to send hello message")
		return err
	}
	return nil
}

func (ser *Service) OnDisconnect(ctx context.Context, s ws.Session) error {
	ser.hub.RemoveClient(s)
	log.Info().Str("session_id", s.ID()).Msg("session disconnected")
	return nil
}
