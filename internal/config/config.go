package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Device    DeviceConfig      `toml:"device"`
	Receive   ReceiveConfig     `toml:"receive"`
	Discovery DiscoveryConfig   `toml:"discovery"`
	Whitelist WhitelistConfig   `toml:"whitelist"`
	Favorites map[string]string `toml:"favorites"` // alias -> IP
	Trusted   map[string]string `toml:"trusted"`   // IP -> fingerprint
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

// Save writes the config back to disk.
func Save(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return write(filepath.Join(dir, "config.toml"), cfg)
}

func write(path string, cfg *Config) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
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
