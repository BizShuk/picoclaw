package a2a

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func TestA2AIntegration_EndToEnd(t *testing.T) {
	// Initialize Alice
	busAlice := bus.NewMessageBus()
	cfgAlice := &config.Channel{
		Enabled:   true,
		Type:      "a2a",
		AllowFrom: []string{"*"},
	}
	settingsAlice := &config.A2ASettings{
		AgentID:          "alice",
		Port:             0, // random OS port
		PeerTTL:          5 * time.Second,
		MaxTurnDefault:   6,
		AnnounceInterval: -1 * time.Second,
	}
	chAlice := NewA2AChannel(cfgAlice, settingsAlice, busAlice, "alice")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := chAlice.Start(ctx)
	require.NoError(t, err)
	defer chAlice.Stop(ctx)

	// Initialize Bob
	busBob := bus.NewMessageBus()
	cfgBob := &config.Channel{
		Enabled:   true,
		Type:      "a2a",
		AllowFrom: []string{"*"},
	}
	settingsBob := &config.A2ASettings{
		AgentID:          "bob",
		Port:             0, // random OS port
		PeerTTL:          5 * time.Second,
		MaxTurnDefault:   6,
		AnnounceInterval: -1 * time.Second,
	}
	chBob := NewA2AChannel(cfgBob, settingsBob, busBob, "bob")

	err = chBob.Start(ctx)
	require.NoError(t, err)
	defer chBob.Stop(ctx)

	// Manually link peers, bypass mDNS
	chAlice.peers.Upsert(&PeerInfo{
		AgentID:  "bob",
		Host:     "127.0.0.1",
		Port:     chBob.port,
		Version:  ProtocolVersion,
		WSPath:   "/a2a/v1/ws",
		LastSeen: time.Now(),
	})

	chBob.peers.Upsert(&PeerInfo{
		AgentID:  "alice",
		Host:     "127.0.0.1",
		Port:     chAlice.port,
		Version:  ProtocolVersion,
		WSPath:   "/a2a/v1/ws",
		LastSeen: time.Now(),
	})

	// Start a consumer goroutine on Bob's side to reply to messages
	go func() {
		ch := busBob.InboundChan()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				// Bob receives Alice's question and sends reply back
				if msg.Content == "What is 1+1?" {
					outbound := bus.OutboundMessage{
						Context: bus.InboundContext{
							Channel: "a2a",
							ChatID:  msg.Context.ChatID,
							Raw:     msg.Context.Raw, // carry "a2a_frame_id", "a2a_session_id"
						},
						Content: "2",
					}
					outbound.Context.Raw["done"] = "true"
					_, _ = chBob.Send(context.Background(), outbound)
				} else if msg.Content == "Loop Question" {
					outbound := bus.OutboundMessage{
						Context: bus.InboundContext{
							Channel: "a2a",
							ChatID:  msg.Context.ChatID,
							Raw:     msg.Context.Raw,
						},
						Content: "Reply to loop",
					}
					_, _ = chBob.Send(context.Background(), outbound)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// 1. A.AskPeer("bob", "What is 1+1?") -> reply "2"
	ans, err := chAlice.AskPeer(ctx, "bob", "test-session-1", 6, "What is 1+1?")
	require.NoError(t, err)
	assert.Equal(t, "2", ans)

	// 2. Loop turn limit test
	// Ask multiple times on same session to trigger MaxTurnExceeded
	var turnErr error
	for i := 0; i < 10; i++ {
		_, turnErr = chAlice.AskPeer(ctx, "bob", "loop-session", 3, "Loop Question")
		if turnErr != nil {
			break
		}
	}
	assert.Error(t, turnErr)
	assert.Contains(t, turnErr.Error(), "max turn limit exceeded")
}
