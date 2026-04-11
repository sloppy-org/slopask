package server

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// sseEvent is a single SSE event to broadcast.
type sseEvent struct {
	Name string
	Data string
}

// subscriber is a channel that receives SSE events.
type subscriber struct {
	ch     chan sseEvent
	roomID int64
}

// sseBroker manages per-room fan-out of SSE events.
type sseBroker struct {
	mu          sync.RWMutex
	subscribers map[*subscriber]struct{}
}

func newSSEBroker() *sseBroker {
	return &sseBroker{
		subscribers: make(map[*subscriber]struct{}),
	}
}

func (b *sseBroker) subscribe(roomID int64) *subscriber {
	s := &subscriber{
		ch:     make(chan sseEvent, 64),
		roomID: roomID,
	}
	b.mu.Lock()
	b.subscribers[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *sseBroker) unsubscribe(s *subscriber) {
	b.mu.Lock()
	delete(b.subscribers, s)
	b.mu.Unlock()
}

// publish sends an event to all subscribers of the given room.
func (b *sseBroker) publish(roomID int64, name, data string) {
	evt := sseEvent{Name: name, Data: data}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subscribers {
		if s.roomID != roomID {
			continue
		}
		select {
		case s.ch <- evt:
		default:
			log.Printf("sse: dropping event for slow subscriber (room %d)", roomID)
		}
	}
}

// serveSSE handles an SSE connection for a room.
func (b *sseBroker) serveSSE(w http.ResponseWriter, r *http.Request, roomID int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sub := b.subscribe(roomID)
	defer b.unsubscribe(sub)

	ctx := r.Context()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-sub.ch:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Name, evt.Data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}
