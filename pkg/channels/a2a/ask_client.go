package a2a

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrPeerUnreachable = errors.New("peer unreachable")

type pendingMap struct {
	mu sync.Mutex
	m  map[string]chan *Envelope
}

func newPendingMap() *pendingMap {
	return &pendingMap{
		m: make(map[string]chan *Envelope),
	}
}

func (p *pendingMap) Wait(frameID string) chan *Envelope {
	ch := make(chan *Envelope, 1)
	p.mu.Lock()
	p.m[frameID] = ch
	p.mu.Unlock()
	return ch
}

func (p *pendingMap) Deliver(inReplyTo string, env *Envelope) bool {
	p.mu.Lock()
	ch, ok := p.m[inReplyTo]
	if ok {
		delete(p.m, inReplyTo)
	}
	p.mu.Unlock()
	if ok {
		ch <- env
		return true
	}
	return false
}

func (p *pendingMap) Cancel(frameID string) {
	p.mu.Lock()
	delete(p.m, frameID)
	p.mu.Unlock()
}

func (p *pendingMap) CancelAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, ch := range p.m {
		close(ch)
		delete(p.m, id)
	}
}

// AskPeer runs a synchronous RPC conversation over A2A.
func (c *A2AChannel) AskPeer(ctx context.Context, peerID, sessionID string, maxTurn int, question string) (string, error) {
	// 1. Check and increment turn count
	turn, err := c.turns.CheckAndIncrement(sessionID, maxTurn)
	if err != nil {
		return "", err
	}

	// 2. Get connection to the peer
	conn, err := c.clients.GetOrDial(ctx, peerID)
	if err != nil {
		return "", ErrPeerUnreachable
	}

	frameID := uuid.New().String()
	env := &Envelope{
		V:         ProtocolVersion,
		Type:      TypeAsk,
		FrameID:   frameID,
		SessionID: sessionID,
		From:      c.agentID,
		To:        peerID,
		Ts:        time.Now().Unix(),
		Payload: AskPayload{
			Question: question,
			MaxTurn:  maxTurn,
			Turn:     turn,
		},
	}

	// 3. Register pending waiter
	ch := conn.pending.Wait(frameID)

	// 4. Send frame
	if err := conn.writer.WriteFrame(ctx, env); err != nil {
		conn.pending.Cancel(frameID)
		return "", fmt.Errorf("write ask frame: %w", err)
	}

	// 5. Wait for reply
	select {
	case reply := <-ch:
		if reply == nil {
			return "", errors.New("connection closed while waiting for reply")
		}
		if reply.Type == TypeError {
			ep, ok := reply.Payload.(ErrorPayload)
			if ok {
				return "", fmt.Errorf("remote error (%s): %s", ep.Code, ep.Message)
			}
			return "", errors.New("remote error without details")
		}

		rp, ok := reply.Payload.(ReplyPayload)
		if !ok {
			return "", errors.New("invalid reply payload type")
		}

		if rp.Done {
			c.turns.MarkDone(sessionID)
		}

		return rp.Answer, nil

	case <-ctx.Done():
		conn.pending.Cancel(frameID)
		return "", ctx.Err()
	}
}
