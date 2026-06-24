package a2a

import "testing"

func TestSessionMap_ResolveCreatesOnceAndIsStable(t *testing.T) {
	sm := newSessionMap()
	k1, created := sm.Resolve("ext-1", "bob")
	if !created || k1 == "" {
		t.Fatalf("first call: created=%v key=%q", created, k1)
	}
	k2, created2 := sm.Resolve("ext-1", "bob")
	if created2 {
		t.Fatalf("second call should not create")
	}
	if k1 != k2 {
		t.Fatalf("keys differ: %q vs %q", k1, k2)
	}
}

func TestSessionMap_DifferentPeersDifferentKeys(t *testing.T) {
	sm := newSessionMap()
	kBob, _ := sm.Resolve("ext-1", "bob")
	kCarol, _ := sm.Resolve("ext-1", "carol")
	if kBob == kCarol {
		t.Fatal("two peers, same key — scope dimension lost")
	}
}

func TestSessionMap_ReverseLookup(t *testing.T) {
	sm := newSessionMap()
	k, _ := sm.Resolve("ext-1", "bob")
	if got, ok := sm.ExternalFor(k); !ok || got != "ext-1" {
		t.Fatalf("ExternalFor: %q ok=%v", got, ok)
	}
}

func TestSessionMap_BindExplicit(t *testing.T) {
	sm := newSessionMap()
	sm.Bind("ext-9", "sk_v1_test")
	got, ok := sm.LocalFor("ext-9")
	if !ok || got != "sk_v1_test" {
		t.Fatalf("LocalFor: %q ok=%v", got, ok)
	}
}

func TestSessionMap_GetOrCreate(t *testing.T) {
	sm := newSessionMap()
	extID1 := sm.GetOrCreate("sk_parent", "bob")
	if extID1 == "" {
		t.Fatal("GetOrCreate returned empty external ID")
	}
	extID2 := sm.GetOrCreate("sk_parent", "bob")
	if extID1 != extID2 {
		t.Fatalf("subsequent GetOrCreate returned different ID: %q vs %q", extID1, extID2)
	}
	extID3 := sm.GetOrCreate("sk_parent", "carol")
	if extID1 == extID3 {
		t.Fatal("GetOrCreate returned same ID for different peer")
	}
}
