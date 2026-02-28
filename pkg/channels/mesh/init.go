package mesh

import (
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("mesh", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewMeshChannel(cfg.Channels.Mesh, b)
	})
}
