package a2a

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/sipeed/picoclaw/pkg/logger"
)

type discovery struct {
	ch     *A2AChannel
	server *mdns.Server
	ctx    context.Context
	cancel context.CancelFunc
}

func newDiscovery(ch *A2AChannel) *discovery {
	return &discovery{
		ch: ch,
	}
}

func (d *discovery) Start() error {
	d.ctx, d.cancel = context.WithCancel(context.Background())

	// 1. Prepare TXT records
	desc := d.ch.cfg.Description
	if len(desc) > 200 {
		desc = desc[:200]
	}
	txts := []string{
		"v=1",
		"path=/a2a/v1/ws",
		"agent=" + d.ch.agentID,
		"desc=" + desc,
	}

	// 2. Announce mDNS service
	// We use the local agent ID as the instance name.
	serviceType := d.ch.cfg.ServiceType
	if serviceType == "" {
		serviceType = "_picoclaw-a2a._tcp"
	}
	domain := d.ch.cfg.MDNSDomain
	if domain == "" {
		domain = "local."
	}

	// Pass an explicit hostname + IPs rather than letting hashicorp/mdns resolve
	// os.Hostname(): on hosts whose hostname is unset or set to a bare IP (common
	// on macOS/DHCP), that lookup fails with "could not determine host IP addresses".
	hostName := fmt.Sprintf("%s.%s", sanitizeInstance(d.ch.agentID), domain)
	ips := advertiseIPs(d.ch.cfg.BindAddr)

	service, err := mdns.NewMDNSService(
		d.ch.agentID,
		serviceType,
		domain,
		hostName,
		d.ch.port,
		ips,
		txts,
	)
	if err != nil {
		return fmt.Errorf("create mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return fmt.Errorf("start mDNS server: %w", err)
	}
	d.server = server

	// 3. Start browse loop and reaper loop
	go d.browseLoop()
	go d.reapLoop()

	logger.InfoCF("a2a", "mDNS discovery started", map[string]any{
		"agent_id":     d.ch.agentID,
		"port":         d.ch.port,
		"service_type": serviceType,
	})
	return nil
}

func (d *discovery) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.server != nil {
		_ = d.server.Shutdown()
	}
}

func (d *discovery) browseLoop() {
	// First immediate browse
	d.browse()

	interval := d.ch.cfg.AnnounceInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.browse()
		}
	}
}

func (d *discovery) browse() {
	entriesCh := make(chan *mdns.ServiceEntry, 32)

	// Collect entries in background
	go func() {
		for entry := range entriesCh {
			d.handleEntry(entry)
		}
	}()

	serviceType := d.ch.cfg.ServiceType
	if serviceType == "" {
		serviceType = "_picoclaw-a2a._tcp"
	}
	domain := d.ch.cfg.MDNSDomain
	if domain == "" {
		domain = "local."
	}

	params := &mdns.QueryParam{
		Service:             serviceType,
		Domain:              domain,
		Timeout:             2 * time.Second,
		Entries:             entriesCh,
		WantUnicastResponse: true,
	}

	if err := mdns.Query(params); err != nil {
		logger.WarnCF("a2a", "mDNS query failed", map[string]any{"error": err.Error()})
	}
	close(entriesCh)
}

func (d *discovery) handleEntry(entry *mdns.ServiceEntry) {
	// Parse instance name to extract peer AgentID
	parts := strings.Split(entry.Name, ".")
	if len(parts) == 0 {
		return
	}
	peerID := parts[0]

	// Self-filter: don't discover ourselves
	if peerID == d.ch.agentID {
		return
	}

	// Parse TXT records
	var version int
	wsPath := "/a2a/v1/ws"
	desc := ""
	for _, txt := range entry.InfoFields {
		if strings.HasPrefix(txt, "v=") {
			_, _ = fmt.Sscanf(txt, "v=%d", &version)
		} else if strings.HasPrefix(txt, "path=") {
			wsPath = strings.TrimPrefix(txt, "path=")
		} else if strings.HasPrefix(txt, "desc=") {
			desc = strings.TrimPrefix(txt, "desc=")
		}
	}

	host := entry.AddrV4.String()
	if entry.AddrV4 == nil && entry.AddrV6 != nil {
		host = entry.AddrV6.String()
	}

	if host == "0.0.0.0" || host == "<nil>" || host == "" {
		// Use the entry name resolver if host is empty
		return
	}

	peer := &PeerInfo{
		AgentID:     peerID,
		Host:        host,
		Port:        entry.Port,
		Version:     version,
		WSPath:      wsPath,
		Description: desc,
		LastSeen:    time.Now(),
	}

	d.ch.peers.Upsert(peer)
}

func (d *discovery) reapLoop() {
	interval := d.ch.cfg.AnnounceInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.ch.peers.ReapStale()
		}
	}
}

// advertiseIPs returns the single IP address to announce over mDNS. A valid,
// concrete bindAddr IP wins. Otherwise the primary outbound IP is chosen via a
// route lookup (a UDP "dial" that sends no packets) so we announce the real LAN
// interface rather than a virtual/VPN one — advertising every interface lets a
// peer resolve an unreachable address (e.g. a utun or bridge network base).
// Falls back to the first non-loopback IPv4, then loopback on an isolated host.
func advertiseIPs(bindAddr string) []net.IP {
	if ip := net.ParseIP(strings.TrimSpace(bindAddr)); ip != nil && !ip.IsUnspecified() {
		return []net.IP{ip}
	}

	if ip := primaryOutboundIP(); ip != nil {
		return []net.IP{ip}
	}

	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if v4 := ipNet.IP.To4(); v4 != nil {
			return []net.IP{v4}
		}
	}
	return []net.IP{net.IPv4(127, 0, 0, 1)}
}

// primaryOutboundIP returns the local IPv4 the kernel would use to reach the
// public internet — i.e. the default-route interface — without sending packets.
func primaryOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil
	}
	defer conn.Close()
	ua, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || ua.IP == nil || ua.IP.IsLoopback() {
		return nil
	}
	return ua.IP.To4()
}

// sanitizeInstance turns an agent ID into a DNS-safe single label for use as the
// mDNS hostname (so we never depend on a resolvable os.Hostname()).
func sanitizeInstance(s string) string {
	s = strings.NewReplacer(" ", "-", ".", "-").Replace(strings.TrimSpace(s))
	if s == "" {
		return "picoclaw"
	}
	return s
}
