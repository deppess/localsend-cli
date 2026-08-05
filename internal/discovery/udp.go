package discovery

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

// ListenUDP listens for UDP multicast announcements.
// When a peer announces with announce:true, we respond via HTTP POST to their
// /register endpoint so they can discover us. filter gates both the response
// and the registry insertion, mirroring RegisterHandler's HTTP-layer
// enforcement — when whitelist mode is enabled, announcements from
// non-whitelisted IPs are ignored entirely.
func ListenUDP(ctx context.Context, reg *Registry, self protocol.DeviceInfo, filter *whitelist.Filter, favorites map[string]string, onDevice func(protocol.DiscoveredDevice)) error {
	addr := &net.UDPAddr{
		IP:   net.ParseIP(MulticastAddr),
		Port: DefaultPort,
	}
	conn, err := net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 8192)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				continue
			}
		}

		var info protocol.DeviceInfo
		if err := json.Unmarshal(buf[:n], &info); err != nil {
			continue
		}
		info.Alias = protocol.SanitizeAlias(info.Alias)
		if info.Alias == "" {
			continue
		}
		// Ignore our own broadcasts.
		if info.Fingerprint == self.Fingerprint {
			continue
		}

		remoteIP := remote.IP.String()
		if !filter.Allow(remoteIP) {
			continue
		}

		// When a device actively announces itself, respond so they know about us.
		if info.Announce && info.Port > 0 {
			go respondToAnnounce(ctx, remoteIP, info.Port, self)
		}

		dev := protocol.DiscoveredDevice{
			DeviceInfo: info,
			IP:         remoteIP,
			Favorite:   isFavorite(remoteIP, favorites),
		}
		if isNew := reg.Upsert(dev); isNew && onDevice != nil {
			onDevice(dev)
		}
	}
}

// BroadcastUDP sends the device announcement via multicast every 2 seconds.
// Whitelist filtering is enforced at the HTTP layer (RegisterHandler) — official
// LocalSend apps bind their UDP socket to the multicast group address and won't
// receive unicast packets, so we always multicast regardless of whitelist mode.
func BroadcastUDP(ctx context.Context, info protocol.DeviceInfo) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	data, _ := json.Marshal(info)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendMulticast(data, info.Port)
		}
	}
}

// sendMulticast sends on every up, multicast-capable interface so that devices
// on WiFi are reached even when this machine is primarily on ethernet.
func sendMulticast(data []byte, port int) {
	dst := &net.UDPAddr{IP: net.ParseIP(MulticastAddr), Port: port}
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, iface := range ifaces {
		if iface.Flags&(net.FlagUp|net.FlagMulticast) != (net.FlagUp|net.FlagMulticast) {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			conn, err := net.DialUDP("udp", &net.UDPAddr{IP: ipNet.IP}, dst)
			if err != nil {
				continue
			}
			conn.Write(data) //nolint:errcheck
			conn.Close()
			break // one send per interface
		}
	}
}


func isFavorite(ip string, favorites map[string]string) bool {
	for _, favIP := range favorites {
		if favIP == ip {
			return true
		}
	}
	return false
}
