package discovery

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

func TestRegisterHandlerWhitelist(t *testing.T) {
	self := protocol.DeviceInfo{Alias: "me", Fingerprint: "self-fp"}
	reg := NewRegistry()
	filter := whitelist.New(true, []string{"10.0.0.1"})
	handler := RegisterHandler(reg, self, filter, map[string]string{}, nil)

	body, _ := json.Marshal(protocol.DeviceInfo{Alias: "peer", Fingerprint: "peer-fp"})

	t.Run("non-whitelisted IP is forbidden", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.2:1234"
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
		if len(reg.List()) != 0 {
			t.Fatal("non-whitelisted peer should not be added to the registry")
		}
	})

	t.Run("whitelisted IP is registered", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if len(reg.List()) != 1 {
			t.Fatalf("expected peer to be registered, got %d entries", len(reg.List()))
		}
	})
}

func TestRegisterHandlerSanitizesAlias(t *testing.T) {
	self := protocol.DeviceInfo{Alias: "me", Fingerprint: "self-fp"}
	reg := NewRegistry()
	filter := whitelist.New(false, nil)
	handler := RegisterHandler(reg, self, filter, map[string]string{}, nil)

	body, _ := json.Marshal(protocol.DeviceInfo{Alias: "Evil\x1b[31mDevice", Fingerprint: "peer-fp"})
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler(rec, req)

	devices := reg.List()
	if len(devices) != 1 {
		t.Fatalf("expected 1 registered device, got %d", len(devices))
	}
	if devices[0].Alias != "Evil[31mDevice" {
		t.Fatalf("alias was not sanitized: %q", devices[0].Alias)
	}
}
