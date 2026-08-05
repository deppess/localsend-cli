package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Device    DeviceConfig      `toml:"device"`
	Receive   ReceiveConfig     `toml:"receive"`
	Discovery DiscoveryConfig   `toml:"discovery"`
	Whitelist WhitelistConfig   `toml:"whitelist"`
	Favorites map[string]string `toml:"favorites"` // alias -> IP
	Trusted   map[string]string `toml:"trusted"`   // IP -> fingerprint

	// mu guards Trusted mutation and persistence of this Config to disk.
	// Unexported, so it is invisible to the TOML encoder/decoder.
	mu sync.Mutex
}

type DeviceConfig struct {
	Name        string `toml:"name"`
	Port        int    `toml:"port"`
	Type        string `toml:"type"`
	Fingerprint string `toml:"fingerprint"` // populated after first TLS init
}

type ReceiveConfig struct {
	Dir           string `toml:"dir"`
	PromptTimeout int    `toml:"prompt_timeout"` // seconds; 0 = wait forever
	MaxFileMB     int64  `toml:"max_file_mb"`    // 0 = unlimited
}

type DiscoveryConfig struct {
	TimeoutMS         int `toml:"timeout_ms"`
	UploadConcurrency int `toml:"upload_concurrency"`
}

type WhitelistConfig struct {
	Enabled bool     `toml:"enabled"`
	IPs     []string `toml:"ips"`
}

// Dir returns the XDG config directory for localsend-cli.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "localsend-cli"), nil
}

// Load reads the config file, creating it with defaults if it doesn't exist.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	path := filepath.Join(dir, "config.toml")
	cfg := defaults()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := write(path, cfg); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(cfg)
	return cfg, nil
}

// Save writes the config back to disk. Safe for concurrent callers.
func Save(cfg *Config) error {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	return saveUnlocked(cfg)
}

// saveUnlocked persists cfg without acquiring cfg.mu. Callers must already
// hold the lock (or own a *Config not yet shared across goroutines, as in
// Load's initial-write path).
func saveUnlocked(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return write(filepath.Join(dir, "config.toml"), cfg)
}

// SetTrusted atomically records fp as the pinned fingerprint for ip and
// persists it. Safe for concurrent callers — e.g. multiple simultaneous
// TLS handshakes to the same not-yet-trusted peer during a multi-file send.
func (c *Config) SetTrusted(ip, fp string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Trusted == nil {
		c.Trusted = map[string]string{}
	}
	c.Trusted[ip] = fp
	return saveUnlocked(c)
}

// LookupTrusted returns the pinned fingerprint for ip, if any. Safe for
// concurrent callers; used as the read side of the TOFU trust check.
func (c *Config) LookupTrusted(ip string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fp, ok := c.Trusted[ip]
	return fp, ok
}

// RemoveTrusted deletes a pinned fingerprint, forcing re-TOFU on next
// contact. Returns false if there was nothing to remove.
func (c *Config) RemoveTrusted(ip string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.Trusted[ip]; !ok {
		return false, nil
	}
	delete(c.Trusted, ip)
	return true, saveUnlocked(c)
}

// TrustedSnapshot returns a copy of the trust store safe for callers to
// range over without holding c.mu.
func (c *Config) TrustedSnapshot() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.Trusted))
	for k, v := range c.Trusted {
		out[k] = v
	}
	return out
}

// write persists cfg to path atomically: it encodes to a temp file in the
// same directory (required so the final rename stays on one filesystem),
// fsyncs it, then renames it into place. This means a crash mid-write
// can never leave config.toml truncated or half-written.
func write(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config.toml.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) //nolint:errcheck // no-op once the rename below succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := toml.NewEncoder(tmp).Encode(cfg); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	// Best-effort: fsync the directory entry so the rename survives a
	// crash. Non-fatal if unsupported on the underlying filesystem.
	if d, err := os.Open(dir); err == nil {
		d.Sync() //nolint:errcheck
		d.Close()
	}
	return nil
}

func defaults() *Config {
	return &Config{
		Device: DeviceConfig{
			Name: randomName(),
			Port: 53317,
			Type: "headless",
		},
		Receive: ReceiveConfig{
			Dir:           "~/Downloads",
			PromptTimeout: 30,
		},
		Discovery: DiscoveryConfig{
			TimeoutMS:         500,
			UploadConcurrency: 4,
		},
		Whitelist: WhitelistConfig{
			Enabled: false,
			IPs:     []string{},
		},
		Favorites: map[string]string{},
		Trusted:   map[string]string{},
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Device.Port == 0 {
		cfg.Device.Port = 53317
	}
	if cfg.Device.Type == "" {
		cfg.Device.Type = "headless"
	}
	if cfg.Receive.Dir == "" {
		cfg.Receive.Dir = "~/Downloads"
	}
	if cfg.Discovery.TimeoutMS == 0 {
		cfg.Discovery.TimeoutMS = 500
	}
	if cfg.Discovery.UploadConcurrency == 0 {
		cfg.Discovery.UploadConcurrency = 4
	}
	if cfg.Favorites == nil {
		cfg.Favorites = map[string]string{}
	}
	if cfg.Trusted == nil {
		cfg.Trusted = map[string]string{}
	}
}

// ExpandDir expands ~ in a directory path.
func ExpandDir(dir string) string {
	if len(dir) == 0 || dir[0] != '~' {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	return filepath.Join(home, dir[1:])
}

// IsFavorite returns true if the given IP matches a configured favorite.
func (c *Config) IsFavorite(ip string) bool {
	for _, favIP := range c.Favorites {
		if favIP == ip {
			return true
		}
	}
	return false
}

// FavoriteAlias returns the alias for a favorited IP, or empty string.
func (c *Config) FavoriteAlias(ip string) string {
	for alias, favIP := range c.Favorites {
		if favIP == ip {
			return alias
		}
	}
	return ""
}

func randomName() string {
	b := make([]byte, 4)
	rand.Read(b) //nolint:errcheck
	return "localsend-" + hex.EncodeToString(b)
}
