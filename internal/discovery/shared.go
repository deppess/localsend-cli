package discovery

import (
	"sync"
	"time"

	"github.com/deppes/localsend-cli/internal/protocol"
)

const (
	MulticastAddr = "224.0.0.167"
	DefaultPort   = 53317
	deviceTTL     = 120 * time.Second
)

type entry struct {
	device  protocol.DiscoveredDevice
	lastSeen time.Time
}

// Registry holds discovered devices with TTL-based expiry.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]entry // key: IP
}

func NewRegistry() *Registry {
	r := &Registry{entries: make(map[string]entry)}
	go r.reap()
	return r
}

// Upsert adds or updates a device. Returns true if it's a new device.
func (r *Registry) Upsert(dev protocol.DiscoveredDevice) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exists := r.entries[dev.IP]
	r.entries[dev.IP] = entry{device: dev, lastSeen: time.Now()}
	return !exists
}

// List returns a snapshot of all live devices.
func (r *Registry) List() []protocol.DiscoveredDevice {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]protocol.DiscoveredDevice, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.device)
	}
	return out
}

func (r *Registry) reap() {
	for range time.Tick(30 * time.Second) {
		now := time.Now()
		r.mu.Lock()
		for ip, e := range r.entries {
			if now.Sub(e.lastSeen) > deviceTTL {
				delete(r.entries, ip)
			}
		}
		r.mu.Unlock()
	}
}
