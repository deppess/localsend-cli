package discovery

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

var httpClient = &http.Client{
	// Overall timeout generous enough for iOS (TLS handshake on a sandboxed network stack
	// takes 200-400ms alone). The dial timeout catches unreachable IPs fast so they don't
	// hold a scan slot for the full 1.5s.
	Timeout: 1500 * time.Millisecond,
	Transport: &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // peer identity verified via fingerprint
		MaxIdleConnsPerHost: 1,
		DisableKeepAlives:   true,
		DialContext: (&net.Dialer{
			Timeout: 250 * time.Millisecond, // IPs with nothing on 53317 fail here, not at 1.5s
		}).DialContext,
	},
}

// ScanHTTP sends the device info to all hosts on the local subnet and collects
// responses. No pre-ping — we try the LocalSend port directly, which avoids
// the cap_net_raw requirement that ICMP pinging needs.
func ScanHTTP(ctx context.Context, reg *Registry, self protocol.DeviceInfo, filter *whitelist.Filter, favorites map[string]string, onDevice func(protocol.DiscoveredDevice)) { //nolint:cyclop
	ips := subnetIPs(filter)

	data, _ := json.Marshal(self)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 128)

	for _, ip := range ips {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			registerHTTPFiltered(ctx, reg, ip, self.Port, self.Fingerprint, data, favorites, onDevice)
		}(ip)
	}
	wg.Wait()
}

// respondToAnnounce sends our device info to a peer's /register endpoint so
// they can add us to their device list. Called when we receive a UDP announce.
func respondToAnnounce(ctx context.Context, ip string, port int, self protocol.DeviceInfo) {
	data, _ := json.Marshal(self)
	registerHTTP(ctx, nil, ip, port, data, nil, nil)
}

// ReannounceToKnown periodically POSTs our device info to all peers in the
// registry. This keeps us visible in their device lists even when:
//   - their UI "refresh" doesn't re-broadcast UDP
//   - UDP multicast is unreliable on the path between us
func ReannounceToKnown(ctx context.Context, reg *Registry, self protocol.DeviceInfo, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	data, _ := json.Marshal(self)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, dev := range reg.List() {
				go registerHTTP(ctx, nil, dev.IP, dev.Port, data, nil, nil)
			}
		}
	}
}

func registerHTTPFiltered(ctx context.Context, reg *Registry, ip string, port int, selfFP string, body []byte, favorites map[string]string, onDevice func(protocol.DiscoveredDevice)) {
	url := fmt.Sprintf("https://%s:%d/api/localsend/v2/register", ip, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return
	}

	var info protocol.DeviceInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return
	}
	info.Alias = protocol.SanitizeAlias(info.Alias)
	if info.Alias == "" || info.Fingerprint == selfFP || reg == nil {
		return
	}

	dev := protocol.DiscoveredDevice{
		DeviceInfo: info,
		IP:         ip,
		Favorite:   isFavorite(ip, favorites),
	}
	if isNew := reg.Upsert(dev); isNew && onDevice != nil {
		onDevice(dev)
	}
}

func registerHTTP(ctx context.Context, reg *Registry, ip string, port int, body []byte, favorites map[string]string, onDevice func(protocol.DiscoveredDevice)) {
	url := fmt.Sprintf("https://%s:%d/api/localsend/v2/register", ip, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return
	}

	var info protocol.DeviceInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return
	}
	info.Alias = protocol.SanitizeAlias(info.Alias)
	if info.Alias == "" || reg == nil {
		return
	}

	dev := protocol.DiscoveredDevice{
		DeviceInfo: info,
		IP:         ip,
		Favorite:   isFavorite(ip, favorites),
	}
	if isNew := reg.Upsert(dev); isNew && onDevice != nil {
		onDevice(dev)
	}
}

// RegisterHandler handles POST /api/localsend/v2/register.
func RegisterHandler(reg *Registry, self protocol.DeviceInfo, filter *whitelist.Filter, favorites map[string]string, onDevice func(protocol.DiscoveredDevice)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := RemoteIP(r)
		if !filter.Allow(ip) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		var info protocol.DeviceInfo
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&info); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		info.Alias = protocol.SanitizeAlias(info.Alias)
		if info.Alias == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Always respond with our own info first (the caller needs this to know about us).
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(self) //nolint:errcheck

		// Don't add ourselves to the registry.
		if info.Fingerprint == self.Fingerprint {
			return
		}

		dev := protocol.DiscoveredDevice{
			DeviceInfo: info,
			IP:         ip,
			Favorite:   isFavorite(ip, favorites),
		}
		if isNew := reg.Upsert(dev); isNew && onDevice != nil {
			onDevice(dev)
		}
	}
}

// subnetIPs returns all host IPs to scan.
// In whitelist-only mode it returns the whitelist; otherwise it enumerates
// every host on each local IPv4 subnet.
func subnetIPs(filter *whitelist.Filter) []string {
	if filter.Enabled() {
		return filter.IPs()
	}
	return enumerateLocalSubnets()
}

func enumerateLocalSubnets() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var all []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
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
			all = append(all, hostsInSubnet(ipNet)...)
		}
	}
	return all
}

// hostsInSubnet enumerates all host addresses in a subnet (excludes network
// and broadcast). Works for any prefix length.
func hostsInSubnet(ipNet *net.IPNet) []string {
	ip4 := ipNet.IP.To4()
	if ip4 == nil {
		return nil
	}
	ones, _ := ipNet.Mask.Size()
	hostBits := 32 - ones
	if hostBits <= 0 || hostBits >= 31 {
		return nil
	}
	count := (1 << hostBits) - 2
	if count <= 0 {
		return nil
	}

	base := ipToUint32(ip4.Mask(ipNet.Mask)) + 1 // network addr + 1
	result := make([]string, count)
	for i := 0; i < count; i++ {
		result[i] = uint32ToIP(base + uint32(i)).String()
	}
	return result
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIP(n uint32) net.IP {
	return net.IP{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}

// RemoteIP extracts the client IP from a request.
func RemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
