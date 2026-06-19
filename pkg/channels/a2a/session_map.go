package a2a

import (
	"sync"

	"github.com/google/uuid"
	"github.com/sipeed/picoclaw/pkg/session"
)

// sessionMap holds the bidirectional mapping between an external (peer-supplied)
// session_id and the local opaque session_key derived from a SessionScope.
type sessionMap struct {
	mu        sync.RWMutex
	ext2local map[string]string // key: "peerID:extID" -> local session_key
	local2ext map[string]string // key: local session_key -> extID or "parentKey:peerID" -> extID
}

func newSessionMap() *sessionMap {
	return &sessionMap{
		ext2local: make(map[string]string),
		local2ext: make(map[string]string),
	}
}

// Resolve looks up the local session_key for an external id and peer ID. If absent,
// it derives a fresh key from a SessionScope scoped to the peer and records
// the mapping. Returns the key and whether a new mapping was created.
func (s *sessionMap) Resolve(extID, peerID string) (string, bool) {
	cacheKey := peerID + ":" + extID
	s.mu.RLock()
	if k, ok := s.ext2local[cacheKey]; ok {
		s.mu.RUnlock()
		return k, false
	}
	s.mu.RUnlock()

	scope := session.SessionScope{
		Version:    1,
		AgentID:    "a2a-incoming",
		Channel:    "a2a",
		Dimensions: []string{"sender"},
		Values:     map[string]string{"sender": peerID},
	}
	localKey := session.BuildSessionKey(scope)

	s.mu.Lock()
	if k, ok := s.ext2local[cacheKey]; ok { // double-check under write lock
		s.mu.Unlock()
		return k, false
	}
	s.ext2local[cacheKey] = localKey
	s.local2ext[localKey] = extID
	s.mu.Unlock()
	return localKey, true
}

// Bind records an explicit external↔local mapping. Used on the initiator
// side when AskPeer creates a session locally before sending the first ask.
func (s *sessionMap) Bind(extID, localKey string) {
	s.mu.Lock()
	// To preserve peerID mapping in Bind, we extract the sender from localKey if needed,
	// but normally for Bind, the localKey is the unique local session key.
	// Since Bind is on initiator side and we have localKey -> extID, we can map:
	s.local2ext[localKey] = extID
	// To support LocalFor on initiator side, we record:
	s.ext2local[extID] = localKey
	s.mu.Unlock()
}

// GetOrCreate retrieves an existing external session ID for a local parent key
// and peer ID. If not found, it generates a new UUID-v4, binds them, and returns it.
func (s *sessionMap) GetOrCreate(parentKey, peerID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	cacheKey := parentKey + ":" + peerID
	if extID, ok := s.local2ext[cacheKey]; ok {
		return extID
	}

	extID := uuid.New().String()
	// Store mapping for this parentKey and peerID
	s.local2ext[cacheKey] = extID
	s.ext2local[peerID+":"+extID] = parentKey
	s.local2ext[parentKey] = extID // also bind parent key directly for reverse lookups on Send
	return extID
}

func (s *sessionMap) LocalFor(extID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// On initiator side, we map extID -> localKey directly
	k, ok := s.ext2local[extID]
	return k, ok
}

func (s *sessionMap) ExternalFor(localKey string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.local2ext[localKey]
	return e, ok
}
