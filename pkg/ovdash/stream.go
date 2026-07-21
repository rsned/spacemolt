package ovdash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// AgentMove is a system transition; FromSystemID lets the frontend animate
// along the lane between the two systems.
type AgentMove struct {
	Agent        AgentState `json:"agent"`
	FromSystemID string     `json:"from_system_id"`
}

// Delta is the between-snapshots change set pushed over SSE.
type Delta struct {
	Moved       []AgentMove  `json:"moved"`
	Updated     []AgentState `json:"updated"`
	Joined      []AgentState `json:"joined"`
	Left        []string     `json:"left"`
	StaleFleets []string     `json:"stale_fleets"`
}

// Empty reports whether the delta carries no agent changes (stale-fleet
// changes alone still count as content for the caller to decide on).
func (d Delta) Empty() bool {
	return len(d.Moved)+len(d.Updated)+len(d.Joined)+len(d.Left) == 0
}

// Diff computes the change set from prev to cur. Off-map agents are treated
// like on-map ones (keyed by agent_id) so an agent entering/leaving the
// unresolved tray still produces events.
func Diff(prev, cur *Snapshot) Delta {
	d := Delta{StaleFleets: cur.StaleFleets}
	prevByID := map[string]AgentState{}
	if prev != nil {
		for _, a := range append(append([]AgentState{}, prev.Agents...), prev.OffMap...) {
			prevByID[a.AgentID] = a
		}
	}
	seen := map[string]bool{}
	for _, a := range append(append([]AgentState{}, cur.Agents...), cur.OffMap...) {
		seen[a.AgentID] = true
		p, ok := prevByID[a.AgentID]
		switch {
		case !ok:
			d.Joined = append(d.Joined, a)
		case p.SystemID != a.SystemID:
			d.Moved = append(d.Moved, AgentMove{Agent: a, FromSystemID: p.SystemID})
		case p != a:
			d.Updated = append(d.Updated, a)
		}
	}
	for id := range prevByID {
		if !seen[id] {
			d.Left = append(d.Left, id)
		}
	}
	return d
}

// Hub fans SSE events out to connected dashboard clients.
type Hub struct {
	mu       sync.Mutex
	clients  map[chan []byte]struct{}
	keyframe []byte // pre-rendered "event: ...\ndata: ...\n\n" sent on connect
}

func NewHub() *Hub { return &Hub{clients: map[chan []byte]struct{}{}} }

func render(event string, payload any) []byte {
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return []byte("event: " + event + "\ndata: " + string(b) + "\n\n")
}

// SetKeyframe stores the frame every new connection receives first.
func (h *Hub) SetKeyframe(event string, payload any) {
	f := render(event, payload)
	h.mu.Lock()
	h.keyframe = f
	h.mu.Unlock()
}

// Broadcast sends an event to every connected client. A client whose buffer
// is full is dropped (its next keyframe reconnect self-heals) — a stuck
// browser must never block the loop.
func (h *Hub) Broadcast(event string, payload any) {
	f := render(event, payload)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- f:
		default:
			close(ch)
			delete(h.clients, ch)
		}
	}
}

// CloseAll disconnects every client (shutdown and tests).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}

// ServeHTTP is the SSE endpoint.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	kf := h.keyframe
	h.mu.Unlock()

	if kf != nil {
		_, _ = w.Write(kf)
		fl.Flush()
	}
	defer func() {
		h.mu.Lock()
		if _, live := h.clients[ch]; live {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}()
	for {
		select {
		case <-r.Context().Done():
			return
		case f, open := <-ch:
			if !open {
				return
			}
			if _, err := w.Write(f); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
