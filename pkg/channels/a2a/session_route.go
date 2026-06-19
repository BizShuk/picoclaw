package a2a

import (
	"context"
	"sync"
	"time"
)

// sessionRoute holds the A2A reply-routing captured from an inbound request.
// The agent's response path rebuilds the outbound with a fresh context that
// drops the inbound Raw, so Send() recovers these fields to address the reply
// back to the originating peer/session/frame.
type sessionRoute struct {
	sessionID string
	peerID    string
	frameID   string
	lastSeen  time.Time
}

// sessionRouteTable maps an inbound ChatID to its A2A reply route. Entries are
// removed as soon as the reply is sent (server side) / the caller is resolved
// (HTTP side); a janitor goroutine evicts anything left behind after the TTL as
// a backstop against leaks.
type sessionRouteTable struct {
	mu     sync.Mutex
	m      map[string]*sessionRoute
	ttl    time.Duration
	cancel context.CancelFunc
}

func newSessionRouteTable(ttl time.Duration) *sessionRouteTable {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &sessionRouteTable{
		m:   make(map[string]*sessionRoute),
		ttl: ttl,
	}
}

func (t *sessionRouteTable) put(key, sessionID, peerID, frameID string) {
	if key == "" {
		return
	}
	t.mu.Lock()
	t.m[key] = &sessionRoute{
		sessionID: sessionID,
		peerID:    peerID,
		frameID:   frameID,
		lastSeen:  time.Now(),
	}
	t.mu.Unlock()
}

func (t *sessionRouteTable) get(key string) *sessionRoute {
	if key == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.m[key]
	if !ok {
		return nil
	}
	r.lastSeen = time.Now()
	return r
}

func (t *sessionRouteTable) remove(key string) {
	t.mu.Lock()
	delete(t.m, key)
	t.mu.Unlock()
}

// startJanitor launches the TTL reaper goroutine; it runs until ctx is done.
func (t *sessionRouteTable) startJanitor(ctx context.Context) {
	jctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	// Reap several times per TTL window so stale entries don't linger a full day.
	interval := t.ttl / 12
	if interval < time.Minute {
		interval = time.Minute
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-jctx.Done():
				return
			case <-ticker.C:
				t.reapStale()
			}
		}
	}()
}

func (t *sessionRouteTable) stop() {
	if t.cancel != nil {
		t.cancel()
	}
}

func (t *sessionRouteTable) reapStale() {
	cutoff := time.Now().Add(-t.ttl)
	t.mu.Lock()
	for key, r := range t.m {
		if r.lastSeen.Before(cutoff) {
			delete(t.m, key)
		}
	}
	t.mu.Unlock()
}
