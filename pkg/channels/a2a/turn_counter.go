package a2a

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrMaxTurnExceeded = errors.New("max turn limit exceeded")
	ErrSessionEnded    = errors.New("session already ended")
)

type turnCounter struct {
	mu       sync.Mutex
	counts   map[string]int
	endedSet map[string]time.Time
	ttl      time.Duration
}

func newTurnCounter() *turnCounter {
	return &turnCounter{
		counts:   make(map[string]int),
		endedSet: make(map[string]time.Time),
		ttl:      5 * time.Minute,
	}
}

func (tc *turnCounter) Current(sessionID string) int {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.counts[sessionID]
}

// CheckAndIncrement verifies if the next turn (current+1) exceeds maxTurn.
// If it exceeds, returns ErrMaxTurnExceeded.
// If the session has already ended within the TTL, returns ErrSessionEnded.
// Otherwise, it increments the turn count and returns the new count.
func (tc *turnCounter) CheckAndIncrement(sessionID string, maxTurn int) (int, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Clean up stale ended sessions first (lazy cleanup)
	now := time.Now()
	for sid, t := range tc.endedSet {
		if now.Sub(t) > tc.ttl {
			delete(tc.endedSet, sid)
		}
	}

	if _, ended := tc.endedSet[sessionID]; ended {
		return 0, ErrSessionEnded
	}

	current := tc.counts[sessionID]
	next := current + 1
	if next > maxTurn {
		return 0, ErrMaxTurnExceeded
	}

	tc.counts[sessionID] = next
	return next, nil
}

// MarkDone moves the session into the ended set and clears its turn count.
func (tc *turnCounter) MarkDone(sessionID string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.endedSet[sessionID] = time.Now()
	delete(tc.counts, sessionID)
}
