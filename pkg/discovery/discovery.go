package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
)

// PeerInfo represents a discovered agent on the network.
type PeerInfo struct {
	AgentID      string    `json:"agent_id"`
	Name         string    `json:"name,omitempty"`
	Description  string    `json:"description,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	Features     []string  `json:"features,omitempty"`
	LastSeen     time.Time `json:"last_seen"`
}

// AnnouncePacket is the UDP broadcast payload.
type AnnouncePacket struct {
	Type         string   `json:"type"`
	AgentID      string   `json:"agent_id"`
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Features     []string `json:"features,omitempty"`
	Token        string   `json:"token,omitempty"`
	TS           int64    `json:"ts"`
}

// PeerCallback is invoked when a peer joins or leaves.
type PeerCallback func(peer PeerInfo)

// Config holds the discovery service configuration.
type Config struct {
	AgentID           string
	AgentName         string
	Description       string
	Capabilities      []string
	Host              string
	Port              int
	BroadcastPort     int
	BroadcastInterval time.Duration
	PeerTTL           time.Duration
	Token             string
	Features          []string
}

// Service manages UDP broadcast-based LAN agent discovery.
type Service struct {
	cfg    Config
	peers  map[string]*PeerInfo
	mu     sync.RWMutex
	cancel context.CancelFunc

	onJoin PeerCallback
	onLeft PeerCallback
}

// NewService creates a new discovery service.
func NewService(cfg Config) *Service {
	if cfg.BroadcastInterval == 0 {
		cfg.BroadcastInterval = 60 * time.Second
	}
	if cfg.PeerTTL == 0 {
		cfg.PeerTTL = 3 * cfg.BroadcastInterval
	}
	if cfg.BroadcastPort == 0 {
		cfg.BroadcastPort = 9101
	}
	return &Service{
		cfg:   cfg,
		peers: make(map[string]*PeerInfo),
	}
}

// OnPeerJoined sets the callback for newly discovered peers.
func (s *Service) OnPeerJoined(cb PeerCallback) { s.onJoin = cb }

// OnPeerLeft sets the callback for peers that have timed out.
func (s *Service) OnPeerLeft(cb PeerCallback) { s.onLeft = cb }

// Start begins the broadcast announcer, listener, and TTL reaper.
func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	go s.listenLoop(ctx)
	go s.announceLoop(ctx)
	go s.reaperLoop(ctx)

	logger.InfoCF("discovery", "Discovery service started", map[string]any{
		"agent_id":       s.cfg.AgentID,
		"broadcast_port": s.cfg.BroadcastPort,
		"interval":       s.cfg.BroadcastInterval.String(),
		"peer_ttl":       s.cfg.PeerTTL.String(),
	})
	return nil
}

// Stop shuts down the discovery service.
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	logger.InfoC("discovery", "Discovery service stopped")
}

// GetPeers returns a snapshot of all known alive peers.
func (s *Service) GetPeers() []PeerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peers := make([]PeerInfo, 0, len(s.peers))
	for _, p := range s.peers {
		peers = append(peers, *p)
	}
	return peers
}

// GetPeer returns a specific peer by agent ID.
func (s *Service) GetPeer(agentID string) (PeerInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.peers[agentID]
	if !ok {
		return PeerInfo{}, false
	}
	return *p, true
}

func (s *Service) announceLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.BroadcastInterval)
	defer ticker.Stop()

	// Send first announce immediately.
	s.broadcast()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.broadcast()
		}
	}
}

func (s *Service) broadcast() {
	pkt := AnnouncePacket{
		Type:         "announce",
		AgentID:      s.cfg.AgentID,
		Name:         s.cfg.AgentName,
		Description:  s.cfg.Description,
		Capabilities: s.cfg.Capabilities,
		Host:         s.cfg.Host,
		Port:         s.cfg.Port,
		Features:     s.cfg.Features,
		Token:        s.cfg.Token,
		TS:           time.Now().UnixMilli(),
	}

	data, err := json.Marshal(pkt)
	if err != nil {
		logger.ErrorCF("discovery", "Failed to marshal announce packet", map[string]any{"error": err.Error()})
		return
	}

	addrs := broadcastAddresses(s.cfg.BroadcastPort)
	if len(addrs) == 0 {
		logger.WarnC("discovery", "No broadcast addresses found")
		return
	}

	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		logger.ErrorCF("discovery", "Failed to open UDP socket for broadcast", map[string]any{"error": err.Error()})
		return
	}
	defer conn.Close()

	for _, addr := range addrs {
		if _, err := conn.WriteTo(data, addr); err != nil {
			logger.DebugCF("discovery", "Broadcast send failed", map[string]any{
				"addr":  addr.String(),
				"error": err.Error(),
			})
		}
	}
}

func (s *Service) listenLoop(ctx context.Context) {
	addr := fmt.Sprintf(":%d", s.cfg.BroadcastPort)
	conn, err := net.ListenPacket("udp4", addr)
	if err != nil {
		logger.ErrorCF("discovery", "Failed to listen for broadcasts", map[string]any{
			"addr":  addr,
			"error": err.Error(),
		})
		return
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 4096)
	for {
		n, remoteAddr, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.DebugCF("discovery", "UDP read error", map[string]any{"error": err.Error()})
			continue
		}

		var pkt AnnouncePacket
		if err := json.Unmarshal(buf[:n], &pkt); err != nil {
			logger.DebugCF("discovery", "Invalid announce packet", map[string]any{
				"from":  remoteAddr.String(),
				"error": err.Error(),
			})
			continue
		}

		if pkt.Type != "announce" {
			continue
		}

		// Ignore our own announcements.
		if pkt.AgentID == s.cfg.AgentID {
			continue
		}

		// Validate shared token if configured.
		if s.cfg.Token != "" && pkt.Token != s.cfg.Token {
			logger.DebugCF("discovery", "Announce rejected (token mismatch)", map[string]any{
				"from":     remoteAddr.String(),
				"agent_id": pkt.AgentID,
			})
			continue
		}

		host := pkt.Host
		if host == "" {
			host, _, _ = net.SplitHostPort(remoteAddr.String())
		}

		s.registerPeer(PeerInfo{
			AgentID:      pkt.AgentID,
			Name:         pkt.Name,
			Description:  pkt.Description,
			Capabilities: pkt.Capabilities,
			Host:         host,
			Port:         pkt.Port,
			Features:     pkt.Features,
			LastSeen:     time.Now(),
		})
	}
}

func (s *Service) registerPeer(peer PeerInfo) {
	s.mu.Lock()
	existing, ok := s.peers[peer.AgentID]
	s.peers[peer.AgentID] = &peer
	s.mu.Unlock()

	if !ok {
		logger.InfoCF("discovery", "New peer discovered", map[string]any{
			"agent_id": peer.AgentID,
			"name":     peer.Name,
			"host":     peer.Host,
			"port":     peer.Port,
		})
		if s.onJoin != nil {
			s.onJoin(peer)
		}
	} else if existing.Host != peer.Host || existing.Port != peer.Port {
		logger.InfoCF("discovery", "Peer endpoint changed", map[string]any{
			"agent_id": peer.AgentID,
			"old_host": existing.Host,
			"new_host": peer.Host,
		})
	}
}

func (s *Service) reaperLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.BroadcastInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.reapExpired(now)
		}
	}
}

func (s *Service) reapExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, peer := range s.peers {
		if now.Sub(peer.LastSeen) > s.cfg.PeerTTL {
			delete(s.peers, id)
			logger.InfoCF("discovery", "Peer expired", map[string]any{
				"agent_id": id,
				"last_seen": peer.LastSeen.Format(time.RFC3339),
			})
			if s.onLeft != nil {
				go s.onLeft(*peer)
			}
		}
	}
}

// broadcastAddresses returns the UDP broadcast addresses for all suitable network interfaces.
func broadcastAddresses(port int) []*net.UDPAddr {
	ifaces, err := net.Interfaces()
	if err != nil {
		return []*net.UDPAddr{{IP: net.IPv4bcast, Port: port}}
	}

	var addrs []*net.UDPAddr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		ifAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range ifAddrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			bcast := broadcastAddr(ipNet)
			if bcast != nil {
				addrs = append(addrs, &net.UDPAddr{IP: bcast, Port: port})
			}
		}
	}

	if len(addrs) == 0 {
		addrs = append(addrs, &net.UDPAddr{IP: net.IPv4bcast, Port: port})
	}
	return addrs
}

// broadcastAddr computes the broadcast address for a given network.
func broadcastAddr(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	if ip == nil {
		return nil
	}
	mask := n.Mask
	bcast := make(net.IP, len(ip))
	for i := range ip {
		bcast[i] = ip[i] | ^mask[i]
	}
	return bcast
}
