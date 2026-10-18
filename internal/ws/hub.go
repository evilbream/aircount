package ws

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	writeWait  = 15 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 5 * time.Second
)

type Hub struct {
	mu       sync.RWMutex
	byChan   map[string]map[*Client]struct{}
	byClient map[*Client]map[string]struct{}
}

func (h *Hub) Subscribe(c *Client, channels ...string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byClient[c] == nil {
		h.byClient[c] = make(map[string]struct{})
	}
	for _, ch := range channels {
		if h.byChan[ch] == nil {
			h.byChan[ch] = make(map[*Client]struct{})
		}
		h.byChan[ch][c] = struct{}{}
		h.byClient[c][ch] = struct{}{}
	}
	return slices.Sorted(maps.Keys(h.byClient[c]))
}

func (h *Hub) Unsubscribe(c *Client, channels ...string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byClient[c] == nil {
		return nil
	}
	for _, ch := range channels {
		delete(h.byChan[ch], c)
		delete(h.byClient[c], ch)
	}
	return slices.Sorted(maps.Keys(h.byClient[c]))
}

func NewHub() *Hub {
	return &Hub{
		byChan:   make(map[string]map[*Client]struct{}),
		byClient: make(map[*Client]map[string]struct{}),
	}
}

func (h *Hub) RemoveClient(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byClient[c] == nil {
		return
	}
	for ch := range h.byClient[c] {
		delete(h.byChan[ch], c)
	}
	delete(h.byClient, c)
}

func (h *Hub) Publish(ctx context.Context, channel string, message []byte) {
	h.mu.RLock()
	subs := make([]*Client, 0, len(h.byChan[channel]))
	for c := range h.byChan[channel] {
		subs = append(subs, c)
	}
	h.mu.RUnlock()
	for _, c := range subs {
		if err := c.Send(ctx, message); err != nil {
			log.Error().Err(err).Str("session_id", c.ID()).Str("channel", channel).Msg("failed to send message to subscriber")
		}
	}
}
