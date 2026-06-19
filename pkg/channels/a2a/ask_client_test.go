package a2a

import (
	"testing"
)

func TestPendingMap_WaitAndDeliver(t *testing.T) {
	p := newPendingMap()
	ch := p.Wait("f1")

	env := &Envelope{FrameID: "reply-1", InReplyTo: "f1", Type: TypeReply}
	delivered := p.Deliver("f1", env)
	if !delivered {
		t.Fatal("expected delivery to succeed")
	}

	select {
	case gotBytes := <-ch:
		if gotBytes.FrameID != "reply-1" {
			t.Fatalf("got FrameID = %s, want reply-1", gotBytes.FrameID)
		}
	default:
		t.Fatal("expected event to be in channel")
	}

	// second deliver should return false (already removed)
	delivered = p.Deliver("f1", env)
	if delivered {
		t.Fatal("expected second delivery to fail")
	}
}

func TestPendingMap_Cancel(t *testing.T) {
	p := newPendingMap()
	ch := p.Wait("f1")

	p.Cancel("f1")

	env := &Envelope{FrameID: "reply-1", InReplyTo: "f1", Type: TypeReply}
	delivered := p.Deliver("f1", env)
	if delivered {
		t.Fatal("expected delivery to fail after cancel")
	}

	select {
	case <-ch:
		t.Fatal("expected no message in channel")
	default:
		// success
	}
}

func TestPendingMap_CancelAll(t *testing.T) {
	p := newPendingMap()
	ch1 := p.Wait("f1")
	ch2 := p.Wait("f2")

	p.CancelAll()

	_, ok1 := <-ch1
	if ok1 {
		t.Fatal("expected ch1 to be closed")
	}

	_, ok2 := <-ch2
	if ok2 {
		t.Fatal("expected ch2 to be closed")
	}
}
