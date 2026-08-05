package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func tempConfigHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tempConfigHome(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cfg.Whitelist.Enabled = true
	cfg.Whitelist.IPs = []string{"192.168.1.5", "192.168.1.6"}
	cfg.Favorites["phone"] = "192.168.1.20"
	if err := cfg.SetTrusted("192.168.1.20", "sha256:deadbeef"); err != nil {
		t.Fatalf("SetTrusted: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	if !reloaded.Whitelist.Enabled || len(reloaded.Whitelist.IPs) != 2 {
		t.Fatalf("whitelist not persisted: %+v", reloaded.Whitelist)
	}
	if reloaded.Favorites["phone"] != "192.168.1.20" {
		t.Fatalf("favorites not persisted: %+v", reloaded.Favorites)
	}
	if fp, ok := reloaded.LookupTrusted("192.168.1.20"); !ok || fp != "sha256:deadbeef" {
		t.Fatalf("trusted not persisted: %q %v", fp, ok)
	}
}

func TestSetTrustedConcurrentNoRace(t *testing.T) {
	tempConfigHome(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ip := "10.0.0.1" // same peer, simulating concurrent handshakes to one not-yet-trusted host
			if _, ok := cfg.LookupTrusted(ip); !ok {
				if err := cfg.SetTrusted(ip, "sha256:same"); err != nil {
					t.Errorf("SetTrusted: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	fp, ok := cfg.LookupTrusted("10.0.0.1")
	if !ok || fp != "sha256:same" {
		t.Fatalf("unexpected trust state: %q %v", fp, ok)
	}

	// Reload from disk to make sure the last concurrent Save left a valid file.
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload after concurrent writes: %v", err)
	}
	if fp, ok := reloaded.LookupTrusted("10.0.0.1"); !ok || fp != "sha256:same" {
		t.Fatalf("trust not durably persisted: %q %v", fp, ok)
	}
}

func TestRemoveTrusted(t *testing.T) {
	tempConfigHome(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.SetTrusted("192.168.1.20", "sha256:abc"); err != nil {
		t.Fatalf("SetTrusted: %v", err)
	}

	removed, err := cfg.RemoveTrusted("192.168.1.20")
	if err != nil || !removed {
		t.Fatalf("RemoveTrusted: removed=%v err=%v", removed, err)
	}
	if _, ok := cfg.LookupTrusted("192.168.1.20"); ok {
		t.Fatal("entry still present after removal")
	}

	removed, err = cfg.RemoveTrusted("192.168.1.20")
	if err != nil || removed {
		t.Fatalf("second removal should be a no-op: removed=%v err=%v", removed, err)
	}
}

func TestTrustedSnapshotIsCopy(t *testing.T) {
	tempConfigHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.SetTrusted("1.2.3.4", "sha256:x"); err != nil {
		t.Fatalf("SetTrusted: %v", err)
	}
	snap := cfg.TrustedSnapshot()
	snap["1.2.3.4"] = "tampered"
	if fp, _ := cfg.LookupTrusted("1.2.3.4"); fp != "sha256:x" {
		t.Fatalf("mutating snapshot affected underlying store: %q", fp)
	}
}

func TestWriteIsAtomicNoTempLeftover(t *testing.T) {
	tempConfigHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	const tmpPrefix = ".config.toml.tmp-"
	for _, e := range entries {
		if len(e.Name()) >= len(tmpPrefix) && e.Name()[:len(tmpPrefix)] == tmpPrefix {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}

	info, err := os.Stat(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("Stat config.toml: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected mode 0600, got %o", perm)
	}
}
