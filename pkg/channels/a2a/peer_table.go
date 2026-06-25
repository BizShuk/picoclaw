package a2a

import (
	"sync"
	"time"
)

// PeerInfo is the discovery record for one known peer.
type PeerInfo struct {
	AgentID     string
	Host        string
	Port        int
	Version     int
	WSPath      string
	Description string
	CardURL     string `json:"card_url,omitempty"`
	LastSeen    time.Time
}

type peerTable struct {
	mu       sync.RWMutex
	peers    map[string]*PeerInfo
	ttl      time.Duration
	onAdd    func(*PeerInfo)
	onRemove func(string)
}

func newPeerTable(ttl time.Duration) *peerTable {
	return &peerTable{
		peers: make(map[string]*PeerInfo),
		ttl:   ttl,
	}
}

// Upsert inserts or refreshes a peer. onAdd fires only the first time a peer
// is inserted; subsequent calls only refresh LastSeen.
func (pt *peerTable) Upsert(p *PeerInfo) {
	pt.mu.Lock()
	existing, known := pt.peers[p.AgentID]
	now := time.Now()
	p.LastSeen = now
	if known {
		existing.Host = p.Host
		existing.Port = p.Port
		existing.Version = p.Version
		existing.WSPath = p.WSPath
		existing.CardURL = p.CardURL
		existing.LastSeen = now
		pt.mu.Unlock()
		return
	}
	pt.peers[p.AgentID] = p
	cb := pt.onAdd
	pt.mu.Unlock()
	if cb != nil {
		cb(p)
	}
}

func (pt *peerTable) Get(id string) (*PeerInfo, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	p, ok := pt.peers[id]
	return p, ok
}

func (pt *peerTable) List() []*PeerInfo {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	out := make([]*PeerInfo, 0, len(pt.peers))
	for _, p := range pt.peers {
		out = append(out, p)
	}
	return out
}

// ReapStale removes peers whose LastSeen is older than ttl and invokes
// onRemove for each removed peer.
func (pt *peerTable) ReapStale() {
	pt.mu.Lock()
	cutoff := time.Now().Add(-pt.ttl)
	var stale []string
	for id, p := range pt.peers {
		if p.LastSeen.Before(cutoff) {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		delete(pt.peers, id)
	}
	cb := pt.onRemove
	pt.mu.Unlock()
	if cb != nil {
		for _, id := range stale {
			cb(id)
		}
	}
}
