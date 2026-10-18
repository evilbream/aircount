package ws

import (
	"context"
	"encoding/json"
	"maps"
	"poltergeist/internal/domain"
	"poltergeist/internal/uiwire"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	writeWait  = 15 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 25 * time.Second
)

type Hub struct {
	mu       sync.RWMutex
	byChan   map[string]map[Session]struct{}
	byClient map[Session]map[string]struct{}

	seqMu sync.Mutex
	seqs  map[string]int64 // per channel, so a gap in seq means a lost event on that channel
}

func (h *Hub) nextSeq(channel string) int64 {
	h.seqMu.Lock()
	defer h.seqMu.Unlock()
	h.seqs[channel]++
	return h.seqs[channel]
}

func (h *Hub) PublishDevices(ctx context.Context, devices domain.DetectionBatch) error {
	// Implement the logic to publish devices to the appropriate channels
	return nil
}
func (h *Hub) PublishCSI(ctx context.Context, csi domain.CSIWindow) error {
	channel := "csi." + csi.SensorID
	event := uiwire.NewEvent(channel, h.nextSeq(channel), csi.ObservedAt, uiwire.FromCsiWindow(csi))
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	h.Publish(ctx, channel, data)
	return nil
}

func (h *Hub) Subscribe(c Session, channels ...string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byClient[c] == nil {
		h.byClient[c] = make(map[string]struct{})
	}
	for _, ch := range channels {
		if h.byChan[ch] == nil {
			h.byChan[ch] = make(map[Session]struct{})
		}
		h.byChan[ch][c] = struct{}{}
		h.byClient[c][ch] = struct{}{}
	}
	return slices.Sorted(maps.Keys(h.byClient[c]))
}

func (h *Hub) Unsubscribe(c Session, channels ...string) []string {
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
		byChan:   make(map[string]map[Session]struct{}),
		byClient: make(map[Session]map[string]struct{}),
		seqs:     make(map[string]int64),
	}
}

func (h *Hub) RemoveClient(c Session) {
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
	subs := make([]Session, 0, len(h.byChan[channel]))
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
