package gateway

import "time"

// Diagnostics exposes only fixed event names and counts, never request bodies,
// addresses, authentication material or PCM content. Reading it touches no device.
func (g *Gateway) Diagnostics() map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	events := make(map[string]uint64, len(g.events))
	for name, count := range g.events {
		events[name] = count
	}
	return map[string]any{"paired_clients": len(g.keys), "pcm_active": g.pcm,
		"events": events, "last_event": g.lastEvent, "last_event_at": g.lastEventAt}
}

// Call with implementation-defined constants only.
func (g *Gateway) event(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.events == nil {
		g.events = make(map[string]uint64)
	}
	g.events[name]++
	g.lastEvent = name
	g.lastEventAt = time.Now().UTC().Format(time.RFC3339)
}
