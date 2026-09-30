package ws

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	writeWait  = 15 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 5 * time.Second
)

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*Client]struct{}),
	}
}

func (h *Hub) Register(ctx context.Context, c *websocket.Conn) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	client := NewClient(c)
	go client.pingLoop(ctx)
	h.clients[client] = struct{}{}
	return client
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

func (h *Hub) Broadcast(ctx context.Context, message []byte) {
	h.mu.Lock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		if err := c.write(ctx, message); err != nil {
			c.close()
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
		}
	}

}
