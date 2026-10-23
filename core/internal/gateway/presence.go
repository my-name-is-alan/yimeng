package gateway

import (
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/events"
)

// Presence tracks which devices currently hold a WebSocket. A device with
// count > 0 is online; transitions publish device.online / device.offline.
type Presence struct {
	Hub *events.Hub
	mu  sync.Mutex
	n   map[string]int
	at  map[string]time.Time
}

func NewPresence(hub *events.Hub) *Presence {
	return &Presence{Hub: hub, n: map[string]int{}, at: map[string]time.Time{}}
}

func (p *Presence) Connect(id string) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	was := p.n[id]
	p.n[id]++
	p.at[id] = time.Now().UTC()
	p.mu.Unlock()
	if was == 0 {
		p.Hub.Publish("device.online", map[string]any{"device_id": id, "at": time.Now().UTC()})
	}
}

func (p *Presence) Disconnect(id string) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	p.n[id]--
	if p.n[id] <= 0 {
		delete(p.n, id)
		delete(p.at, id)
		p.mu.Unlock()
		p.Hub.Publish("device.offline", map[string]any{"device_id": id, "at": time.Now().UTC()})
		return
	}
	p.mu.Unlock()
}

func (p *Presence) Online(id string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n[id] > 0
}

func (p *Presence) Snapshot() []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.n))
	for id := range p.n {
		out = append(out, id)
	}
	return out
}
