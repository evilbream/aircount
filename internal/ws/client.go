package ws

import (
	"context"
	"sync"
	"time"
	"uuid"

	"github.com/coder/websocket"
	"github.com/rs/zerolog/log"
)

type Client struct {
	id           string
	conn         *websocket.Conn
	done         chan struct{}
	once         sync.Once
	isSubscribed bool
	channels     map[string]struct{}
}

type Protocol interface {
	Handle(ctx context.Context, s Session, message []byte) error
	OnConnect(ctx context.Context, s Session) error
	OnDisconnect(ctx context.Context, s Session) error
}

type Session interface {
	ID() string
	Send(ctx context.Context, message []byte) error
}

func (c *Client) ID() string {
	return c.id
}

func (c *Client) IsSubscribed() bool {
	return c.isSubscribed
}

func (c *Client) Subscribe(channels ...string) []string {
	c.isSubscribed = true
	return channels
}

func (c *Client) Unsubscribe(channels ...string) []string {
	c.isSubscribed = false
	return channels
}

func NewClient(ctx context.Context, conn *websocket.Conn) *Client {
	cl := &Client{
		id:   uuid.NewV7().String(),
		conn: conn,
		done: make(chan struct{}),
	}
	go cl.pingLoop(ctx)
	return cl
}

func (c *Client) close() {
	c.once.Do(func() {
		close(c.done)
	})
}

func (c *Client) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.conn.Ping(ctx); err != nil {
				log.Error().Err(err).Msg("failed to send ping to client")
				return
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) ReadLoop(ctx context.Context, handler Protocol) {
	for {
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		default:
			_, msg, err := c.conn.Read(ctx)
			if err != nil {
				log.Error().Err(err).Msg("failed to read message from client")
				c.conn.Close(websocket.StatusInternalError, "failed to read message from client")
				return
			}
			if err := handler.Handle(ctx, c, msg); err != nil {
				log.Error().Err(err).Msg("handler returned an error")
			}

		}
	}
}

func (c *Client) Send(ctx context.Context, message []byte) error {
	return c.write(ctx, message)
}

func (c *Client) write(ctx context.Context, message []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeWait)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, message); err != nil {
		log.Error().Err(err).Msg("failed to write message to client")
		c.conn.Close(websocket.StatusInternalError, "failed to write message to client")
		return err
	}
	return nil
}

func (c *Client) Close() {
	c.close()
	c.conn.Close(websocket.StatusNormalClosure, "client closed")
}

func (c *Client) read(ctx context.Context) ([]byte, error) {
	_, message, err := c.conn.Read(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to read message from client")
		c.conn.Close(websocket.StatusInternalError, "failed to read message from client")
		return nil, err
	}
	return message, nil
}
