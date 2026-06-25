package whitelist

type Filter struct {
	enabled bool
	ips     map[string]struct{}
}

func New(enabled bool, ips []string) *Filter {
	m := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		m[ip] = struct{}{}
	}
	return &Filter{enabled: enabled, ips: m}
}

// Allow returns true if the IP is permitted to interact with this device.
// When the filter is disabled, all IPs are allowed.
func (f *Filter) Allow(ip string) bool {
	if !f.enabled {
		return true
	}
	_, ok := f.ips[ip]
	return ok
}

func (f *Filter) Enabled() bool { return f.enabled }

func (f *Filter) IPs() []string {
	out := make([]string, 0, len(f.ips))
	for ip := range f.ips {
		out = append(out, ip)
	}
	return out
}
