package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/deppes/localsend-cli/internal/config"
)

func newOutCmd() (*cobra.Command, *bytes.Buffer) {
	var buf bytes.Buffer
	c := &cobra.Command{}
	c.SetOut(&buf)
	return c, &buf
}

func TestTrustListEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c, buf := newOutCmd()
	if err := runTrustList(c, nil); err != nil {
		t.Fatalf("runTrustList: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "no trusted peers" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestTrustListAndRemove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.SetTrusted("192.168.1.20", "sha256:abc"); err != nil {
		t.Fatalf("SetTrusted: %v", err)
	}

	c, buf := newOutCmd()
	if err := runTrustList(c, nil); err != nil {
		t.Fatalf("runTrustList: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "192.168.1.20  sha256:abc" {
		t.Fatalf("unexpected list output: %q", got)
	}

	c2, buf2 := newOutCmd()
	if err := runTrustRemove(c2, []string{"192.168.1.20"}); err != nil {
		t.Fatalf("runTrustRemove: %v", err)
	}
	if got := strings.TrimSpace(buf2.String()); got != "removed trust entry for 192.168.1.20" {
		t.Fatalf("unexpected remove output: %q", got)
	}

	c3, buf3 := newOutCmd()
	if err := runTrustList(c3, nil); err != nil {
		t.Fatalf("runTrustList after removal: %v", err)
	}
	if got := strings.TrimSpace(buf3.String()); got != "no trusted peers" {
		t.Fatalf("expected empty list after removal, got: %q", got)
	}

	c4, buf4 := newOutCmd()
	if err := runTrustRemove(c4, []string{"192.168.1.20"}); err != nil {
		t.Fatalf("runTrustRemove (no-op): %v", err)
	}
	if got := strings.TrimSpace(buf4.String()); got != "no trust entry for 192.168.1.20" {
		t.Fatalf("unexpected no-op remove output: %q", got)
	}
}
