package a2a

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestPeerTable_UpsertAndGet(t *testing.T) {
	pt := newPeerTable(1 * time.Hour)
	p := &PeerInfo{AgentID: "bob", Host: "10.0.0.2", Port: 9100, Version: 1}
	pt.Upsert(p)
	got, ok := pt.Get("bob")
	if !ok || got.Host != "10.0.0.2" {
		t.Fatalf("Get(bob): %+v ok=%v", got, ok)
	}
}

func TestPeerTable_OnAddFiresOnceForNewPeer(t *testing.T) {
	var added atomic.Int32
	pt := newPeerTable(1 * time.Hour)
	pt.onAdd = func(*PeerInfo) { added.Add(1) }

	p := &PeerInfo{AgentID: "bob", Host: "x", Port: 1}
	pt.Upsert(p)
	pt.Upsert(p)
	if got := added.Load(); got != 1 {
		t.Fatalf("onAdd fired %d times, want 1", got)
	}
}

func TestPeerTable_ReapStaleRemovesAndCallsOnRemove(t *testing.T) {
	var removed atomic.Int32
	pt := newPeerTable(10 * time.Millisecond)
	pt.onRemove = func(string) { removed.Add(1) }

	pt.Upsert(&PeerInfo{AgentID: "bob", Host: "x", Port: 1})
	time.Sleep(20 * time.Millisecond)
	pt.ReapStale()

	if _, ok := pt.Get("bob"); ok {
		t.Fatal("bob should have been reaped")
	}
	if removed.Load() != 1 {
		t.Fatalf("onRemove fired %d, want 1", removed.Load())
	}
}

func TestPeerTable_UpsertRefreshesLastSeen(t *testing.T) {
	pt := newPeerTable(50 * time.Millisecond)
	pt.Upsert(&PeerInfo{AgentID: "bob", Host: "x", Port: 1})
	time.Sleep(30 * time.Millisecond)
	pt.Upsert(&PeerInfo{AgentID: "bob", Host: "x", Port: 1}) // refresh
	time.Sleep(30 * time.Millisecond)
	pt.ReapStale()
	if _, ok := pt.Get("bob"); !ok {
		t.Fatal("bob should still be present (refresh)")
	}
}
