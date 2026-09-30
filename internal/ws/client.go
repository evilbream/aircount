package ws

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog/log"
)

type Client struct {
	conn *websocket.Conn
	done chan struct{}
	once sync.Once
}

func NewClient(conn *websocket.Conn) *Client {
	return &Client{
		conn: conn,
		done: make(chan struct{}),
	}
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
				c.conn.Close(websocket.StatusInternalError, "failed to send ping to client")
				return
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
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
