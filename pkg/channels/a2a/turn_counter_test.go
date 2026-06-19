package a2a

import (
	"errors"
	"testing"
	"time"
)

func TestTurnCounter_StartsAtZero(t *testing.T) {
	tc := newTurnCounter()
	if got := tc.Current("s1"); got != 0 {
		t.Fatalf("Current = %d, want 0", got)
	}
}

func TestTurnCounter_CheckAndIncrementUpToMax(t *testing.T) {
	tc := newTurnCounter()
	for want := 1; want <= 3; want++ {
		got, err := tc.CheckAndIncrement("s1", 3)
		if err != nil {
			t.Fatalf("turn %d: err = %v", want, err)
		}
		if got != want {
			t.Fatalf("got = %d, want %d", got, want)
		}
	}

	// turn 4 should exceed max_turn=3
	_, err := tc.CheckAndIncrement("s1", 3)
	if !errors.Is(err, ErrMaxTurnExceeded) {
		t.Fatalf("got err = %v, want ErrMaxTurnExceeded", err)
	}
}

func TestTurnCounter_MarkDonePutsInEndedSet(t *testing.T) {
	tc := newTurnCounter()
	tc.ttl = 50 * time.Millisecond

	_, err := tc.CheckAndIncrement("s1", 3)
	if err != nil {
		t.Fatal(err)
	}

	tc.MarkDone("s1")

	// Current should be reset to 0
	if got := tc.Current("s1"); got != 0 {
		t.Fatalf("Current after MarkDone = %d, want 0", got)
	}

	// Post-done ask on same session should fail with ErrSessionEnded
	_, err = tc.CheckAndIncrement("s1", 3)
	if !errors.Is(err, ErrSessionEnded) {
		t.Fatalf("got err = %v, want ErrSessionEnded", err)
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// After TTL, check should allow a new conversation on the same session ID
	got, err := tc.CheckAndIncrement("s1", 3)
	if err != nil {
		t.Fatalf("after TTL: err = %v", err)
	}
	if got != 1 {
		t.Fatalf("after TTL: got = %d, want 1", got)
	}
}
